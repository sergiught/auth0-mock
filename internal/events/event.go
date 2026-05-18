// Package events owns the in-process Server-Sent Events hub for the
// mock's GET /events endpoint. The hub fans out events posted via
// POST /admin0/events to every connected subscriber whose event_type
// filter matches, backed by a bounded ring buffer that lets dropped
// subscribers resume via Last-Event-ID / ?from / ?from_timestamp.
//
// The package wraps github.com/tmaxmax/go-sse: sse.Server is the
// HTTP handler, sse.Joe is the in-memory pub/sub provider, and
// recordingReplayer (this package) adds the (id, timestamp) index
// needed to translate ?from_timestamp into an event ID for the
// library's Last-Event-ID-driven Replay path.
package events

import "encoding/json"

// Event is the wire shape the control plane pushes into the hub.
// Type is the CloudEvent discriminator (e.g. "user.created") and
// drives the SSE `event:` field. ID is the SSE `id:` field; the hub
// auto-generates one when this is empty. Payload is the raw JSON body
// streamed in the SSE `data:` field — it MUST include the same
// `type` value (the OpenAPI schema requires it as the oneOf
// discriminator), but this type doesn't enforce that; the
// /admin0/events handler validates against the schema before calling
// Hub.Publish.
type Event struct {
	Type    string
	ID      string
	Payload json.RawMessage
}
