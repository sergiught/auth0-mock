package mgmtapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/spec"
)

const tinySpec = `{
  "openapi": "3.0.0",
  "info":{"title":"t","version":"1"},
  "servers":[{"url":"http://x/api/v2"}],
  "paths":{
    "/widgets/{id}":{
      "get":{
        "operationId":"getWidget",
        "parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],
        "responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}}}}}
      }
    }
  }
}`

func newDeps(t *testing.T) (*spec.Spec, *spec.Validator, *matches.Store, *jwks.KeySet, *httprouter.Router) {
	t.Helper()
	s, err := spec.Load([]byte(tinySpec))
	require.NoError(t, err)
	v := spec.NewValidator(s)
	store := matches.NewStore()
	ks, err := jwks.NewKeySet(jwks.Config{Issuer: "https://mock/", AccessTokenTTL: time.Hour})
	require.NoError(t, err)
	r := httprouter.New()
	return s, v, store, ks, r
}

func TestMount_RegistersOriginalAndSiblingRoutes(t *testing.T) {
	s, v, store, ks, r := newDeps(t)
	log := zerolog.Nop()
	require.NoError(t, Mount(MountOpts{Router: r, Spec: s, Validator: v, Store: store, Keys: ks, Log: log}))

	// Without a registered match, the original endpoint should 401 (no bearer).
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v2/widgets/abc", nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// /match should be reachable without a bearer (verb mirrors original = GET).
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v2/widgets/abc/match", nil))
	assert.NotEqual(t, http.StatusNotFound, w.Code, "match route should be registered")

	// /reset should be reachable without a bearer (verb mirrors original = GET).
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v2/widgets/abc/reset", nil))
	assert.NotEqual(t, http.StatusNotFound, w.Code, "reset route should be registered")
}

func mintBearer(t *testing.T, ks *jwks.KeySet) string {
	t.Helper()
	tok, err := ks.Mint(jwks.MintOpts{Subject: "test", Audience: []string{"a"}, TTL: time.Hour})
	require.NoError(t, err)
	return tok
}

func TestGeneric_NoMatch_404(t *testing.T) {
	s, v, store, ks, r := newDeps(t)
	require.NoError(t, Mount(MountOpts{Router: r, Spec: s, Validator: v, Store: store, Keys: ks, Log: zerolog.Nop(), Strict: true}))

	req := httptest.NewRequest("GET", "/api/v2/widgets/abc", nil)
	req.Header.Set("Authorization", "Bearer "+mintBearer(t, ks))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, 404, w.Code)
	assert.True(t, strings.Contains(w.Body.String(), `"statusCode":404`))
}

func TestGeneric_ExactMatchWins(t *testing.T) {
	s, v, store, ks, r := newDeps(t)
	require.NoError(t, Mount(MountOpts{Router: r, Spec: s, Validator: v, Store: store, Keys: ks, Log: zerolog.Nop(), Strict: true}))

	store.Put(matches.Match{Method: "GET", Path: "/api/v2/widgets/{id}", Kind: matches.KindTemplate, Status: 200, Body: json.RawMessage(`{"id":"any"}`)})
	store.Put(matches.Match{Method: "GET", Path: "/api/v2/widgets/abc", Kind: matches.KindExact, Status: 200, Body: json.RawMessage(`{"id":"abc"}`)})

	req := httptest.NewRequest("GET", "/api/v2/widgets/abc", nil)
	req.Header.Set("Authorization", "Bearer "+mintBearer(t, ks))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.JSONEq(t, `{"id":"abc"}`, w.Body.String())
}

func TestMatchHandler_RegistersValid(t *testing.T) {
	s, v, store, ks, r := newDeps(t)
	require.NoError(t, Mount(MountOpts{Router: r, Spec: s, Validator: v, Store: store, Keys: ks, Log: zerolog.Nop(), Strict: true}))

	body := `{"status":200,"body":{"id":"abc"}}`
	req := httptest.NewRequest("GET", "/api/v2/widgets/abc/match", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, 204, w.Code)
	stored := store.Find("GET", "/api/v2/widgets/abc", "/api/v2/widgets/{id}")
	if assert.NotNil(t, stored) {
		assert.Equal(t, 200, stored.Status)
		assert.JSONEq(t, `{"id":"abc"}`, string(stored.Body))
	}
}

func TestMatchHandler_RejectsInvalid(t *testing.T) {
	s, v, store, ks, r := newDeps(t)
	require.NoError(t, Mount(MountOpts{Router: r, Spec: s, Validator: v, Store: store, Keys: ks, Log: zerolog.Nop(), Strict: true}))

	body := `{"status":200,"body":{"unrelated":true}}`
	req := httptest.NewRequest("GET", "/api/v2/widgets/abc/match", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
	assert.Empty(t, store.List())
}

func TestMatchHandler_TemplateRegistration(t *testing.T) {
	s, v, store, ks, r := newDeps(t)
	require.NoError(t, Mount(MountOpts{Router: r, Spec: s, Validator: v, Store: store, Keys: ks, Log: zerolog.Nop(), Strict: true}))

	body := `{"status":200,"body":{"id":"any"}}`
	req := httptest.NewRequest("GET", "/api/v2/widgets/{id}/match", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, 204, w.Code)
	stored := store.Find("GET", "/api/v2/widgets/zzz", "/api/v2/widgets/{id}")
	require.NotNil(t, stored)
	assert.Equal(t, matches.KindTemplate, stored.Kind)
}
