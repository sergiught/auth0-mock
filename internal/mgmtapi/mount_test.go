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
    },
    "/widgets":{
      "post":{
        "operationId":"createWidget",
        "requestBody":{
          "required":true,
          "content":{"application/json":{"schema":{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}}}
        },
        "responses":{"201":{"description":"created","content":{"application/json":{"schema":{"type":"object","properties":{"id":{"type":"string"}}}}}}}
      }
    }
  }
}`

func newDeps(t *testing.T) (*spec.Spec, *spec.Validator, *matches.Store, *jwks.KeySet, chi.Router) {
	t.Helper()
	s, err := spec.Load([]byte(tinySpec))
	require.NoError(t, err)
	v := spec.NewValidator(s)
	store := matches.NewStore()
	ks, err := jwks.NewKeySet(jwks.Config{Issuer: "https://mock/", AccessTokenTTL: time.Hour})
	require.NoError(t, err)
	r := chi.NewRouter()
	return s, v, store, ks, r
}

func TestMount_RegistersOriginalRoute(t *testing.T) {
	s, v, store, ks, r := newDeps(t)
	log := zerolog.Nop()
	require.NoError(t, Mount(MountOpts{Router: r, Spec: s, Validator: v, Store: store, Keys: ks, Log: log}))

	// Without a registered match, the original endpoint should 401 (no bearer).
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v2/widgets/abc", nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// No /match or /reset siblings are registered any more.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v2/widgets/abc/match", nil))
	assert.Equal(t, http.StatusNotFound, w.Code, "sibling routes must not be registered")
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

	store.Put(matches.Expectation{Method: "GET", Path: "/api/v2/widgets/{id}", Kind: matches.KindTemplate,
		Response: matches.ResponseDef{Status: 200, Body: json.RawMessage(`{"id":"any"}`)}})
	store.Put(matches.Expectation{Method: "GET", Path: "/api/v2/widgets/abc", Kind: matches.KindExact,
		Response: matches.ResponseDef{Status: 200, Body: json.RawMessage(`{"id":"abc"}`)}})

	req := httptest.NewRequest("GET", "/api/v2/widgets/abc", nil)
	req.Header.Set("Authorization", "Bearer "+mintBearer(t, ks))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.JSONEq(t, `{"id":"abc"}`, w.Body.String())
}

func TestGeneric_RequestBodyMatcherWins(t *testing.T) {
	s, v, store, ks, r := newDeps(t)
	require.NoError(t, Mount(MountOpts{Router: r, Spec: s, Validator: v, Store: store, Keys: ks, Log: zerolog.Nop(), Strict: true}))

	store.Put(matches.Expectation{Method: "POST", Path: "/api/v2/widgets", Kind: matches.KindExact,
		Response: matches.ResponseDef{Status: 201, Body: json.RawMessage(`{"id":"catchall"}`)}})
	store.Put(matches.Expectation{Method: "POST", Path: "/api/v2/widgets", Kind: matches.KindExact,
		Request:  &matches.RequestMatcher{Body: json.RawMessage(`{"name":"specific"}`)},
		Response: matches.ResponseDef{Status: 201, Body: json.RawMessage(`{"id":"specific"}`)}})

	req := httptest.NewRequest("POST", "/api/v2/widgets", strings.NewReader(`{"name":"specific"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+mintBearer(t, ks))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, 201, w.Code, w.Body.String())
	assert.JSONEq(t, `{"id":"specific"}`, w.Body.String())
}
