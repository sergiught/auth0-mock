package authapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func newRefreshRouter(t *testing.T) (chi.Router, *jwks.KeySet, *steppedClock, *mfa.Store) {
	t.Helper()
	clk := &steppedClock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	ks, err := jwks.NewKeySet(jwks.Config{
		Issuer: "https://mock/", AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour, Now: clk.Now,
	})
	require.NoError(t, err)
	ms := mfa.NewStore()
	r := chi.NewRouter()
	Mount(Deps{
		Router: r, Keys: ks, Issuer: "https://mock/", DefaultAudience: "https://mock/api/v2/",
		Log: zerolog.Nop(), PKCE: pkce.NewStore(pkce.WithNow(clk.Now)),
		Sessions: session.NewStore(), MFA: ms,
	})
	return r, ks, clk, ms
}

func decodeToken(t *testing.T, w *httptest.ResponseRecorder) tokenResponse {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp tokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

func refreshWith(r chi.Router, rt string, extra url.Values) *httptest.ResponseRecorder {
	form := url.Values{"grant_type": {"refresh_token"}, "client_id": {"demo"}, "refresh_token": {rt}}
	for k, v := range extra {
		form[k] = v
	}
	return postTokenForm(r, form)
}

func passwordLogin(t *testing.T, r chi.Router, extra url.Values) tokenResponse {
	t.Helper()
	form := url.Values{
		"grant_type": {"password"}, "client_id": {"demo"}, "username": {"alice@example.com"},
		"password": {"x"}, "audience": {"https://api/"}, "scope": {"openid offline_access"},
	}
	for k, v := range extra {
		form[k] = v
	}
	return decodeToken(t, postTokenForm(r, form))
}

func TestRefresh_PasswordLoginKeepsUser(t *testing.T) {
	r, ks, clk, _ := newRefreshRouter(t)
	login := passwordLogin(t, r, nil)
	loginID, err := ks.Verify(login.IDToken, jwks.VerifyOpts{})
	require.NoError(t, err)
	clk.Advance(10 * time.Minute)

	w := refreshWith(r, login.RefreshToken, nil)
	resp := decodeToken(t, w)

	access, err := ks.Verify(resp.AccessToken, jwks.VerifyOpts{})
	require.NoError(t, err)
	assert.Equal(t, "alice@example.com", access.Subject)
	assert.Equal(t, "refresh-token", access.Extra["gty"])

	id, err := ks.Verify(resp.IDToken, jwks.VerifyOpts{})
	require.NoError(t, err)
	assert.Equal(t, "alice@example.com", id.Subject)
	assert.Equal(t, "demo", id.Extra["azp"])
	assert.NotContains(t, id.Extra, "nonce")
	assert.EqualValues(t, loginID.Extra["auth_time"], id.Extra["auth_time"])
	for _, k := range []string{"email", "email_verified", "name", "nickname", "picture"} {
		assert.Equal(t, loginID.Extra[k], id.Extra[k], k)
	}

	assert.NotEmpty(t, resp.RefreshToken)
	assert.NotEqual(t, login.RefreshToken, resp.RefreshToken)
	assert.Equal(t, "openid offline_access", resp.Scope)
}

func TestRefresh_NewAndOldTokensBothWork(t *testing.T) {
	r, ks, _, _ := newRefreshRouter(t)
	login := passwordLogin(t, r, nil)
	first := decodeToken(t, refreshWith(r, login.RefreshToken, nil))

	second := decodeToken(t, refreshWith(r, first.RefreshToken, nil))
	again := decodeToken(t, refreshWith(r, login.RefreshToken, nil))
	for _, resp := range []tokenResponse{second, again} {
		id, err := ks.Verify(resp.IDToken, jwks.VerifyOpts{})
		require.NoError(t, err)
		assert.Equal(t, "alice@example.com", id.Subject)
	}
}

func TestRefresh_CodeFlowKeepsSubject(t *testing.T) {
	r, ks, _, _ := newRefreshRouter(t)
	code := authorizeCode(t, r, url.Values{"login_hint": {"carol@example.com"}})
	login := decodeToken(t, exchangeCode(r, code, ""))

	resp := decodeToken(t, refreshWith(r, login.RefreshToken, nil))
	access, err := ks.Verify(resp.AccessToken, jwks.VerifyOpts{})
	require.NoError(t, err)
	assert.Equal(t, "carol@example.com", access.Subject)
	id, err := ks.Verify(resp.IDToken, jwks.VerifyOpts{})
	require.NoError(t, err)
	assert.Equal(t, "carol@example.com", id.Subject)
}

func TestRefresh_AudienceAndScopeOverrideOrFallBack(t *testing.T) {
	r, ks, _, _ := newRefreshRouter(t)
	login := passwordLogin(t, r, nil)

	kept := decodeToken(t, refreshWith(r, login.RefreshToken, nil))
	access, err := ks.Verify(kept.AccessToken, jwks.VerifyOpts{})
	require.NoError(t, err)
	assert.Equal(t, "openid offline_access", access.Scope)
	assert.Contains(t, access.Audience, "https://api/")

	over := decodeToken(t, refreshWith(r, login.RefreshToken, url.Values{
		"audience": {"https://other/"}, "scope": {"read:x"},
	}))
	access, err = ks.Verify(over.AccessToken, jwks.VerifyOpts{})
	require.NoError(t, err)
	assert.Equal(t, "read:x", access.Scope)
	assert.Contains(t, access.Audience, "https://other/")
	assert.Equal(t, "read:x", over.Scope)
}

func TestRefresh_PasswordRealmKeepsConnection(t *testing.T) {
	r, ks, _, _ := newRefreshRouter(t)
	login := decodeToken(t, postTokenForm(r, url.Values{
		"grant_type": {"http://auth0.com/oauth/grant-type/password-realm"}, "client_id": {"demo"},
		"username": {"alice"}, "password": {"x"}, "realm": {"db"}, "scope": {"openid"},
	}))
	resp := decodeToken(t, refreshWith(r, login.RefreshToken, nil))
	id, err := ks.Verify(resp.IDToken, jwks.VerifyOpts{})
	require.NoError(t, err)
	assert.Equal(t, "alice", id.Subject)
	assert.Equal(t, "db", id.Extra["connection"])
}

func TestRefresh_MFAStepUpKeepsSubject(t *testing.T) {
	r, ks, _, ms := newRefreshRouter(t)
	tok := ms.Issue(mfa.Context{ClientID: "demo", Audience: "https://api/", Scope: "openid", Subject: "alice"})
	login := decodeToken(t, postTokenForm(r, url.Values{
		"grant_type": {"http://auth0.com/oauth/grant-type/mfa-otp"}, "client_id": {"demo"},
		"mfa_token": {tok}, "otp": {mfa.AcceptedOTP},
	}))
	resp := decodeToken(t, refreshWith(r, login.RefreshToken, nil))
	id, err := ks.Verify(resp.IDToken, jwks.VerifyOpts{})
	require.NoError(t, err)
	assert.Equal(t, "alice", id.Subject)
}

func TestRefresh_UnknownTokenUnchanged(t *testing.T) {
	r, ks, _, _ := newRefreshRouter(t)
	resp := decodeToken(t, refreshWith(r, "any-uuid", nil))
	access, err := ks.Verify(resp.AccessToken, jwks.VerifyOpts{})
	require.NoError(t, err)
	assert.Equal(t, "demo@refresh", access.Subject)
	assert.Empty(t, resp.IDToken)
	assert.Empty(t, resp.RefreshToken)
	assert.Empty(t, resp.Scope)
}
