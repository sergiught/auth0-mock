package admin0

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/sergiught/auth0-mock/internal/events"
	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/spec"
)

// EventsPublisher is the seam between the /admin0/events handler and
// the SSE hub. The concrete implementation is *events.Hub; tests use
// fakes that record calls. Shutdown is on the interface because the
// ResetHandler also needs it (one-stop dependency).
type EventsPublisher interface {
	Publish(events.Event) error
	Shutdown(context.Context) error
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
// event-stream envelope that extracts just the routing fields
// (outer type + inner event.id). Other fields are validated via the
// spec validator. Fields are exported so encoding/json populates them;
// they aren't part of the public API.
type eventStreamEnvelope struct {
	Type  string `json:"type"`
	Event struct {
		ID string `json:"id"`
	} `json:"event"`
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
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request",
			err.Error(), "invalid_event")
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
		ID:      env.Event.ID,
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
	}{ID: env.Event.ID})
}
