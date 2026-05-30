package events

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sergiught/auth0-mock/internal/httperr"
)

// drainableWriter wraps an http.ResponseWriter and, once markDrained
// is called, silently discards subsequent Write / Flush calls. The
// underlying connection still closes when the handler returns; the
// goal is to suppress the library's "provider is closed" / "context
// canceled" error text that sse.Server.ServeHTTP writes via
// http.Error after our drain cancels its subscribe loop.
//
// Writes are serialized through a mutex so the race detector sees a
// happens-before edge between any in-flight library write and the
// handler's return: markDrained acquires the same mutex, so once it
// returns no further write can reach the underlying writer, and
// net/http is free to close the connection without a concurrent
// touch.
type drainableWriter struct {
	http.ResponseWriter
	mu            sync.Mutex
	drained       bool
	headerWritten bool
}

func (d *drainableWriter) WriteHeader(code int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.drained || d.headerWritten {
		// Suppress late WriteHeader calls — typically the library's
		// http.Error(w, "context canceled", 500) on a normal client
		// disconnect. The pre-flush has already written 200; letting
		// the 500 reach the underlying writer flips the
		// statusRecorder's logged status and triggers a "superfluous
		// response.WriteHeader" warning from net/http.
		return
	}
	d.headerWritten = true
	d.ResponseWriter.WriteHeader(code)
}

func (d *drainableWriter) Write(b []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.drained {
		return len(b), nil
	}
	return d.ResponseWriter.Write(b)
}

func (d *drainableWriter) Flush() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.drained {
		return
	}
	if f, ok := d.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (d *drainableWriter) markDrained() {
	d.mu.Lock()
	d.drained = true
	d.mu.Unlock()
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
	// over ?from_timestamp. Track whether WE synthesised the ID so
	// we don't 410 on it (the up-front Has check below would race a
	// concurrent Put that evicted the just-looked-up ID).
	synthesised := false
	if r.Header.Get("Last-Event-ID") == "" {
		q := r.URL.Query()
		if id := q.Get("from"); id != "" {
			r.Header.Set("Last-Event-ID", id)
		} else if ts := q.Get("from_timestamp"); ts != "" {
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
					synthesised = true
				} else if oldest := replayer.OldestID(); oldest != "" {
					// No stored event predates t, but the buffer
					// holds events newer than t — replay them by
					// resuming from the oldest stored ID. The
					// oldest event itself is skipped (the library
					// replays strictly after the given ID); see
					// recordingReplayer.OldestID for the trade-off.
					r.Header.Set("Last-Event-ID", oldest)
					synthesised = true
				}
				// Empty buffer: nothing to replay; subscriber joins
				// live.
			}
			// Replayer is nil: silently ignore, no replay possible.
		}
	}

	// Surface aged-out resume up-front: if a user-supplied
	// Last-Event-ID names an ID we no longer carry, return 410 Gone
	// before opening the stream so the client doesn't silently miss
	// events. Matches the `410` declared in the OpenAPI spec.
	// Synthesised IDs (looked up from the index moments ago) skip
	// this check — racing with a concurrent eviction would 410 a
	// user who never sent a Last-Event-ID, which is worse than the
	// fallback of joining live.
	if id := r.Header.Get("Last-Event-ID"); id != "" && !synthesised {
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
// cleanup func. The cleanup func cancels the context, marks the writer
// as drained (suppressing any late library writes), and removes the
// entry from the active set. Callers must `defer cleanup()`.
func registerSub(h *Hub, r *http.Request, dw *drainableWriter) (context.Context, func()) {
	ctx, ctxCancel := context.WithCancel(r.Context())
	cancelAndDrain := func() {
		dw.markDrained()
		ctxCancel()
	}
	h.activeMu.Lock()
	id := h.nextSub
	h.nextSub++
	h.totalSubs.Add(1)
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
