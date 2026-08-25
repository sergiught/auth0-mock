package auth0mock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// NewEventID returns a fresh event ID conforming to Auth0's
// event-stream `id` pattern (`evt_` + 16 alphanumeric chars). Tests
// that don't need a specific id value can call this instead of
// hand-rolling a 16-character placeholder — the schema validator
// rejects anything that doesn't match the pattern, and a too-short
// or too-long literal is the most common paste-and-go mistake.
func NewEventID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "evt_" + hex.EncodeToString(b)
}

// NewStreamID returns a fresh event-stream ID conforming to Auth0's
// `est_` + 16 alphanumeric chars pattern. Same rationale as
// NewEventID — saves callers from re-deriving "I need exactly 16
// chars after the prefix" by trial and error.
func NewStreamID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "est_" + hex.EncodeToString(b)
}

// EventsClient pushes events into the mock's SSE hub. Reach it via
// Client.Events. Push is fire-and-forget on the consumer side — the
// mock fans the event out to every currently-connected subscriber and
// records it in the bounded replay buffer for reconnect.
type EventsClient struct{ c *Client }

// SubscriberCount mirrors GET /admin0/events/subscribers.
//
// Active is how many subscribers are connected to GET /events right
// now; it is eventually-consistent — the hub drops a subscriber only
// when the server observes its connection close, so a reading taken
// immediately after a client disconnects may briefly lag. Total is how
// many have connected since the last /admin0/reset and never decreases
// within a window (handy for asserting reconnection behaviour). Active
// and Total increment together on connect, so once Active has settled
// after a connection event, Total has already counted it.
type SubscriberCount struct {
	Active int `json:"active"`
	Total  int `json:"total"`
}

// Subscribers reports the SSE hub's live and lifetime-within-window
// subscriber counts. Intended for tests that assert on connection
// lifecycle — e.g. "after closing my stream, active drops back to 0".
// Because Active is eventually-consistent, prefer
// auth0mocktest.WaitForActiveSubscribers when asserting a count
// settles rather than reading it once.
func (e *EventsClient) Subscribers(ctx context.Context) (SubscriberCount, error) {
	var sc SubscriberCount
	err := e.c.do(ctx, http.MethodGet, "/admin0/events/subscribers", nil, &sc)
	return sc, err
}

// Push POSTs an Auth0 event-stream envelope to /admin0/events.
// Payload is the full envelope ({type, offset, event:{...}}); the
// mock validates it against the OpenAPI
// EventStreamSubscribeEventsResponseContent schema at push time and
// returns *APIError with errorCode "invalid_event" on validation
// failure. The event's `type` (outer) drives `?event_type` filtering;
// the inner `event.id` is what subscribers see in the SSE `id:` line.
//
// The SDK deliberately keeps `payload` raw rather than enumerating
// every CloudEvent variant from Auth0's spec: callers stay in
// control, the schema is enforced server-side, and the SDK never
// becomes a translation hop that masks misshapen test data.
//
// Returns nil on 202 Accepted; *APIError on any non-2xx, decoded
// from the Auth0 error envelope by the shared transport helper.
func (e *EventsClient) Push(ctx context.Context, payload json.RawMessage) error {
	if len(payload) == 0 {
		return fmt.Errorf("auth0mock: events: Push: payload is required")
	}
	// Json.RawMessage marshals to itself — do() sends the bytes
	// verbatim without a re-encode round-trip.
	return e.c.do(ctx, http.MethodPost, "/admin0/events", payload, nil)
}

// expireEventsResponse is the reply from POST /admin0/events/expire:
// how many replay cursors the call dropped.
type expireEventsResponse struct {
	Expired int `json:"expired"`
}

// ExpireEvents ages out every cursor in the mock's replay buffer and
// reports how many it dropped. A subscriber that later resumes from one
// of those cursors — by Last-Event-ID or ?from — gets 410 Gone with
// errorCode "event_aged_out", exactly as real Auth0 does when a
// consumer's cursor falls out of the retention window.
//
// ?from_timestamp behaves differently, and deliberately so: it names an
// instant rather than a cursor, so the mock resolves it against the
// same index this call truncates and never has a client cursor to
// reject with a 410. After ExpireEvents there is nothing left to
// resolve against and such a subscriber joins live. After
// ExpireEventsBefore it resumes from the oldest surviving cursor
// instead, which — since replay starts strictly after the resolved id —
// skips the boundary event itself; assert on that one through
// Last-Event-ID or ?from.
//
// This is the deterministic way to test a consumer's cursor-loss
// handling. The alternatives are worse: pushing past the buffer's
// capacity is slow and couples the test to EVENTS_REPLAY_BUFFER, and
// Client.Reset is far blunter — it wipes every other store and
// disconnects subscribers too.
//
// Idempotent, and scoped to the replay buffer: subscribers that are
// already streaming keep receiving events, and events pushed after the
// call are buffered and resumable as usual.
func (e *EventsClient) ExpireEvents(ctx context.Context) (int, error) {
	var resp expireEventsResponse
	if err := e.c.do(ctx, http.MethodPost, "/admin0/events/expire", nil, &resp); err != nil {
		return 0, err
	}
	return resp.Expired, nil
}

// ExpireEventsBefore ages out every replay cursor older than cursor,
// leaving cursor itself — and everything after it — resumable. Use it
// to model partial cursor loss: a consumer resuming from an older
// offset gets 410 event_aged_out, while one resuming from cursor still
// replays the events that followed. Returns how many cursors it
// dropped.
//
// Forgiving in the same way as ExpectationsClient.ClearOp: a cursor the
// buffer no longer holds (or never held) expires nothing and reports 0
// rather than erroring, so repeat calls are safe. That also means the
// count cannot tell "nothing was older" from "never seen".
//
// Cursor is resolved to its first copy in the buffer, the same entry a
// resume from it would start at. Push unique offsets: with a duplicated
// one, "older than cursor" means older than the earliest copy, so this
// trims less than a caller holding the later copy expects. An empty cursor is
// rejected — expiring the whole buffer is ExpireEvents' job, and it
// should be spelled out rather than fallen into.
func (e *EventsClient) ExpireEventsBefore(ctx context.Context, cursor string) (int, error) {
	if cursor == "" {
		return 0, errors.New("auth0mock: events: ExpireEventsBefore: cursor is required (use ExpireEvents to expire the whole buffer)")
	}
	var resp expireEventsResponse
	path := "/admin0/events/expire?before=" + url.QueryEscape(cursor)
	if err := e.c.do(ctx, http.MethodPost, path, nil, &resp); err != nil {
		return 0, err
	}
	return resp.Expired, nil
}
