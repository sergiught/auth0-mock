package mgmtapi

import (
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

	"github.com/sergiught/auth0-mock/internal/journal"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/mgmtstore"
)

type storeEnv struct {
	r     chi.Router
	stubs *matches.Store
	j     *journal.Journal
	token string
}

func newStoreEnv(t *testing.T, withStore bool) *storeEnv {
	t.Helper()
	s, v := loadSpec(t)
	ks, err := jwks.NewKeySet(jwks.Config{Issuer: "https://mock/", AccessTokenTTL: 3600e9})
	require.NoError(t, err)
	stubs := matches.NewStore()
	j := journal.New(50)
	r := chi.NewRouter()
	r.Use(j.Middleware)
	opts := MountOpts{Router: r, Spec: s, Validator: v, Store: stubs, Keys: ks, Log: zerolog.Nop(), Strict: true}
	if withStore {
		opts.Mgmt = mgmtstore.NewEngine(s, v).NewStore(time.Now)
	}
	require.NoError(t, Mount(opts))
	return &storeEnv{r: r, stubs: stubs, j: j, token: mintBearer(t, ks)}
}

func (e *storeEnv) do(method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.token)
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	return w
}

func (e *storeEnv) createRole(t *testing.T) string {
	t.Helper()
	w := e.do("POST", "/api/v2/roles", `{"name":"r1"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return jsonField(t, w.Body.Bytes(), "id")
}

func TestStoreAnswersHandledOp(t *testing.T) {
	e := newStoreEnv(t, true)
	id := e.createRole(t)
	w := e.do("GET", "/api/v2/roles/"+id, "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "r1", jsonField(t, w.Body.Bytes(), "name"))
	entries := e.j.List(journal.Filter{})
	require.NotEmpty(t, entries)
	assert.Equal(t, journal.OutcomeStored, entries[len(entries)-1].Outcome)
}

func TestStoreIgnoresPlainStub(t *testing.T) {
	e := newStoreEnv(t, true)
	id := e.createRole(t)
	e.stubs.Put(matches.Expectation{Method: "GET", Path: "/api/v2/roles/{id}", Kind: matches.KindTemplate,
		Response: matches.ResponseDef{Status: 200, Body: []byte(`{"id":"x","name":"stub"}`)}})
	w := e.do("GET", "/api/v2/roles/"+id, "")
	assert.Equal(t, "r1", jsonField(t, w.Body.Bytes(), "name"))
	// An id the store never saw is 404 from the store, not the stub.
	w = e.do("GET", "/api/v2/roles/nope", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestStoreUnhandledUsesStubs(t *testing.T) {
	e := newStoreEnv(t, true)
	e.stubs.Put(matches.Expectation{Method: "GET", Path: "/api/v2/users/{id}/roles", Kind: matches.KindTemplate,
		Response: matches.ResponseDef{Status: 200, Body: []byte(`[]`)}})
	w := e.do("GET", "/api/v2/users/u1/roles", "")
	assert.Equal(t, http.StatusOK, w.Code)
	entries := e.j.List(journal.Filter{})
	assert.Equal(t, journal.OutcomeMatched, entries[len(entries)-1].Outcome)
}

func TestOverrideBeatsStore(t *testing.T) {
	e := newStoreEnv(t, true)
	id := e.createRole(t)
	e.stubs.Put(matches.Expectation{Method: "GET", Path: "/api/v2/roles/{id}", Kind: matches.KindTemplate, Override: true,
		Response: matches.ResponseDef{Status: 429, Body: []byte(`{"statusCode":429,"error":"Too Many Requests","message":"slow down"}`)}})
	w := e.do("GET", "/api/v2/roles/"+id, "")
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	// A non-matching path still reaches the store.
	e.stubs.ResetAll()
	assert.Equal(t, http.StatusOK, e.do("GET", "/api/v2/roles/"+id, "").Code)
}

func TestStoreOffOverrideIsPlain(t *testing.T) {
	e := newStoreEnv(t, false)
	e.stubs.Put(matches.Expectation{Method: "GET", Path: "/api/v2/roles/{id}", Kind: matches.KindTemplate, Override: true,
		Response: matches.ResponseDef{Status: 200, Body: []byte(`{"id":"x","name":"stub"}`)}})
	w := e.do("GET", "/api/v2/roles/abc", "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "stub", jsonField(t, w.Body.Bytes(), "name"))
	assert.Equal(t, http.StatusNotFound, e.do("POST", "/api/v2/roles", `{"name":"r"}`).Code)
}

func TestStoreValidatesFirst(t *testing.T) {
	e := newStoreEnv(t, true)
	w := e.do("POST", "/api/v2/roles", `{"name":123}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_request", jsonField(t, w.Body.Bytes(), "errorCode"))
	entries := e.j.List(journal.Filter{})
	assert.NotEqual(t, journal.OutcomeStored, entries[len(entries)-1].Outcome)
}

func jsonField(t *testing.T, body []byte, key string) string {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(body, &m), string(body))
	s, _ := m[key].(string)
	return s
}
