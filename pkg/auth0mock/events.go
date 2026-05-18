package auth0mock

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

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
