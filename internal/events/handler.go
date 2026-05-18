package events

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sergiught/auth0-mock/internal/httperr"
)

// drainableWriter wraps an http.ResponseWriter and, once Drain is
// called, silently discards subsequent Write / Flush calls. The
// underlying connection still closes when the handler returns; the
// goal is to suppress the library's "provider is closed" / "context
// canceled" error text that sse.Server.ServeHTTP writes via
// http.Error after our drain cancels its subscribe loop.
type drainableWriter struct {
	http.ResponseWriter
	drained atomic.Bool
}

func (d *drainableWriter) Write(b []byte) (int, error) {
	if d.drained.Load() {
		return len(b), nil
	}
	return d.ResponseWriter.Write(b)
}

func (d *drainableWriter) Flush() {
	if d.drained.Load() {
		return
	}
	if f, ok := d.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.NewResponseController find the underlying writer
// for SetWriteDeadline and similar passthrough operations.
func (d *drainableWriter) Unwrap() http.ResponseWriter {
	return d.ResponseWriter
}

// onSession is the *sse.Server.OnSession callback. It parses
// `?event_type=...` into the subscriber's topic list:
//   - Filterless subscribers join broadcastTopic — publishers send
//     every event there, so the subscriber sees everything.
//   - Filtered subscribers join the requested types — they receive
//     only matching events.
//
// Every subscriber also joins keepAliveTopic, which the keep-alive
// goroutine targets. That makes heartbeats reach filtered subscribers
// too (otherwise they'd be silently dropped by reverse-proxy idle
// timeouts while waiting for a matching event).
func (h *Hub) onSession(_ http.ResponseWriter, r *http.Request) (topics []string, allowed bool) {
	requested := r.URL.Query()["event_type"]
	if len(requested) == 0 {
		return []string{keepAliveTopic, broadcastTopic}, true
	}
	out := make([]string, 0, len(requested)+1)
	out = append(out, keepAliveTopic)
	out = append(out, requested...)
	return out, true
}

func (h *Hub) serveHTTP(w http.ResponseWriter, r *http.Request) {
	// Bypass the http.Server WriteTimeout for this connection. SSE is
	// long-lived; without this, the default WRITE_TIMEOUT (30s)
	// tears down healthy subscribers. ResponseController is the
	// stdlib-blessed way to override per-request deadlines (Go 1.20+).
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		// Tests using httptest.NewRecorder return ErrNotSupported;
		// production net/http servers accept the deadline fine.
		// Other errors are interesting enough to surface.
		httperr.WriteMgmt(w, http.StatusInternalServerError, "Internal Server Error",
			"sse: set write deadline: "+err.Error(), "sse_deadline_failed")
		return
	}

	// Promote resume hints to Last-Event-ID so the library handles
	// replay natively. Order: explicit header wins over ?from wins
	// over ?from_timestamp.
	if r.Header.Get("Last-Event-ID") == "" {
		if id := r.URL.Query().Get("from"); id != "" {
			r.Header.Set("Last-Event-ID", id)
		} else if ts := r.URL.Query().Get("from_timestamp"); ts != "" {
			t, err := parseFromTimestamp(ts)
			if err != nil {
				httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
					"from_timestamp must be RFC 3339: "+err.Error(),
					"invalid_from_timestamp")
				return
			}
			h.mu.RLock()
			replayer := h.replayer
			h.mu.RUnlock()
			if replayer != nil {
				if id, ok := replayer.IDBefore(t); ok {
					r.Header.Set("Last-Event-ID", id)
				} else if oldest := replayer.OldestID(); oldest != "" {
					// No stored event predates t, but the buffer
					// holds events newer than t — replay them by
					// resuming from the oldest stored ID. The
					// oldest event itself is skipped (the library
					// replays strictly after the given ID); see
					// recordingReplayer.OldestID for the trade-off.
					r.Header.Set("Last-Event-ID", oldest)
				}
				// Empty buffer: nothing to replay; subscriber joins
				// live.
			}
			// Replayer is nil: silently ignore, no replay possible.
		}
	}

	// Surface aged-out resume up-front: if Last-Event-ID (explicit or
	// promoted) names an ID we no longer carry, return 410 Gone
	// before opening the stream so the client doesn't silently miss
	// events. Matches the `410` declared in the OpenAPI spec.
	if id := r.Header.Get("Last-Event-ID"); id != "" {
		h.mu.RLock()
		replayer := h.replayer
		h.mu.RUnlock()
		if replayer != nil && !replayer.Has(id) {
			httperr.WriteMgmt(w, http.StatusGone, "Gone",
				"requested Last-Event-ID is no longer in the replay buffer",
				"event_aged_out")
			return
		}
	}

	// Register the request so Reset / Shutdown can cancel it. We
	// wrap the writer in drainableWriter so the library's late
	// http.Error call (after our cancel returns Subscribe with
	// context.Canceled) gets swallowed instead of leaking into the
	// SSE wire body.
	dw := &drainableWriter{ResponseWriter: w}
	ctx, cancel := registerSub(h, r, dw)
	defer cancel()
	r = r.WithContext(ctx)

	// Pre-flush SSE response headers so http.Client.Do returns
	// immediately, rather than blocking until the first event lands.
	dw.Header().Set("Content-Type", "text/event-stream")
	dw.Header().Set("Cache-Control", "no-cache")
	dw.Header().Set("Connection", "keep-alive")
	dw.WriteHeader(http.StatusOK)
	dw.Flush()

	h.mu.RLock()
	server := h.server
	h.mu.RUnlock()
	if server == nil {
		// Hub was Shutdown between the deadline disable and the
		// delegate. Nothing we can write that the library wouldn't
		// also try to write into the wire body; just return.
		return
	}
	server.ServeHTTP(dw, r)
}

// registerSub adds the request's cancellable context and drainable
// writer to the hub's active set, and returns the child context plus a
// cleanup func. The cleanup func cancels the context, drains the
// writer (suppressing any late library writes), and removes the entry
// from the active set. Callers must `defer cleanup()`.
func registerSub(h *Hub, r *http.Request, dw *drainableWriter) (context.Context, func()) {
	ctx, ctxCancel := context.WithCancel(r.Context())
	cancelAndDrain := func() {
		dw.drained.Store(true)
		ctxCancel()
	}
	h.activeMu.Lock()
	id := h.nextSub
	h.nextSub++
	h.active[id] = cancelAndDrain
	h.activeMu.Unlock()
	return ctx, func() {
		cancelAndDrain()
		h.activeMu.Lock()
		delete(h.active, id)
		h.activeMu.Unlock()
	}
}

// parseFromTimestamp parses an RFC 3339 string, tolerating the common
// case where the client didn't URL-encode the `+` in a timezone
// offset (e.g. `+00:00` arriving as ` 00:00` because Go's URL form
// decoder turns `+` into space). Tries the raw form first, then
// retries with the first space restored to `+`.
func parseFromTimestamp(ts string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, ts)
	if err == nil {
		return t, nil
	}
	if strings.Contains(ts, " ") {
		return time.Parse(time.RFC3339, strings.Replace(ts, " ", "+", 1))
	}
	return time.Time{}, err
}
