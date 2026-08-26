package events

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
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
//
// Reading r.URL.Query() here is safe despite it discarding its error:
// this runs as the delegate of serveHTTP, which has already rejected
// any query string that wouldn't parse. The signature above —
// (topics, allowed) — has no way to spell a 400, which is why that
// check lives in serveHTTP rather than here.
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

// validateQuery refuses the query shapes that parse cleanly and then
// quietly do the wrong thing. It returns false once it has written the
// response, so the caller stops.
//
// Everything below reads its parameters with q.Get, which cannot tell
// "absent" from "present but empty" and silently discards all but the
// first of a repeat. Left alone that turns a caller's typo into a
// working-looking subscription: an empty or duplicated `?from` joins
// live with no 410, and an empty `?event_type` subscribes to a topic
// nothing ever publishes to, so the stream connects and then never
// delivers. POST /admin0/events/expire refuses the same two shapes for
// the same reason; see internal/admin0/events.go.
func validateQuery(w http.ResponseWriter, q url.Values) bool {
	// A resume cursor can only mean one instant, so a repeat is a
	// caller bug rather than a list. `event_type` is deliberately
	// exempt: repeating it is how a caller asks for several types.
	for _, key := range []string{"from", "from_timestamp"} {
		if len(q[key]) > 1 {
			httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
				key+" was supplied more than once; a resume cursor can only name one position",
				"invalid_query")
			return false
		}
	}
	for _, param := range []struct{ key, code string }{
		{"from", "invalid_from"},
		{"from_timestamp", "invalid_from_timestamp"},
		{"event_type", "invalid_event_type"},
	} {
		if slices.Contains(q[param.key], "") {
			httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
				param.key+" was supplied but empty; omit it entirely to mean \"no "+
					param.key+"\", which is not the same request",
				param.code)
			return false
		}
	}
	return true
}

// promoteResumeHint copies a `?from` / `?from_timestamp` resume hint
// into Last-Event-ID so the replay buffer handles it on the normal
// resume path. Order: an explicit header wins over ?from, which wins
// over ?from_timestamp.
//
// The first return says whether WE synthesised the ID, so the caller
// doesn't 410 on it — its up-front Has check would otherwise race a
// concurrent Put that evicted the just-looked-up ID. The second is
// false when the response has already been written and the caller must
// stop; an unparseable ?from_timestamp is the only such case.
func (h *Hub) promoteResumeHint(
	w http.ResponseWriter, r *http.Request, q url.Values,
) (synthesised, ok bool) {
	if r.Header.Get("Last-Event-ID") != "" {
		return false, true
	}
	if id := q.Get("from"); id != "" {
		r.Header.Set("Last-Event-ID", id)
		return false, true
	}
	ts := q.Get("from_timestamp")
	if ts == "" {
		return false, true
	}
	t, err := parseFromTimestamp(ts)
	if err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			"from_timestamp must be RFC 3339: "+err.Error(),
			"invalid_from_timestamp")
		return false, false
	}
	h.mu.RLock()
	replayer := h.replayer
	h.mu.RUnlock()
	if replayer == nil {
		// No replay possible; silently ignore.
		return false, true
	}
	if id, found := replayer.IDBefore(t); found {
		r.Header.Set("Last-Event-ID", id)
		return true, true
	}
	if oldest := replayer.OldestID(); oldest != "" {
		// No stored event predates t, but the buffer holds events
		// newer than t — replay them by resuming from the oldest
		// stored ID. The oldest event itself is skipped (replay
		// starts strictly after the given ID); see
		// recordingReplayer.OldestID for the trade-off.
		r.Header.Set("Last-Event-ID", oldest)
		return true, true
	}
	// Empty buffer: nothing to replay; subscriber joins live.
	return false, true
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

	// Parse explicitly rather than via r.URL.Query(), which throws the
	// error away along with every pair it couldn't parse. A parameter
	// that fails to unescape would then not fail the request — it would
	// vanish, and the handler would proceed as if the caller never sent
	// it. Each of the three we read fails differently and silently: a
	// dropped ?from or ?from_timestamp skips the 410 gate below and
	// joins live, missing everything between the caller's cursor and
	// now; a dropped ?event_type leaves onSession with no requested
	// types, turning a filtered subscription into a firehose. One bad
	// pair rejects the whole request — answering 200 on the strength of
	// the pairs that did parse would serve a request nobody made.
	// POST /admin0/events/expire rejects the same way; see
	// internal/admin0/events.go.
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			"query string could not be parsed: "+err.Error(), "invalid_query")
		return
	}

	if !validateQuery(w, q) {
		return
	}

	synthesised, ok := h.promoteResumeHint(w, r, q)
	if !ok {
		return
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
	// Announce the connection the way Auth0's Events API does: a
	// :connected readiness comment plus a retry: reconnect hint, before
	// the library streams events. Both are non-events, so SSE readers
	// that skip comments / retry: ignore the frame.
	_, _ = dw.Write(h.connectFrame)
	dw.Flush()
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
