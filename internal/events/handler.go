package events

import (
	"context"
	"errors"
	"net/http"
	"net/url"
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

// topicsKey addresses the subscriber's topic list on the request
// context. ServeHTTP computes it from the query string it has already
// parsed and validated; onSession reads it back. Passing the list
// rather than re-deriving it means the values that were validated and
// the values that are subscribed to cannot be two different things.
type topicsKey struct{}

// subscriptionTopics turns a validated `?event_type` list into the
// subscriber's topic list:
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
// A replayAll subscriber also joins replayAllTopic. Nothing is ever
// published there, so it changes nothing about what the subscription
// receives live; it is how the handler asks the replayer for the whole
// buffer, oldest event included — see replayAllTopic.
//
// Callers must validate first: validateResume refuses the requested
// types that would produce a subscription nobody wants, including the
// internal topic names, which would otherwise let `?event_type` name
// broadcastTopic and collect every event.
func subscriptionTopics(q url.Values, replayAll bool) []string {
	requested := q["event_type"]
	// +3: keepAliveTopic, optionally replayAllTopic, and either
	// broadcastTopic or the requested types.
	out := make([]string, 0, len(requested)+3)
	out = append(out, keepAliveTopic)
	if replayAll {
		out = append(out, replayAllTopic)
	}
	if len(requested) == 0 {
		return append(out, broadcastTopic)
	}
	return append(out, requested...)
}

// onSession is the *sse.Server.OnSession callback. ServeHTTP is the
// only route to it and always stores the topic list on the context, so
// the assertion cannot fail on any request a caller can send. If the
// wiring ever changes, a panic recovered into a 500 is the honest
// outcome: the alternative — returning allowed=false — makes go-sse
// return without writing, so the client sees the 200 and :connected
// frame serveHTTP already sent, then a bare EOF.
func (h *Hub) onSession(_ http.ResponseWriter, r *http.Request) (topics []string, allowed bool) {
	return r.Context().Value(topicsKey{}).([]string), true
}

// paddingReason reports why a present value is unusable as given, or
// "" when it is fine. Both cases are one accident — a template that
// produced whitespace — and both end somewhere that blames the wrong
// thing: an empty cursor joins live, and a padded one names a position
// no buffer holds, so it would be reported as aged out.
func paddingReason(key, value string) string {
	switch {
	case strings.TrimSpace(value) == "":
		return key + ` was supplied but empty; omit it entirely to mean "no ` + key +
			`", which is not the same request`
	case value != strings.TrimSpace(value):
		return key + " is padded with whitespace, so it names a position nothing holds"
	}
	return ""
}

// validateResume refuses the ways of naming a resume position that
// parse cleanly and then quietly do the wrong thing. It returns false
// once it has written the response, so the caller stops.
//
// Everything downstream reads these with Get, which cannot tell
// "absent" from "present but empty" and silently discards all but the
// first of a repeat. Left alone that turns a caller's typo into a
// working-looking subscription: an empty or duplicated `?from` joins
// live with no 410, and an empty `?event_type` subscribes to a topic
// nothing ever publishes to, so the stream connects and then never
// delivers. POST /admin0/events/expire refuses the same shapes for the
// same reason; see internal/admin0/events.go.
func validateResume(w http.ResponseWriter, r *http.Request, q url.Values) bool {
	// A cursor can be named three ways — this header, ?from and
	// ?from_timestamp — and every rule here has to cover all three, or
	// a client templating the one it missed still joins live.
	if !validateLastEventID(w, r) {
		return false
	}
	// One pass per key: a repeat is a caller bug rather than a list,
	// since a cursor can only name one position. `event_type` is
	// deliberately exempt — repeating it is how a caller asks for
	// several types — and is validated separately below.
	for _, param := range []struct{ key, code string }{
		{"from", "invalid_from"},
		{"from_timestamp", "invalid_from_timestamp"},
	} {
		values := q[param.key]
		if len(values) > 1 {
			httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
				param.key+" was supplied more than once; a resume cursor can only name one position",
				"invalid_query")
			return false
		}
		if len(values) == 1 {
			if reason := paddingReason(param.key, values[0]); reason != "" {
				httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", reason, param.code)
				return false
			}
		}
	}
	if !validateEventTypes(w, q) {
		return false
	}
	return validateFromTimestamp(w, q)
}

// validateLastEventID applies the same one-position and non-blank rules
// to the header spelling of a cursor. Header.Values rather than raw map
// indexing, so this neither depends on Go's canonical spelling nor
// indexes a slice a hand-built request could leave empty.
func validateLastEventID(w http.ResponseWriter, r *http.Request) bool {
	values := r.Header.Values("Last-Event-ID")
	if len(values) > 1 {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			"Last-Event-ID was sent more than once; a resume cursor can only name one position",
			"invalid_last_event_id")
		return false
	}
	if len(values) == 1 && strings.TrimSpace(values[0]) == "" {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			"Last-Event-ID was sent but empty; omit the header to join live",
			"invalid_last_event_id")
		return false
	}
	return true
}

