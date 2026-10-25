package mgmtapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/api"
	"github.com/sergiught/auth0-mock/internal/events"
	"github.com/sergiught/auth0-mock/internal/journal"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/spec"
)

type journalEnv struct {
	r     chi.Router
	store *matches.Store
	j     *journal.Journal
	token string
}

// newJournalEnv mounts the real Management spec behind a journal of the given
// size. A nil eventsH leaves GET /events on the generic handler.
func newJournalEnv(t *testing.T, size int, eventsH http.Handler) *journalEnv {
	t.Helper()
	s, err := spec.Load(api.ManagementOpenAPIJSON)
	require.NoError(t, err)
	v, err := spec.NewValidator(s)
	require.NoError(t, err)
	ks, err := jwks.NewKeySet(jwks.Config{Issuer: "https://mock/", AccessTokenTTL: time.Hour})
	require.NoError(t, err)
	store := matches.NewStore()
	j := journal.New(size)
	r := chi.NewRouter()
	r.Use(j.Middleware)
	require.NoError(t, Mount(MountOpts{Router: r, Spec: s, Validator: v, Store: store, Keys: ks,
		Log: zerolog.Nop(), EventsHandler: eventsH}))
	return &journalEnv{r: r, store: store, j: j, token: mintBearer(t, ks)}
}

func (e *journalEnv) do(method, path, body string, auth bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if auth {
		req.Header.Set("Authorization", "Bearer "+e.token)
	}
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	return w
}

func (e *journalEnv) noMatchMessage(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	var got struct {
		ErrorCode string `json:"errorCode"`
		Message   string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, "no_match", got.ErrorCode)
	return got.Message
}

func putUserStub(store *matches.Store, req *matches.RequestMatcher) matches.Expectation {
	return store.Put(matches.Expectation{Method: "PATCH", Path: "/api/v2/users/{id}", Kind: matches.KindTemplate,
		Request:  req,
		Response: matches.ResponseDef{Status: 200, Body: json.RawMessage(`{"user_id":"auth0|1"}`)}})
}

func TestNoMatch_HintNamesClosestFields(t *testing.T) {
	e := newJournalEnv(t, 10, nil)
	putUserStub(e.store, &matches.RequestMatcher{Body: json.RawMessage(`{"blocked":true}`)})

	w := e.do("PATCH", "/api/v2/users/auth0|1", `{"blocked":false}`, true)

	assert.Equal(t, "no registered match for PATCH /api/v2/users/auth0|1; closest stub differed on body.blocked",
		e.noMatchMessage(t, w))
}

func TestNoMatch_HintEllipsisOverThree(t *testing.T) {
	e := newJournalEnv(t, 10, nil)
	e.store.Put(matches.Expectation{Method: "GET", Path: "/api/v2/users/{id}", Kind: matches.KindTemplate,
		Request:  &matches.RequestMatcher{Query: map[string]string{"a": "1", "b": "2", "c": "3", "d": "4"}},
		Response: matches.ResponseDef{Status: 200, Body: json.RawMessage(`{"user_id":"auth0|1"}`)}})

	w := e.do("GET", "/api/v2/users/auth0|1", "", true)

	msg := e.noMatchMessage(t, w)
	assert.True(t, strings.HasSuffix(msg, ", …"), msg)
	assert.Equal(t, 3, strings.Count(msg, "query."), msg)
}

func TestNoMatch_HintNoStubs(t *testing.T) {
	e := newJournalEnv(t, 10, nil)

	w := e.do("GET", "/api/v2/users/auth0|1", "", true)

	assert.Equal(t, "no registered match for GET /api/v2/users/auth0|1; no stubs registered for GET /api/v2/users/{id}",
		e.noMatchMessage(t, w))
}

func TestNoMatch_HintWithJournalDisabled(t *testing.T) {
	e := newJournalEnv(t, 0, nil)
	putUserStub(e.store, &matches.RequestMatcher{Body: json.RawMessage(`{"blocked":true}`)})

	w := e.do("PATCH", "/api/v2/users/auth0|1", `{"blocked":false}`, true)

	assert.Contains(t, e.noMatchMessage(t, w), "closest stub differed on body.blocked")
	assert.Empty(t, e.j.List(journal.Filter{}))
}

