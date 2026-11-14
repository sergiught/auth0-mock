package authapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/mfa"
	"github.com/sergiught/auth0-mock/internal/pkce"
	"github.com/sergiught/auth0-mock/internal/session"
)

const grantPasswordRealm = "http://auth0.com/oauth/grant-type/password-realm"

func newOfflineRouter(t *testing.T, strict bool) (chi.Router, *pkce.Store, *mfa.Store) {
	t.Helper()
	ks, err := jwks.NewKeySet(jwks.Config{Issuer: "https://mock/", AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour})
	require.NoError(t, err)
	store := pkce.NewStore()
	ms := mfa.NewStore()
	r := chi.NewRouter()
	Mount(Deps{
		Router: r, Keys: ks, Issuer: "https://mock/", DefaultAudience: "https://mock/api/v2/",
		Log: zerolog.Nop(), PKCE: store, Sessions: session.NewStore(), MFA: ms,
		TokenEndpointStrict: strict,
	})
	return r, store, ms
}

// offlineForms returns one token request per non-refresh grant, sending scope.
func offlineForms(store *pkce.Store, storedScope, scope string) map[string]url.Values {
	store.Put("code-"+scope, pkce.Entry{ClientID: "demo", Redirect: "https://app/cb", Scope: storedScope})
	return map[string]url.Values{
		"password": {
			"grant_type": {"password"}, "client_id": {"demo"}, "username": {"alice@example.com"},
			"password": {"x"}, "scope": {scope},
		},
		"password-realm": {
			"grant_type": {grantPasswordRealm}, "client_id": {"demo"}, "username": {"alice@example.com"},
			"password": {"x"}, "realm": {"Username-Password-Authentication"}, "scope": {scope},
		},
		"passwordless": {
			"grant_type": {grantPasswordlessOTP}, "client_id": {"demo"}, "realm": {"email"},
			"username": {"a@x"}, "otp": {"000000"}, "scope": {scope},
		},
		"authorization_code": {
			"grant_type": {"authorization_code"}, "code": {"code-" + scope}, "client_id": {"demo"},
			"redirect_uri": {"https://app/cb"}, "scope": {scope},
		},
	}
}

func rawBody(t *testing.T, r chi.Router, form url.Values) map[string]any {
	t.Helper()
	w := postTokenForm(r, form)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var m map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &m))
	return m
}

func TestOfflineAccess_Strict_OmitsRefreshToken(t *testing.T) {
	r, store, _ := newOfflineRouter(t, true)
	for name, form := range offlineForms(store, "openid profile", "openid profile") {
		t.Run(name, func(t *testing.T) {
			m := rawBody(t, r, form)
			assert.NotContains(t, m, "refresh_token")
			assert.Contains(t, m, "access_token")
		})
	}
}

func TestOfflineAccess_Strict_KeepsRefreshToken(t *testing.T) {
	r, store, _ := newOfflineRouter(t, true)
	const scope = "openid  offline_access profile"
	for name, form := range offlineForms(store, scope, scope) {
		t.Run(name, func(t *testing.T) {
			assert.NotEmpty(t, rawBody(t, r, form)["refresh_token"])
		})
	}
}

func TestOfflineAccess_Strict_CodeScopeWins(t *testing.T) {
	r, store, _ := newOfflineRouter(t, true)
	forms := offlineForms(store, "openid offline_access", "openid")
	assert.NotEmpty(t, rawBody(t, r, forms["authorization_code"])["refresh_token"])

	r, store, _ = newOfflineRouter(t, true)
	forms = offlineForms(store, "openid", "openid offline_access")
	assert.NotContains(t, rawBody(t, r, forms["authorization_code"]), "refresh_token")
}

func TestOfflineAccess_Strict_MFAGrant(t *testing.T) {
	r, _, ms := newOfflineRouter(t, true)
	ms.SetRequired(true)
	w := postTokenForm(r, url.Values{
		"grant_type": {"password"}, "client_id": {"demo"}, "username": {"alice@example.com"},
		"password": {"x"}, "scope": {"openid"},
	})
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	var ch map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &ch))
	tok, _ := ch["mfa_token"].(string)
	require.NotEmpty(t, tok)

	m := rawBody(t, r, url.Values{
		"grant_type": {grantMFAOTP}, "client_id": {"demo"}, "mfa_token": {tok}, "otp": {mfa.AcceptedOTP},
	})
	assert.NotContains(t, m, "refresh_token")
	assert.Contains(t, m, "access_token")
}

func TestOfflineAccess_Strict_RefreshGrantAlwaysRotates(t *testing.T) {
	r, _, _ := newOfflineRouter(t, true)
	login := rawBody(t, r, url.Values{
		"grant_type": {"password"}, "client_id": {"demo"}, "username": {"alice@example.com"},
		"password": {"x"}, "scope": {"openid offline_access"},
	})
	rt, _ := login["refresh_token"].(string)
	require.NotEmpty(t, rt)

	m := rawBody(t, r, url.Values{
		"grant_type": {"refresh_token"}, "client_id": {"demo"}, "refresh_token": {rt}, "scope": {"openid"},
	})
	next, _ := m["refresh_token"].(string)
	assert.NotEmpty(t, next)
	assert.NotEqual(t, rt, next)
}

func TestOfflineAccess_Strict_EmptyStoredScopeIgnoresRequestScope(t *testing.T) {
	r, store, _ := newOfflineRouter(t, true)
	forms := offlineForms(store, "", "openid offline_access")
	m := rawBody(t, r, forms["authorization_code"])
	assert.NotContains(t, m, "refresh_token")
	assert.NotContains(t, m, "scope")
}

func TestOfflineAccess_Lenient_EmptyStoredScopeUsesRequestScope(t *testing.T) {
	r, store, _ := newOfflineRouter(t, false)
	forms := offlineForms(store, "", "openid offline_access")
	m := rawBody(t, r, forms["authorization_code"])
	assert.NotEmpty(t, m["refresh_token"])
	assert.Equal(t, "openid offline_access", m["scope"])
}

func TestOfflineAccess_Lenient_AlwaysIssues(t *testing.T) {
	r, store, _ := newOfflineRouter(t, false)
	for name, form := range offlineForms(store, "openid profile", "openid profile") {
		t.Run(name, func(t *testing.T) {
			assert.NotEmpty(t, rawBody(t, r, form)["refresh_token"])
		})
	}
}
