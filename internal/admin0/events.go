package admin0

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/render"

	"github.com/sergiught/auth0-mock/internal/events"
	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/spec"
)

// EventsPublisher is the seam between the /admin0/events handler and
// the SSE hub. The concrete implementation is *events.Hub; tests use
// fakes that record calls. Reset is on the interface because the
// ResetHandler drains the SSE state between tests via this hook
// (without permanently destroying the hub).
type EventsPublisher interface {
	Publish(events.Event) error
	Reset(context.Context) error
	// ActiveSubscribers / TotalSubscribers back GET
	// /admin0/events/subscribers so tests can observe the SSE
	// connection lifecycle (e.g. assert a stream closed cleanly).
	ActiveSubscribers() int
	TotalSubscribers() int
	// ExpireAll / ExpireBefore back POST /admin0/events/expire, the
	// narrow counterpart to Reset: they age out replay cursors and
	// nothing else. Two methods rather than one with an empty-string
	// sentinel, so "expire everything" can only ever be said on purpose.
	// ExpireBefore's second return says whether the buffer held the
	// cursor; the handler turns a false into 404 rather than a success
	// that expired nothing.
	ExpireAll() int
	ExpireBefore(cursor string) (dropped int, found bool)
}

// GetEventSubscribersHandler reports the SSE hub's live and
// lifetime-within-window subscriber counts. Intended for tests that
// assert on connection lifecycle — e.g. "after closing my stream,
// active drops back to 0". Active is eventually-consistent: the hub
// removes a subscriber when the server observes its connection close,
// so poll until it settles rather than asserting immediately.
type GetEventSubscribersHandler struct {
	Events EventsPublisher
}

type eventSubscribersResponse struct {
	Active int `json:"active"`
	Total  int `json:"total"`
}

func (h *GetEventSubscribersHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, eventSubscribersResponse{
		Active: h.Events.ActiveSubscribers(),
		Total:  h.Events.TotalSubscribers(),
	})
}

// ExpireEventsHandler ages out cursors in the SSE replay buffer so a
// test can provoke the 410 Gone / event_aged_out path on demand,
// instead of pushing past the buffer's capacity to force natural
// eviction or reaching for /admin0/reset (which also drops every other
// store and disconnects subscribers).
//
// `?before=<cursor>` expires everything older than that cursor and
// keeps the cursor itself resumable; without it the whole buffer goes.
// Subscribers that are already streaming are untouched — expiry only
// affects future resumes.
//
// Responds 200 with {"expired": <count>}: the number of cursors
// dropped. Repeating a `?before` call is safe and still answers 200 —
// the boundary cursor survives its own expiry, so the second call finds
// it and drops 0.
//
// A cursor the buffer doesn't hold is 404 / cursor_not_found rather
// than a 200 that expired nothing, because {"expired": 0} cannot say
// which of the two it meant. A test that mistypes an offset would
// otherwise be told its expiry worked and fail several steps later, on
// the reconnect that got 200 where it asserted 410. The same 404
// answers when the mock was started with replay disabled: there is no
// buffer holding that cursor either.
type ExpireEventsHandler struct {
	Events EventsPublisher
}

type expireEventsResponse struct {
	Expired int `json:"expired"`
}

