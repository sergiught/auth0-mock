package admin0_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/api"
	"github.com/sergiught/auth0-mock/internal/admin0"
	"github.com/sergiught/auth0-mock/internal/events"
	"github.com/sergiught/auth0-mock/internal/spec"
)

// captureHub records every Publish call and Reset invocation.
// Replaces *events.Hub in tests via the EventsPublisher interface so
// the admin0 handler can be exercised without spinning up a real hub.
type captureHub struct {
	mu         sync.Mutex
	got        []events.Event
	resetCalls int
	active     int
	total      int
}

func (h *captureHub) ActiveSubscribers() int { return h.active }
func (h *captureHub) TotalSubscribers() int  { return h.total }

func (h *captureHub) Publish(e events.Event) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.got = append(h.got, e)
	return nil
}

func (h *captureHub) Reset(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.resetCalls++
	return nil
}

func newEventsRouter(t *testing.T, pub admin0.EventsPublisher) chi.Router {
	t.Helper()
	s, err := spec.Load(api.ManagementOpenAPIJSON)
	require.NoError(t, err)
	v, err := spec.NewValidator(s)
	require.NoError(t, err)
	r := chi.NewRouter()
	admin0.Mount(r, admin0.Deps{Validator: v, Events: pub})
	return r
}

// validUserCreatedBody is the smallest body that passes the Auth0
// event-stream envelope schema. Shared by the admin0 + reset tests so
// each case spells out only what it's exercising.
const validUserCreatedBody = `{
  "type":"user.created",
  "offset":"0",
  "event":{
    "specversion":"1.0",
    "type":"user.created",
    "source":"https://auth0.local/",
    "id":"evt_aaaaaaaaaaaaaaaa",
    "time":"2026-05-19T00:00:00Z",
    "a0tenant":"my-tenant",
    "a0stream":"est_aaaaaaaaaaaaaaaa",
    "data":{"object":{
      "user_id":"u-1",
      "email":"u@x.test",
      "created_at":"2026-05-19T00:00:00Z",
      "updated_at":"2026-05-19T00:00:00Z",
      "identities":[]
    }}
  }
}`

func TestPostAdmin0Events_AcceptsValidPayload(t *testing.T) {
	hub := &captureHub{}
	r := newEventsRouter(t, hub)

	req := httptest.NewRequest(http.MethodPost, "/admin0/events", bytes.NewReader([]byte(validUserCreatedBody)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.Len(t, hub.got, 1)
	assert.Equal(t, "user.created", hub.got[0].Type)
	assert.Equal(t, "evt_aaaaaaaaaaaaaaaa", hub.got[0].ID)
	assert.JSONEq(t, validUserCreatedBody, string(hub.got[0].Payload))
}

func TestPostAdmin0Events_RejectsInvalidJSON(t *testing.T) {
	hub := &captureHub{}
	r := newEventsRouter(t, hub)

	req := httptest.NewRequest(http.MethodPost, "/admin0/events", bytes.NewReader([]byte(`not json`)))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid_body")
	assert.Empty(t, hub.got, "publisher should not be called on bad body")
}

func TestPostAdmin0Events_RejectsSchemaViolation(t *testing.T) {
	hub := &captureHub{}
	r := newEventsRouter(t, hub)

	body := []byte(`{"type":"not.a.real.event","offset":"0","event":{}}`)
	req := httptest.NewRequest(http.MethodPost, "/admin0/events", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid_event")
	assert.Empty(t, hub.got)
}

func TestGetAdmin0EventsSubscribers_ReturnsCounts(t *testing.T) {
	hub := &captureHub{active: 2, total: 5}
	r := newEventsRouter(t, hub)

	req := httptest.NewRequest(http.MethodGet, "/admin0/events/subscribers", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"active":2,"total":5}`, rec.Body.String())
}

func TestReset_CallsEventsReset(t *testing.T) {
	hub := &captureHub{}
	r := chi.NewRouter()
	admin0.Mount(r, admin0.Deps{Events: hub})

	req := httptest.NewRequest(http.MethodPost, "/admin0/reset", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, 1, hub.resetCalls, "reset must drain SSE subscribers without destroying the hub")
}
