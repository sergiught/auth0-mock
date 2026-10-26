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
	"github.com/sergiught/auth0-mock/internal/session"
)

func getUserinfo(t *testing.T, r chi.Router, tok string) map[string]any {
	t.Helper()
	req := httptest.NewRequest("GET", "/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body
}

func accessTokenOf(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var resp tokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp.AccessToken
}

func TestUserinfo_CodeFlowReturnsProfile(t *testing.T) {
	r, _, _ := newNonceRouter(t)
	code := authorizeCode(t, r, url.Values{"login_hint": {"alice@example.com"}})
	w := exchangeCode(r, code, "")
	require.Equal(t, http.StatusOK, w.Code)
	body := getUserinfo(t, r, accessTokenOf(t, w))
	assert.Equal(t, "alice@example.com", body["sub"])
	assert.Equal(t, "alice", body["nickname"])
	assert.Equal(t, "alice@example.com", body["email"])
	assert.Equal(t, "https://cdn.auth0.com/avatars/al.png", body["picture"])
	assert.Equal(t, true, body["email_verified"])
	assert.NotContains(t, body, "nonce")
}

func TestUserinfo_PasswordGrantReturnsProfile(t *testing.T) {
	r, _, _ := newNonceRouter(t)
	w := postTokenForm(r, url.Values{
		"grant_type": {"password"}, "client_id": {"demo"}, "username": {"alice@example.com"}, "password": {"x"},
	})
	body := getUserinfo(t, r, accessTokenOf(t, w))
	assert.Equal(t, "alice", body["nickname"])

	w = postTokenForm(r, url.Values{
		"grant_type": {"http://auth0.com/oauth/grant-type/password-realm"}, "client_id": {"demo"},
		"username": {"bob@example.com"}, "password": {"x"}, "realm": {"db"},
	})
	body = getUserinfo(t, r, accessTokenOf(t, w))
	assert.Equal(t, "bob", body["nickname"])
}

func TestUserinfo_TokenClaimsWinOverProfile(t *testing.T) {
	r, ks, _ := newNonceRouter(t)
	w := postTokenForm(r, url.Values{
		"grant_type": {"password"}, "client_id": {"demo"}, "username": {"alice@example.com"}, "password": {"x"},
	})
	require.Equal(t, http.StatusOK, w.Code)
	tok, err := ks.Mint(jwks.MintOpts{
		Subject: "alice@example.com", Audience: []string{"a"}, TTL: time.Hour,
		Extra: map[string]any{"nickname": "custom"},
	})
	require.NoError(t, err)
	body := getUserinfo(t, r, tok)
	assert.Equal(t, "custom", body["nickname"])
	assert.Equal(t, "alice@example.com", body["email"])
}

func TestUserinfo_UnknownSubjectUnchanged(t *testing.T) {
	r, ks, _ := newNonceRouter(t)
	tok, err := ks.Mint(jwks.MintOpts{Subject: "auth0|x", Audience: []string{"a"}, TTL: time.Hour})
	require.NoError(t, err)
	body := getUserinfo(t, r, tok)
	assert.Equal(t, "auth0|x", body["sub"])
	assert.NotContains(t, body, "nickname")
}

func TestUserinfo_MFAGrantReturnsProfile(t *testing.T) {
	ks, err := jwks.NewKeySet(jwks.Config{Issuer: "https://mock/", AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour})
	require.NoError(t, err)
	mfaStore := mfa.NewStore()
	r := chi.NewRouter()
	Mount(Deps{
		Router: r, Keys: ks, Issuer: "https://mock/", DefaultAudience: "https://mock/api/v2/",
		Log: zerolog.Nop(), MFA: mfaStore, Sessions: session.NewStore(),
	})
	tok := mfaStore.Issue(mfa.Context{ClientID: "abc", Audience: "https://api/", Scope: "openid", Subject: "alice"})
	w := postTokenForm(r, url.Values{
		"grant_type": {"http://auth0.com/oauth/grant-type/mfa-otp"}, "client_id": {"abc"},
		"mfa_token": {tok}, "otp": {mfa.AcceptedOTP},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := getUserinfo(t, r, accessTokenOf(t, w))
	assert.Equal(t, "alice", body["nickname"])
	assert.Equal(t, "https://cdn.auth0.com/avatars/al.png", body["picture"])
}

func TestProfileOf_PicksOnlyProfileKeys(t *testing.T) {
	got := profileOf(map[string]any{
		"email": "a@x", "name": "A", "nonce": "n", "azp": "c", "auth_time": 1, "picture": "p",
	})
	assert.Equal(t, map[string]any{"email": "a@x", "name": "A", "picture": "p"}, got)
}
