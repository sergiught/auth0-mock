package mgmtapi

import (
	"net/http"
	"net/http/httptest"
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

const scopedSpec = `{
  "openapi": "3.0.0",
  "info":{"title":"t","version":"1"},
  "servers":[{"url":"http://x/api/v2"}],
  "components":{"securitySchemes":{"bearerAuth":{"type":"http","scheme":"bearer"},"oAuth2ClientCredentials":{"type":"oauth2","flows":{"clientCredentials":{"tokenUrl":"/oauth/token","scopes":{"read:things":"r","read:all_things":"r"}}}}}},
  "paths":{
    "/things":{
      "get":{
        "operationId":"listThings",
        "security":[{"bearerAuth":[]},{"oAuth2ClientCredentials":["read:things","read:all_things"]}],
        "responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object"}}}}}
      }
    }
  }
}`

func scopedRouter(t *testing.T, requireScopes bool) (chi.Router, *jwks.KeySet) {
	t.Helper()
	s, err := spec.Load([]byte(scopedSpec))
	require.NoError(t, err)
	v, err := spec.NewValidator(s)
	require.NoError(t, err)
	ks, err := jwks.NewKeySet(jwks.Config{Issuer: "https://mock/", AccessTokenTTL: time.Hour})
	require.NoError(t, err)
	r := chi.NewRouter()
	require.NoError(t, Mount(MountOpts{
		Router: r, Spec: s, Validator: v, Store: matches.NewStore(), Keys: ks,
		Log: zerolog.Nop(), RequireScopes: requireScopes,
	}))
	return r, ks
}

func getThings(t *testing.T, r chi.Router, ks *jwks.KeySet, scope string) *httptest.ResponseRecorder {
	t.Helper()
	tok, err := ks.Mint(jwks.MintOpts{Subject: "a@clients", Audience: []string{"https://mock/api/v2/"}, Scope: scope, TTL: time.Hour})
	require.NoError(t, err)
	req := httptest.NewRequest("GET", "/api/v2/things", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestMount_RequireScopes_RejectsBeforeStubMatching(t *testing.T) {
	r, ks := scopedRouter(t, true)
	// With the scope the request reaches stub matching (no stub: 404 no_match).
	assert.Equal(t, http.StatusNotFound, getThings(t, r, ks, "read:all_things").Code)
	// Without it the request is stopped with 403 first.
	w := getThings(t, r, ks, "read:other")
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.JSONEq(t, `{"statusCode":403,"error":"Forbidden","message":"Insufficient scope, expected any of: read:things,read:all_things","errorCode":"insufficient_scope"}`, w.Body.String())
}

func TestMount_RequireScopes_DisabledSkipsCheck(t *testing.T) {
	r, ks := scopedRouter(t, false)
	assert.Equal(t, http.StatusNotFound, getThings(t, r, ks, "read:other").Code)
}