// validateEventTypes refuses the requested types that would build a
// subscription nobody wants. Each value becomes a topic name verbatim,
// so anything that is not exactly a publishable event type yields a
// stream that connects and then never delivers — or, for the internal
// topic names, one that delivers far too much.
func validateEventTypes(w http.ResponseWriter, q url.Values) bool {
	for _, typ := range q["event_type"] {
		reason := paddingReason("event_type", typ)
		if reason == "" && (typ == broadcastTopic || typ == keepAliveTopic ||
			typ == barrierTopic || typ == replayAllTopic) {
			reason = "event_type names an internal topic; those carry the unfiltered " +
				"fan-out and are not event types"
		}
		if reason == "" {
			continue
		}
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", reason, "invalid_event_type")
		return false
	}
	return true
}

// validateFromTimestamp checks RFC 3339 syntax here rather than where
// the value is consumed. PromoteResumeHint returns early when `?from`
// or the header already named a cursor, so a malformed
// `?from_timestamp` beside either of those would otherwise never be
// looked at — leaving the empty value rejected and the garbage value
// silently accepted.
func validateFromTimestamp(w http.ResponseWriter, q url.Values) bool {
	ts := q.Get("from_timestamp")
	if ts == "" {
		return true
	}
	if _, err := parseFromTimestamp(ts); err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			"from_timestamp must be RFC 3339: "+err.Error(), "invalid_from_timestamp")
		return false
	}
	return true
}

// promoteResumeHint copies a `?from` / `?from_timestamp` resume hint
// into Last-Event-ID so the replay buffer handles it on the normal
// resume path. Order: an explicit header wins over ?from, which wins
// over ?from_timestamp.
//
// It reports whether WE synthesised the ID, so the caller doesn't 410
// on it — the caller's up-front Has check would otherwise race a
// concurrent Put that evicted the just-looked-up ID. It also reports
// whether the subscription should replay the whole buffer, which is
// the answer when `?from_timestamp` predates every buffered event and
// so cannot be expressed as a cursor at all.
//
// The replayer is passed in rather than read from the hub, so this and
// the caller's 410 gate judge the same buffer.
//
// Every value it reads has already been through validateResume, so
// there is nothing left here that can fail.
func promoteResumeHint(
	r *http.Request, q url.Values, replayer *recordingReplayer,
) (synthesised, replayAll bool) {
	if r.Header.Get("Last-Event-ID") != "" {
		return false, false
	}
	if id := q.Get("from"); id != "" {
		r.Header.Set("Last-Event-ID", id)
		return false, false
	}
	ts := q.Get("from_timestamp")
	if ts == "" {
		return false, false
	}
	// ValidateResume already rejected an unparseable value.
	t, _ := parseFromTimestamp(ts)
	if replayer == nil {
		// No replay possible; silently ignore.
		return false, false
	}
	if id, found := replayer.IDBefore(t); found {
		r.Header.Set("Last-Event-ID", id)
		return true, false
	}
	// No buffered event predates t, so every buffered event is one the
	// caller asked for — the oldest included. No cursor can say that,
	// since replay runs strictly after the cursor it is given, so say it
	// through the subscription's topics instead. An empty buffer takes
	// this branch too and replays nothing, which is the same join-live
	// outcome as before.
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

	if !validateResume(w, r, q) {
		return
	}

	// One snapshot for both the promotion and the 410 gate below. Each
	// used to take its own RLock and read h.replayer separately, so a
	// concurrent Reset landing between them would promote a cursor
	// against one buffer and check it against another.
	h.mu.RLock()
	replayer := h.replayer
	h.mu.RUnlock()

	synthesised, replayAll := promoteResumeHint(r, q, replayer)

	// Surface aged-out resume up-front: if a user-supplied
	// Last-Event-ID names an ID we no longer carry, return 410 Gone
	// before opening the stream so the client doesn't silently miss
	// events. Matches the `410` declared in the OpenAPI spec.
	// Synthesised IDs (looked up from the index moments ago) skip
	// this check — racing with a concurrent eviction would 410 a
	// user who never sent a Last-Event-ID, which is worse than the
	// fallback of joining live.
	//
	// A nil replayer (EVENTS_REPLAY_BUFFER <= 0) is the same answer, not
	// an exemption: with no buffer the cursor is certainly not in it, so
	// accepting the resume and joining live would be the silent miss
	// every rule above exists to prevent. POST /admin0/events/expire
	// already answers 404 for a cursor in this configuration.
	if id := r.Header.Get("Last-Event-ID"); id != "" && !synthesised {
		if replayer == nil || !replayer.Has(id) {
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
	// Hand the delegate the topics derived from the query string this
	// handler already parsed and validated, rather than leaving it to
	// re-parse and re-derive them.
	r = r.WithContext(context.WithValue(ctx, topicsKey{}, subscriptionTopics(q, replayAll)))

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
