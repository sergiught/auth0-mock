package authapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/mfa"
)

func authenticatorsReq(r chi.Router, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://mock"+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func mfaAccessToken(t *testing.T, ks *jwks.KeySet, aud, scope string) string {
	t.Helper()
	iss := ks.IssuerFor(httptest.NewRequest(http.MethodGet, "http://mock/mfa/authenticators", nil))
	tok, err := ks.Mint(jwks.MintOpts{
		Issuer: iss, Subject: "alice@example.com", Scope: scope, TTL: time.Hour,
		Audience: []string{strings.Replace(aud, "<iss>", iss, 1)},
	})
	require.NoError(t, err)
	return tok
}

func decodeList(t *testing.T, w *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var out []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out), w.Body.String())
	return out
}

func TestListAuthenticators_MFAToken(t *testing.T) {
	r, _, _, ms := newRefreshRouter(t)
	ms.SetRequired(true)
	tok := challengeToken(t, r, userPasswordForm("alice@example.com"))
	_, err := ms.Enroll("alice@example.com", []mfa.AuthenticatorConfig{{Type: mfa.TypeOTP}})
	require.NoError(t, err)

	got := decodeList(t, authenticatorsReq(r, http.MethodGet, "/mfa/authenticators", tok))
	require.Len(t, got, 2)
	for _, e := range got {
		assert.Equal(t, false, e["active"])
	}
}

func TestListAuthenticators_AccessToken(t *testing.T) {
	r, ks, _, ms := newRefreshRouter(t)
	_, err := ms.SetUser("alice@example.com", mfa.UserConfig{Authenticators: []mfa.AuthenticatorConfig{
		{Type: mfa.TypeOTP}, {Type: mfa.TypeOOB, OOBChannel: mfa.ChannelSMS, Name: "+1 555"},
	}})
	require.NoError(t, err)
	tok := mfaAccessToken(t, ks, "<iss>mfa/", "read:authenticators")

	got := decodeList(t, authenticatorsReq(r, http.MethodGet, "/mfa/authenticators", tok))
	require.Len(t, got, 2)
	assert.Equal(t, "otp", got[0]["authenticator_type"])
	assert.Equal(t, true, got[0]["active"])
	assert.NotContains(t, got[0], "oob_channel")
	assert.Equal(t, "oob", got[1]["authenticator_type"])
	assert.Equal(t, "sms", got[1]["oob_channel"])
	assert.Equal(t, "+1 555", got[1]["name"])
}

func TestListAuthenticators_Errors(t *testing.T) {
	r, ks, _, _ := newRefreshRouter(t)

	w := authenticatorsReq(r, http.MethodGet, "/mfa/authenticators", "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	w = authenticatorsReq(r, http.MethodGet, "/mfa/authenticators",
		mfaAccessToken(t, ks, "https://api.example.com/", "read:authenticators"))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "invalid_token", decodeMap(t, w)["error"])

	w = authenticatorsReq(r, http.MethodGet, "/mfa/authenticators", mfaAccessToken(t, ks, "<iss>mfa/", "openid"))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, "insufficient_scope", decodeMap(t, w)["error"])

	w = authenticatorsReq(r, http.MethodGet, "/mfa/authenticators",
		mfaAccessToken(t, ks, "<iss>mfa/", "read:authenticators"))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `[]`, w.Body.String())
}

func TestDeleteAuthenticator(t *testing.T) {
	r, ks, _, ms := newRefreshRouter(t)
	ms.SetRequired(true)
	mfaTok := challengeToken(t, r, userPasswordForm("alice@example.com"))
	enrolled, err := ms.Enroll("alice@example.com", []mfa.AuthenticatorConfig{
		{Type: mfa.TypeOOB, OOBChannel: mfa.ChannelPush}, {Type: mfa.TypeOTP},
	})
	require.NoError(t, err)
	pushID := enrolled[0].ID
	del := mfaAccessToken(t, ks, "<iss>mfa/", "remove:authenticators")

	assert.Equal(t, http.StatusUnauthorized,
		authenticatorsReq(r, http.MethodDelete, "/mfa/authenticators/"+pushID, mfaTok).Code)
	assert.Equal(t, http.StatusForbidden, authenticatorsReq(r, http.MethodDelete, "/mfa/authenticators/"+pushID,
		mfaAccessToken(t, ks, "<iss>mfa/", "read:authenticators")).Code)

	w := authenticatorsReq(r, http.MethodDelete, "/mfa/authenticators/"+pushID, del)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	cfg, _ := ms.GetUser("alice@example.com")
	for _, a := range cfg.Authenticators {
		assert.NotEqual(t, mfa.TypeOTP, a.Type, "group otp must be gone")
		assert.NotEqual(t, pushID, a.ID)
	}

	w = authenticatorsReq(r, http.MethodDelete, "/mfa/authenticators/"+pushID, del)
	require.Equal(t, http.StatusNotFound, w.Code)
	got := decodeMap(t, w)
	assert.Equal(t, "not_found", got["error"])
	assert.Equal(t, "Authenticator not found", got["error_description"])
}

func TestDeleteAuthenticator_PipeInID(t *testing.T) {
	r, ks, _, ms := newRefreshRouter(t)
	del := mfaAccessToken(t, ks, "<iss>mfa/", "remove:authenticators")
	for _, path := range []string{"/mfa/authenticators/totp|dev_X", "/mfa/authenticators/totp%7Cdev_X"} {
		_, err := ms.SetUser("alice@example.com", mfa.UserConfig{Authenticators: []mfa.AuthenticatorConfig{
			{ID: "totp|dev_X", Type: mfa.TypeOTP},
		}})
		require.NoError(t, err)
		w := authenticatorsReq(r, http.MethodDelete, path, del)
		assert.Equal(t, http.StatusNoContent, w.Code, path+" "+w.Body.String())
	}
}
