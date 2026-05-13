package authapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/jwks"
)

func TestAuthorize_RedirectsToCallback(t *testing.T) {
	r, _ := newAuthRouter(t)
	req := httptest.NewRequest("GET", "/authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&state=s1&response_type=code", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusFound, w.Code)
	loc := w.Header().Get("Location")
	assert.Contains(t, loc, "https://app/cb")
	assert.Contains(t, loc, "code=")
	assert.Contains(t, loc, "state=s1")
}

func TestAuthorize_MissingRedirectURI_400(t *testing.T) {
	r, _ := newAuthRouter(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/authorize?client_id=abc", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUserinfo_ReturnsClaims(t *testing.T) {
	r, ks := newAuthRouter(t)
	tok, err := ks.Mint(jwks.MintOpts{
		Subject:  "auth0|123",
		Audience: []string{"a"},
		TTL:      time.Hour,
		Extra:    map[string]any{"email": "a@x", "name": "Alice"},
	})
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "auth0|123", body["sub"])
	assert.Equal(t, "a@x", body["email"])
	assert.Equal(t, "Alice", body["name"])
}

func TestUserinfo_NoBearer_401(t *testing.T) {
	r, _ := newAuthRouter(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/userinfo", nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestLogout_RedirectsToReturnTo(t *testing.T) {
	r, _ := newAuthRouter(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/v2/logout?returnTo=https%3A%2F%2Fapp%2Fbye", nil))
	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "https://app/bye", w.Header().Get("Location"))
}

func TestRevoke_AlwaysReturns200(t *testing.T) {
	r, _ := newAuthRouter(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/oauth/revoke", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}
