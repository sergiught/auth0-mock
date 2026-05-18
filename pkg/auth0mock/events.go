package auth0mock

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Event is the wire shape POST /admin0/events expects. Type is the
// CloudEvent discriminator (e.g. "user.created"); ID is the SSE event
// ID surfaced to subscribers as `id:`; Payload is the full Auth0
// event-stream envelope ({type, offset, event:{...}}) — the mock
// validates it against the OpenAPI
// EventStreamSubscribeEventsResponseContent schema at push time and
// rejects misshapen bodies with 400 invalid_event. Callers stay in
// control of the envelope rather than the SDK enumerating every
// envelope variant from the spec.
//
// Type and ID are informational for callers (and for typed test
// assertions); the server reads both out of the Payload itself.
type Event struct {
	Type    string
	ID      string
	Payload json.RawMessage
}

// EventsClient pushes events into the mock's SSE hub. Reach it via
// Client.Events. Push is fire-and-forget on the consumer side — the
// mock fans the event out to every currently-connected subscriber and
// records it in the bounded replay buffer for reconnect.
type EventsClient struct{ c *Client }

// Push POSTs evt.Payload to /admin0/events. Returns *APIError on a
// non-2xx response, decoded from the Auth0 envelope by the shared
// transport helper.
func (e *EventsClient) Push(ctx context.Context, evt Event) error {
	if len(evt.Payload) == 0 {
		return fmt.Errorf("auth0mock: events: Push: Payload is required")
	}
	// Evt.Payload is a json.RawMessage, which marshals to itself —
	// passing it through do() sends the bytes on the wire verbatim
	// without a re-encode round-trip.
	return e.c.do(ctx, http.MethodPost, "/admin0/events", evt.Payload, nil)
}
