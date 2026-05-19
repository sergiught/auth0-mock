package mgmtapi

import (
	"encoding/json"
	"errors"
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
	v, err := spec.NewValidator(s)
	require.NoError(t, err)
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

// TestMount_UnknownMgmtPathReturnsJSONEnvelope locks the consistency
// guarantee: every /api/v2/* response uses the Auth0 error envelope,
// including 404 / 405 fallbacks. Before this fix, chi's default
// text/plain "404 page not found" leaked through for unknown
// /api/v2 paths, breaking SDK error-handling logic.
func TestMount_UnknownMgmtPathReturnsJSONEnvelope(t *testing.T) {
	s, v, store, ks, r := newDeps(t)
	require.NoError(t, Mount(MountOpts{Router: r, Spec: s, Validator: v, Store: store, Keys: ks, Log: zerolog.Nop()}))

	cases := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantCode   string
	}{
		{
			name:   "unknown Mgmt path → 404 JSON envelope",
			method: "GET", path: "/api/v2/totally-unknown",
			wantStatus: http.StatusNotFound,
			wantCode:   `"errorCode":"unknown_operation"`,
		},
		{
			name:   "unknown method on known path → 405 JSON envelope",
			method: "PATCH", path: "/api/v2/widgets/abc",
			wantStatus: http.StatusMethodNotAllowed,
			wantCode:   `"errorCode":"method_not_allowed"`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(c.method, c.path, nil))
			assert.Equal(t, c.wantStatus, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"),
				"Mgmt-API errors must be JSON, never text/plain")
			assert.Contains(t, w.Body.String(), c.wantCode)
		})
	}

	// Non-Mgmt paths still get chi's default — those belong to other
	// mounts (admin0, auth API) that 404 themselves.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/random/nothing", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.NotContains(t, w.Body.String(), "errorCode",
		"non-Mgmt 404s stay as chi's default; Mgmt envelope is scoped to /api/v2/")
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

func TestIsRouteConflict(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"unrelated", errors.New("validation failed"), false},
		{"conflict keyword", errors.New("chi: route conflict on /api/v2/users"), true},
		{"existing pattern", errors.New("chi detected an existing pattern matching this route"), true},
		{"duplicate keyword", errors.New("duplicate handler for POST /api/v2/x"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, isRouteConflict(tc.err))
		})
	}
}

func TestMount_SubscribeEvents_UsesEventsHandlerInsteadOfGeneric(t *testing.T) {
	called := false
	eventsH := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	r := chi.NewRouter()
	s, err := spec.Load(api.ManagementOpenAPIJSON)
	require.NoError(t, err)
	v, err := spec.NewValidator(s)
	require.NoError(t, err)
	ks, err := jwks.NewKeySet(jwks.Config{Issuer: "https://mock/", AccessTokenTTL: time.Hour})
	require.NoError(t, err)
	require.NoError(t, Mount(MountOpts{
		Router:        r,
		Spec:          s,
		Validator:     v,
		Store:         matches.NewStore(),
		Keys:          ks,
		Log:           zerolog.Nop(),
		EventsHandler: eventsH,
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v2/events", nil)
	req.Header.Set("Authorization", "Bearer "+mintBearer(t, ks))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, called, "events handler should be invoked")
}

func TestMount_SubscribeEvents_Without_Bearer_Is_401(t *testing.T) {
	eventsH := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	r := chi.NewRouter()
	s, err := spec.Load(api.ManagementOpenAPIJSON)
	require.NoError(t, err)
	v, err := spec.NewValidator(s)
	require.NoError(t, err)
	ks, err := jwks.NewKeySet(jwks.Config{Issuer: "https://mock/", AccessTokenTTL: time.Hour})
	require.NoError(t, err)
	require.NoError(t, Mount(MountOpts{
		Router: r, Spec: s, Validator: v, Store: matches.NewStore(),
		Keys: ks, Log: zerolog.Nop(), EventsHandler: eventsH,
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v2/events", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
