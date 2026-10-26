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

func newNoAudienceMFARouter(t *testing.T) (chi.Router, *jwks.KeySet, *mfa.Store) {
	t.Helper()
	clk := &steppedClock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	ks, err := jwks.NewKeySet(jwks.Config{
		Issuer: "https://mock/", AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour, Now: clk.Now,
	})
	require.NoError(t, err)
	ms := mfa.NewStore()
	r := chi.NewRouter()
	Mount(Deps{
		Router: r, Keys: ks, Issuer: "https://mock/", DefaultAudience: "",
		Log: zerolog.Nop(), PKCE: pkce.NewStore(pkce.WithNow(clk.Now)),
		Sessions: session.NewStore(), MFA: ms,
	})
	return r, ks, ms
}

func tokenBody(t *testing.T, w *httptest.ResponseRecorder) tokenResponse {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp tokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

func requireOpaque(t *testing.T, r chi.Router, ks *jwks.KeySet, tok string) {
	t.Helper()
	require.NotEmpty(t, tok)
	assert.NotContains(t, tok, ".")
	_, err := ks.Verify(tok, jwks.VerifyOpts{})
	assert.Error(t, err)
	assert.Equal(t, http.StatusOK, userInfoWith(r, tok).Code)
}

func passwordForm() url.Values {
	return url.Values{
		"grant_type": {"password"}, "client_id": {"demo"},
		"username": {"dana@example.com"}, "password": {"x"},
		"scope": {"openid offline_access"},
	}
}

func TestToken_ClientCredentialsWithoutAudienceRefused(t *testing.T) {
	r, ks, _ := newNoAudienceMFARouter(t)
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {"demo"}, "client_secret": {"x"}}
	w := postTokenForm(r, form)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "access_denied", body["error"])
	assert.Equal(t, "No audience parameter provided, no default audience been configured", body["error_description"])

	form.Set("audience", "https://api/")
	resp := tokenBody(t, postTokenForm(r, form))
	_, err := ks.Verify(resp.AccessToken, jwks.VerifyOpts{})
	require.NoError(t, err)
}

func TestToken_PasswordWithoutAudienceIsOpaque(t *testing.T) {
	r, ks, _ := newNoAudienceMFARouter(t)
	resp := tokenBody(t, postTokenForm(r, passwordForm()))
	requireOpaque(t, r, ks, resp.AccessToken)
	id := verified(t, ks, resp.IDToken)
	var info map[string]any
	require.NoError(t, json.Unmarshal(userInfoWith(r, resp.AccessToken).Body.Bytes(), &info))
	assert.Equal(t, id.Subject, info["sub"])
}

func TestToken_RefreshOfOpaqueGrantStaysOpaque(t *testing.T) {
	r, ks, _ := newNoAudienceMFARouter(t)
	first := tokenBody(t, postTokenForm(r, passwordForm()))
	require.NotEmpty(t, first.RefreshToken)
	resp := tokenBody(t, postTokenForm(r, url.Values{
		"grant_type": {"refresh_token"}, "client_id": {"demo"}, "refresh_token": {first.RefreshToken},
	}))
	requireOpaque(t, r, ks, resp.AccessToken)
}

func TestToken_AuthorizationCodeWithoutAudienceIsOpaque(t *testing.T) {
	r, ks, _ := newNoAudienceMFARouter(t)
	loc := location(t, getAuthorize(r, "response_type=code&client_id=demo&redirect_uri=https%3A%2F%2Fapp%2Fcb"))
	code := loc.Query().Get("code")
	require.NotEmpty(t, code)
	resp := tokenBody(t, exchangeCode(r, code, ""))
	requireOpaque(t, r, ks, resp.AccessToken)
}

func TestToken_PasswordlessWithoutAudienceIsOpaque(t *testing.T) {
	r, ks, _ := newNoAudienceMFARouter(t)
	resp := tokenBody(t, postTokenForm(r, url.Values{
		"grant_type": {"http://auth0.com/oauth/grant-type/passwordless/otp"}, "client_id": {"demo"},
		"realm": {"email"}, "username": {"erin@example.com"}, "otp": {"000000"},
	}))
	requireOpaque(t, r, ks, resp.AccessToken)
}

func TestToken_MFAWithoutAudienceIsOpaque(t *testing.T) {
	r, ks, ms := newNoAudienceMFARouter(t)
	tok := ms.Issue(mfa.Context{ClientID: "demo", Scope: "openid", Subject: "alice"})
	resp := tokenBody(t, postTokenForm(r, url.Values{
		"grant_type": {"http://auth0.com/oauth/grant-type/mfa-otp"}, "client_id": {"demo"},
		"mfa_token": {tok}, "otp": {mfa.AcceptedOTP},
	}))
	requireOpaque(t, r, ks, resp.AccessToken)
}

func TestToken_WhitespaceAudienceFallsBack(t *testing.T) {
	r, ks := newAuthRouter(t)
	resp := tokenBody(t, postTokenForm(r, url.Values{
		"grant_type": {"client_credentials"}, "client_id": {"demo"}, "client_secret": {"x"},
		"audience": {"  "},
	}))
	assert.Contains(t, verified(t, ks, resp.AccessToken).Audience, "https://mock/api/v2/")

	frag := fragmentOf(t, r, implicitBase+"response_type=token&audience=%20")
	assert.Contains(t, verified(t, ks, frag.Get("access_token")).Audience, "https://mock/api/v2/")
}

func refreshForm(refresh, audience string) url.Values {
	return url.Values{
		"grant_type": {"refresh_token"}, "client_id": {"demo"},
		"refresh_token": {refresh}, "audience": {audience},
	}
}

func TestToken_RefreshWithAudienceMintsJWT(t *testing.T) {
	r, ks, _ := newNoAudienceMFARouter(t)
	first := tokenBody(t, postTokenForm(r, passwordForm()))
	resp := tokenBody(t, postTokenForm(r, refreshForm(first.RefreshToken, "https://api/")))
	assert.Contains(t, verified(t, ks, resp.AccessToken).Audience, "https://api/")
}

func TestToken_RefreshWhitespaceAudienceFallsBack(t *testing.T) {
	r, ks, _, _, _ := newRefreshRouterWithSessions(t)
	first := tokenBody(t, postTokenForm(r, passwordForm()))
	resp := tokenBody(t, postTokenForm(r, refreshForm(first.RefreshToken, "  ")))
	assert.Contains(t, verified(t, ks, resp.AccessToken).Audience, "https://mock/api/v2/")
}

func TestPasswordless_VerifyWhitespaceAudienceFallsBack(t *testing.T) {
	r, ks := newAuthRouter(t)
	w := postForm(r, "/passwordless/verify", url.Values{
		"grant_type": {grantPasswordlessOTP}, "client_id": {"abc"}, "realm": {"email"},
		"username": {"alice@example.com"}, "otp": {"000000"}, "audience": {"  "},
	})
	assert.Contains(t, verified(t, ks, tokenBody(t, w).AccessToken).Audience, "https://mock/api/v2/")
}
