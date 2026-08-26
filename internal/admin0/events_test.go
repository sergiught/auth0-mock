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
	// Expired records the cursor of every ExpireAll ("") / ExpireBefore
	// call; expireResult is what those calls report back.
	expired      []string
	expireResult int
	// ExpireNotFound makes ExpireBefore report the cursor as absent, the
	// input to the handler's 404. Negative so the zero value is the
	// ordinary found case that most tests want.
	expireNotFound bool
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

// expireAllMarker distinguishes an ExpireAll call from
// ExpireBefore(""). Recording both as "" would let a refactor that
// routed the omitted-parameter case through ExpireBefore — the method
// whose whole contract is that it can never mean "expire everything" —
// pass the tests unchanged.
const expireAllMarker = "\x00all"

func (h *captureHub) ExpireAll() int { return h.record(expireAllMarker) }

func (h *captureHub) ExpireBefore(cursor string) (int, bool) {
	return h.record(cursor), !h.expireNotFound
}

func (h *captureHub) record(before string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.expired = append(h.expired, before)
	return h.expireResult
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

// validOffsetOnlyBody is a progress marker: it advances the cursor without
// carrying event data.
const validOffsetOnlyBody = `{"type":"offset-only","offset":"7"}`

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

// An offset-only progress marker validates and publishes with its offset as
// the SSE id (it's a cursor-bearing message, unlike an error frame).
func TestPostAdmin0Events_OffsetOnlyMarker(t *testing.T) {
	hub := &captureHub{}
	r := newEventsRouter(t, hub)

	req := httptest.NewRequest(http.MethodPost, "/admin0/events", bytes.NewReader([]byte(validOffsetOnlyBody)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.Len(t, hub.got, 1)
	assert.Equal(t, "offset-only", hub.got[0].Type)
	assert.Equal(t, "7", hub.got[0].ID)
	assert.JSONEq(t, `{"id":"7"}`, rec.Body.String())
}

// Error frames are terminal control signals, not resumable events: they
// carry no offset, so they go out with no SSE id.
func TestPostAdmin0Events_ErrorPayloadHasNoCursorID(t *testing.T) {
	hub := &captureHub{}
	r := newEventsRouter(t, hub)

	req := httptest.NewRequest(http.MethodPost, "/admin0/events", bytes.NewReader([]byte(validErrorBody)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.Len(t, hub.got, 1)
	assert.Equal(t, "error", hub.got[0].Type)
	assert.Empty(t, hub.got[0].ID, "error frames are not a resume point — no SSE id")
	assert.JSONEq(t, validErrorBody, string(hub.got[0].Payload))
	assert.JSONEq(t, `{"id":""}`, rec.Body.String())
}

// TestPostAdmin0Events_ErrorPayloadBypassesReplayBuffer drives the real hub
// with the replay buffer enabled — the configuration where, before the fix, an
// id-less message was rejected by go-sse's FiniteReplayer and the push 500'd.
// The hub now delivers error frames live without buffering them.
func TestPostAdmin0Events_ErrorPayloadBypassesReplayBuffer(t *testing.T) {
	hub, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = hub.Shutdown(context.Background()) })
	r := newEventsRouter(t, hub)

	req := httptest.NewRequest(http.MethodPost, "/admin0/events", bytes.NewReader([]byte(validErrorBody)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"id":""}`, rec.Body.String())
}

// TestPostAdmin0Events_ErrorPayloadReachesSubscriber proves the issue's
// end-to-end goal: an error message POSTed to /admin0/events reaches a live
// GET /events subscriber as an `event: error` control frame (no id, since it's
// not a resume point). Before the fix the push 500'd and the subscriber saw
// nothing.
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
	readSSEFrame(t, br, 2*time.Second) // Consume the connect announcement frame.

	// Give the subscription a moment to register before pushing.
	time.Sleep(50 * time.Millisecond)

	pushResp, err := http.Post(srv.URL+"/admin0/events", "application/json", bytes.NewReader([]byte(validErrorBody)))
	require.NoError(t, err)
	_ = pushResp.Body.Close()
	require.Equal(t, http.StatusAccepted, pushResp.StatusCode)

	frame := readSSEFrame(t, br, 2*time.Second)
	assert.Contains(t, frame, "event: error")
	assert.Contains(t, frame, `"code":"cursor_expired"`)
	assert.NotContains(t, frame, "id:", "error frames carry no resume id")
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

func TestPostAdmin0EventsExpire_ExpiresWholeBuffer(t *testing.T) {
	hub := &captureHub{expireResult: 10}
	r := newEventsRouter(t, hub)

	req := httptest.NewRequest(http.MethodPost, "/admin0/events/expire", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"expired":10}`, rec.Body.String())
	assert.Equal(t, []string{expireAllMarker}, hub.expired,
		"no ?before must route through ExpireAll, not ExpireBefore(\"\")")
}

func TestPostAdmin0EventsExpire_BeforeCursor(t *testing.T) {
	hub := &captureHub{expireResult: 7}
	r := newEventsRouter(t, hub)

	req := httptest.NewRequest(http.MethodPost, "/admin0/events/expire?before=8", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"expired":7}`, rec.Body.String())
	assert.Equal(t, []string{"8"}, hub.expired)
}

// A cursor the buffer never held is 404, not a 200 that expired
// nothing: {"expired":0} would read as success to a test that mistyped
// an offset, which then fails much later on the reconnect it expected
// to see 410.
func TestPostAdmin0EventsExpire_UnknownCursorIsNotFound(t *testing.T) {
	hub := &captureHub{expireNotFound: true}
	r := newEventsRouter(t, hub)

	req := httptest.NewRequest(http.MethodPost, "/admin0/events/expire?before=nope", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"errorCode":"cursor_not_found"`)
	assert.Contains(t, rec.Body.String(), `\"nope\"`,
		"the rejected cursor is quoted back so a stray space or newline is visible")
	assert.Equal(t, []string{"nope"}, hub.expired,
		"the 404 comes from the hub's answer, not from a guess made before asking it")
}

// The boundary cursor survives its own expiry, so calling again with
// the same cursor still finds it and drops 0. Cleanup paths that expire
// twice must not start failing on the second call.
func TestPostAdmin0EventsExpire_RepeatBeforeStillSucceeds(t *testing.T) {
	hub := &captureHub{expireResult: 0}
	r := newEventsRouter(t, hub)

	for range 2 {
		req := httptest.NewRequest(http.MethodPost, "/admin0/events/expire?before=8", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.JSONEq(t, `{"expired":0}`, rec.Body.String())
	}
}

// Expiry is scoped to the replay buffer: it must not reach for the
// blunt Reset hook that drains subscribers and wipes the rest of the
// mock's state.
func TestPostAdmin0EventsExpire_DoesNotReset(t *testing.T) {
	hub := &captureHub{}
	r := newEventsRouter(t, hub)

	req := httptest.NewRequest(http.MethodPost, "/admin0/events/expire", nil)
	r.ServeHTTP(httptest.NewRecorder(), req)

	assert.Zero(t, hub.resetCalls)
	assert.Empty(t, hub.got)
}

// `?before=` with an empty value is not the same request as omitting
// the parameter: a caller interpolating an unset shell variable means
// to name a cursor, not to expire everything. Treating it as
// expire-all is the footgun the SDK refuses at
// ExpireEventsBefore; the HTTP surface has to refuse it too.
func TestPostAdmin0EventsExpire_RejectsEmptyBefore(t *testing.T) {
	hub := &captureHub{expireResult: 10}
	r := newEventsRouter(t, hub)

	req := httptest.NewRequest(http.MethodPost, "/admin0/events/expire?before=", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid_before")
	assert.Empty(t, hub.expired, "nothing should have been expired")
}

// A variable that expanded to a space is the same accident as one that
// expanded to nothing, and it must not fall through to a 404 that
// blames the cursor. GET /api/v2/events applies the identical rule to
// its own parameters, and the two endpoints document themselves as
// refusing the same shapes.
func TestPostAdmin0EventsExpire_RejectsWhitespaceBefore(t *testing.T) {
	hub := &captureHub{expireResult: 10}
	r := newEventsRouter(t, hub)

	req := httptest.NewRequest(http.MethodPost, "/admin0/events/expire?before=%20", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid_before")
	assert.Empty(t, hub.expired, "nothing should have been expired")
}

// r.URL.Query() discards pairs it cannot parse along with the error, so
// a cursor carrying a stray `%` or `;` would vanish and take the
// empty-value guard with it — landing on the "no ?before at all"
// branch and wiping the buffer the caller meant to trim.
func TestPostAdmin0EventsExpire_RejectsMalformedQuery(t *testing.T) {
	hub := &captureHub{expireResult: 10}
	r := newEventsRouter(t, hub)

	for _, raw := range []string{"before=a%2", "before=x;y"} {
		req := httptest.NewRequest(http.MethodPost, "/admin0/events/expire?"+raw, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equalf(t, http.StatusBadRequest, rec.Code, "query %q", raw)
		assert.Contains(t, rec.Body.String(), "invalid_query")
	}
	assert.Empty(t, hub.expired, "a query that failed to parse must not expire anything")
}

// A misspelled or wrong-case key is not "no ?before at all" — it is a
// caller who meant to name a cursor and typo'd. Falling through to
// expire-everything is the same silent blast-radius widening the empty
// and unparseable cases already reject.
func TestPostAdmin0EventsExpire_RejectsUnknownQueryKeys(t *testing.T) {
	for _, raw := range []string{"BEFORE=8", "befor=8", "before2=8", "before=8&extra=1"} {
		hub := &captureHub{expireResult: 42}
		r := newEventsRouter(t, hub)

		req := httptest.NewRequest(http.MethodPost, "/admin0/events/expire?"+raw, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equalf(t, http.StatusBadRequest, rec.Code, "query %q", raw)
		assert.Containsf(t, rec.Body.String(), "invalid_query", "query %q", raw)
		assert.Emptyf(t, hub.expired, "query %q must not expire anything", raw)
	}
}

// `?before=8&before=` is ambiguous — q.Get takes the first, so it would
// trim from 8, while `?before=&before=8` would 400. Reject repeats
// rather than letting the outcome depend on ordering.
func TestPostAdmin0EventsExpire_RejectsRepeatedBefore(t *testing.T) {
	for _, raw := range []string{"before=8&before=", "before=&before=8", "before=8&before=9"} {
		hub := &captureHub{expireResult: 42}
		r := newEventsRouter(t, hub)

		req := httptest.NewRequest(http.MethodPost, "/admin0/events/expire?"+raw, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equalf(t, http.StatusBadRequest, rec.Code, "query %q", raw)
		assert.Emptyf(t, hub.expired, "query %q must not expire anything", raw)
	}
}
