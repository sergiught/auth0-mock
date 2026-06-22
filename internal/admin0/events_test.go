package admin0_test

import (
	"bufio"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

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

// validErrorBody is the smallest error-message envelope that passes the
// event-stream schema. It has no `event` wrapper (so no event.id), only an
// `error` wrapper carrying the resume offset.
const validErrorBody = `{
  "type":"error",
  "error":{
    "code":"cursor_expired",
    "message":"cursor expired; resync from the supplied offset",
    "offset":"42"
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
	// Faithful to Auth0's Events API: the SSE id is the offset (the resume
	// cursor), not the CloudEvent event.id.
	assert.Equal(t, "0", hub.got[0].ID)
	assert.JSONEq(t, validUserCreatedBody, string(hub.got[0].Payload))
	assert.JSONEq(t, `{"id":"0"}`, rec.Body.String())
}

func TestPostAdmin0Events_ErrorPayloadUsesOffsetAsID(t *testing.T) {
	hub := &captureHub{}
	r := newEventsRouter(t, hub)

	req := httptest.NewRequest(http.MethodPost, "/admin0/events", bytes.NewReader([]byte(validErrorBody)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.Len(t, hub.got, 1)
	assert.Equal(t, "error", hub.got[0].Type)
	// Error payloads carry no event.id; error.offset stands in as the SSE
	// message ID so cursor semantics hold on resume.
	assert.Equal(t, "42", hub.got[0].ID)
	assert.JSONEq(t, validErrorBody, string(hub.got[0].Payload))
	assert.JSONEq(t, `{"id":"42"}`, rec.Body.String())
}

// TestPostAdmin0Events_ErrorPayloadAcceptedByReplayBuffer drives the real hub
// with the replay buffer enabled — the exact configuration where an ID-less
// message is rejected by go-sse's FiniteReplayer and the push 500s.
func TestPostAdmin0Events_ErrorPayloadAcceptedByReplayBuffer(t *testing.T) {
	hub, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = hub.Shutdown(context.Background()) })
	r := newEventsRouter(t, hub)

	req := httptest.NewRequest(http.MethodPost, "/admin0/events", bytes.NewReader([]byte(validErrorBody)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"id":"42"}`, rec.Body.String())
}

// TestPostAdmin0Events_ErrorPayloadReachesSubscriber proves the issue's
// end-to-end goal: an error message POSTed to /admin0/events reaches a live,
// filterless GET /events subscriber carrying error.offset as its SSE id — so a
// worker can read it and resume from that cursor. Without the offset fallback
// the push 500s and the subscriber sees nothing.
func TestPostAdmin0Events_ErrorPayloadReachesSubscriber(t *testing.T) {
	hub, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = hub.Shutdown(context.Background()) })

	r := newEventsRouter(t, hub)
	r.Get("/events", hub.Handler().ServeHTTP)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	subReq, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/events", nil)
	require.NoError(t, err)
	subResp, err := http.DefaultClient.Do(subReq)
	require.NoError(t, err)
	t.Cleanup(func() { _ = subResp.Body.Close() })
	require.Equal(t, http.StatusOK, subResp.StatusCode)
	require.Contains(t, subResp.Header.Get("Content-Type"), "text/event-stream")
	br := bufio.NewReader(subResp.Body)

	// Give the subscription a moment to register before pushing.
	time.Sleep(50 * time.Millisecond)

	pushResp, err := http.Post(srv.URL+"/admin0/events", "application/json", bytes.NewReader([]byte(validErrorBody)))
	require.NoError(t, err)
	_ = pushResp.Body.Close()
	require.Equal(t, http.StatusAccepted, pushResp.StatusCode)

	frame := readSSEFrame(t, br, 2*time.Second)
	assert.Contains(t, frame, "id: 42", "offset must surface as the SSE id")
	assert.Contains(t, frame, "event: error")
	assert.Contains(t, frame, `"code":"cursor_expired"`)
}

// readSSEFrame reads one SSE frame (up to the blank-line terminator) from r,
// failing the test if none arrives within d.
func readSSEFrame(t *testing.T, r *bufio.Reader, d time.Duration) string {
	t.Helper()
	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				done <- b.String()
				return
			}
			b.WriteString(line)
			if line == "\n" || line == "\r\n" {
				done <- b.String()
				return
			}
		}
	}()
	select {
	case f := <-done:
		return f
	case <-time.After(d):
		t.Fatalf("timeout waiting for SSE frame")
		return ""
	}
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