func (h *ExpireEventsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Parse explicitly rather than via r.URL.Query(), which throws the
	// error away along with every pair it couldn't parse. A cursor
	// carrying a stray `%` or `;` would then disappear entirely, land on
	// the "no ?before at all" branch below, and expire the whole buffer
	// the caller meant to trim.
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			"query string could not be parsed: "+err.Error(), "invalid_query")
		return
	}
	// Only `before` is meaningful here, and every other spelling of it —
	// `?BEFORE=`, `?befor=`, a stray extra param — would otherwise land
	// on the "no ?before at all" branch and expire the whole buffer. A
	// typo must not be indistinguishable from omission when the two mean
	// opposite things.
	// Sorted: map iteration order is randomised, and an error message
	// naming a different key run to run is one nobody can assert on.
	unknown := slices.Sorted(maps.Keys(q))
	unknown = slices.DeleteFunc(unknown, func(key string) bool { return key == "before" })
	if len(unknown) > 0 {
		for i, key := range unknown {
			unknown[i] = strconv.Quote(key)
		}
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			"unknown query parameter(s) "+strings.Join(unknown, ", ")+`; only "before" is accepted`,
			"invalid_query")
		return
	}
	// Repeats are ambiguous: q.Get takes the first, so `?before=8&before=`
	// would trim from 8 while `?before=&before=8` would be rejected.
	// Refuse rather than let the outcome hinge on ordering.
	if len(q["before"]) > 1 {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			"before must be given at most once", "invalid_query")
		return
	}
	if !q.Has("before") {
		render.JSON(w, r, expireEventsResponse{Expired: h.Events.ExpireAll()})
		return
	}
	before := q.Get("before")
	// `?before=` present but empty is a different request from omitting
	// it: a caller interpolating an unset variable meant to name a
	// cursor, not to expire the whole buffer. Reject it rather than
	// silently widening the blast radius — the same guard the SDK's
	// ExpireEventsBefore applies.
	if before == "" {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			"before must not be empty; omit the parameter to expire the whole buffer",
			"invalid_before")
		return
	}
	dropped, found := h.Events.ExpireBefore(before)
	if !found {
		httperr.WriteMgmt(w, http.StatusNotFound, "Not Found",
			"before is not a cursor in the replay buffer: "+strconv.Quote(before),
			"cursor_not_found")
		return
	}
	render.JSON(w, r, expireEventsResponse{Expired: dropped})
}

// PostEventsHandler validates an incoming Auth0 event-stream envelope
// against the OpenAPI text/event-stream schema for GET /events and
// pushes it into the SSE hub. Responds 202 Accepted with
// {"id": "<inner-cloudevent-id>"} on success. Validation failures use
// the standard mgmt error envelope.
type PostEventsHandler struct {
	Events    EventsPublisher
	Validator *spec.Validator
}

// eventStreamEnvelope is a thin partial decode of the Auth0
// event-stream envelope that extracts just the routing fields (outer
// type + the offset cursor). Other fields are validated via the spec
// validator. Fields are exported so encoding/json populates them; they
// aren't part of the public API.
//
// Auth0's Events API uses the offset as the SSE id, the resume cursor a
// consumer echoes back via Last-Event-ID / ?from. Error messages carry
// no offset: they're terminal control frames, not a resume point, so
// they go out without an id and the hub never buffers them.
type eventStreamEnvelope struct {
	Type   string `json:"type"`
	Offset string `json:"offset"`
}

func (h *PostEventsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			"read body: "+err.Error(), "invalid_body")
		return
	}
	if !json.Valid(body) {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			"body is not valid JSON", "invalid_body")
		return
	}
	op, err := h.Validator.Resolve(http.MethodGet, "/api/v2/events")
	if err != nil {
		// Unreachable in practice — /events is in the embedded spec —
		// but guard anyway to surface a clear server error if the spec
		// is ever stripped down past this point.
		httperr.WriteMgmt(w, http.StatusInternalServerError, "Internal Server Error",
			"resolve /events: "+err.Error(), "spec_resolve_failed")
		return
	}
	if err := h.Validator.ValidateEventStreamPayload(op, http.StatusOK, body); err != nil {
		// Flatten kin-openapi's multi-line Schema/Value dump down to
		// `"/field": reason; "/other": reason` so the wire response
		// stays a single, scannable line.
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			spec.ConciseSchemaError(err), "invalid_event")
		return
	}
	var env eventStreamEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		// Defensive: ValidateEventStreamPayload already proved the
		// body is JSON. Distinct error code so this never gets
		// confused with a real schema-validation failure.
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			"decode envelope: "+err.Error(), "invalid_event_envelope")
		return
	}
	if err := h.Events.Publish(events.Event{
		Type:    env.Type,
		ID:      env.Offset,
		Payload: json.RawMessage(body),
	}); err != nil {
		httperr.WriteMgmt(w, http.StatusInternalServerError, "Internal Server Error",
			"publish: "+err.Error(), "publish_failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(struct {
		ID string `json:"id"`
	}{ID: env.Offset})
}
