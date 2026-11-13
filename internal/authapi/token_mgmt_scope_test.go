package authapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/permissions"
)

var testMgmtScopes = []string{"create:users", "read:users"}

func newMgmtScopeRouter(t *testing.T, issuer string, perms *permissions.Store) (chi.Router, *jwks.KeySet) {
	t.Helper()
	ks, err := jwks.NewKeySet(jwks.Config{Issuer: issuer, AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour})
	require.NoError(t, err)
	r := chi.NewRouter()
	Mount(Deps{
		Router: r, Keys: ks, Issuer: issuer, DefaultAudience: "https://mock/api/v2/",
		Log: zerolog.Nop(), Permissions: perms, ManagementScopes: testMgmtScopes,
	})
	return r, ks
}

func TestToken_ClientCredentials_ManagementAudienceGetsAllScopes(t *testing.T) {
	r, ks := newMgmtScopeRouter(t, "https://mock/", permissions.NewStore())

	body, c := postClientCredentials(t, r, ks, url.Values{"audience": []string{"https://mock/api/v2/"}})
	assert.Equal(t, "create:users read:users", c.Scope)
	assert.Equal(t, "create:users read:users", body.Scope)
}

func TestToken_ClientCredentials_ManagementAudienceFromDefaultAudience(t *testing.T) {
	r, _ := newMgmtScopeRouter(t, "https://mock/", permissions.NewStore())

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", "abc")
	form.Set("client_secret", "x")
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"scope":"create:users read:users"`)
}

func TestToken_ClientCredentials_ManagementAudienceRequestedScopeNarrows(t *testing.T) {
	r, ks := newMgmtScopeRouter(t, "https://mock/", permissions.NewStore())

	_, c := postClientCredentials(t, r, ks, url.Values{
		"audience": []string{"https://mock/api/v2/"}, "scope": []string{"read:users"},
	})
	assert.Equal(t, "read:users", c.Scope)
}

func TestToken_ClientCredentials_ManagementAudienceRegisteredPermissionsNarrow(t *testing.T) {
	perms := permissions.NewStore()
	perms.Set("https://mock/api/v2/", []string{"read:clients"})
	r, ks := newMgmtScopeRouter(t, "https://mock/", perms)

	_, c := postClientCredentials(t, r, ks, url.Values{"audience": []string{"https://mock/api/v2/"}})
	assert.Equal(t, "read:clients", c.Scope)
}

func TestToken_ClientCredentials_OtherAudienceGetsNoDefault(t *testing.T) {
	r, ks := newMgmtScopeRouter(t, "https://mock/", permissions.NewStore())

	_, c := postClientCredentials(t, r, ks, nil) // audience https://api/
	assert.Empty(t, c.Scope)
}

func TestToken_ClientCredentials_ManagementAudienceFollowsIssuerWithoutTrailingSlash(t *testing.T) {
	r, ks := newMgmtScopeRouter(t, "https://tenant.example", permissions.NewStore())

	_, c := postClientCredentials(t, r, ks, url.Values{"audience": []string{"https://tenant.example/api/v2/"}})
	assert.Equal(t, "create:users read:users", c.Scope)
}