func TestJournal_MatchedEntryCarriesExpectationID(t *testing.T) {
	e := newJournalEnv(t, 10, nil)
	exp := putUserStub(e.store, nil)

	w := e.do("PATCH", "/api/v2/users/auth0|1", `{"blocked":true}`, true)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	got := e.j.List(journal.Filter{})
	require.Len(t, got, 1)
	assert.Equal(t, journal.OutcomeMatched, got[0].Outcome)
	assert.Equal(t, exp.ID, got[0].ExpectationID)
	assert.Equal(t, "/api/v2/users/{id}", got[0].Operation)
}

func TestJournal_NoMatchEntryCarriesNearMisses(t *testing.T) {
	e := newJournalEnv(t, 10, nil)
	exp := putUserStub(e.store, &matches.RequestMatcher{Body: json.RawMessage(`{"blocked":true}`)})

	e.do("PATCH", "/api/v2/users/auth0|1", `{"blocked":false}`, true)

	got := e.j.List(journal.Filter{})
	require.Len(t, got, 1)
	assert.Equal(t, journal.OutcomeNoMatch, got[0].Outcome)
	assert.Equal(t, http.StatusNotFound, got[0].Status)
	require.NotEmpty(t, got[0].NearMisses)
	assert.Equal(t, exp.ID, got[0].NearMisses[0].ExpectationID)
}

func TestJournal_BearerRejectRecorded(t *testing.T) {
	e := newJournalEnv(t, 10, nil)

	w := e.do("GET", "/api/v2/users/auth0|1", "", false)
	require.Equal(t, http.StatusUnauthorized, w.Code)

	got := e.j.List(journal.Filter{})
	require.Len(t, got, 1)
	assert.Equal(t, http.StatusUnauthorized, got[0].Status)
	assert.Equal(t, journal.OutcomeRejected, got[0].Outcome)
	assert.Equal(t, "/api/v2/users/{id}", got[0].Operation)
}

func TestJournal_SpecValidationRejectRecorded(t *testing.T) {
	e := newJournalEnv(t, 10, nil)

	w := e.do("PATCH", "/api/v2/users/auth0|1", `{"blocked":"not-a-bool"}`, true)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	got := e.j.List(journal.Filter{})
	require.Len(t, got, 1)
	assert.Equal(t, http.StatusBadRequest, got[0].Status)
	assert.Equal(t, journal.OutcomeRejected, got[0].Outcome)
}

func TestJournal_SSEStreamRecordedWhileOpen(t *testing.T) {
	hub, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = hub.Shutdown(context.Background()) })
	e := newJournalEnv(t, 10, hub.Handler())
	srv := httptest.NewServer(e.r)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v2/events", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+e.token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	// The stream is still open here.
	got := e.j.List(journal.Filter{})
	require.Len(t, got, 1)
	assert.Equal(t, journal.OutcomeMatched, got[0].Outcome)
	assert.Empty(t, got[0].ExpectationID)
	assert.Equal(t, http.StatusOK, got[0].Status)
}

func TestJournal_SSERefusalRecordedAsRejected(t *testing.T) {
	hub, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = hub.Shutdown(context.Background()) })
	e := newJournalEnv(t, 10, hub.Handler())

	w := e.do("GET", "/api/v2/events?from_timestamp=garbage", "", true)
	require.Equal(t, http.StatusBadRequest, w.Code)

	got := e.j.List(journal.Filter{})
	require.Len(t, got, 1)
	assert.Equal(t, http.StatusBadRequest, got[0].Status)
	assert.Equal(t, journal.OutcomeRejected, got[0].Outcome)
}

func TestJournal_EventsErrorStubRecordedAsMatched(t *testing.T) {
	hub, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = hub.Shutdown(context.Background()) })
	e := newJournalEnv(t, 10, hub.Handler())
	exp := e.store.Put(matches.Expectation{Method: "GET", Path: "/api/v2/events", Kind: matches.KindExact,
		Response: matches.ResponseDef{Status: 401, Body: json.RawMessage(`{"statusCode":401,"error":"Unauthorized","message":"nope"}`)}})

	w := e.do("GET", "/api/v2/events", "", true)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())

	got := e.j.List(journal.Filter{})
	require.Len(t, got, 1)
	assert.Equal(t, journal.OutcomeMatched, got[0].Outcome)
	assert.Equal(t, exp.ID, got[0].ExpectationID)
}
