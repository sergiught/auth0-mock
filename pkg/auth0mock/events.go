package auth0mock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
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
