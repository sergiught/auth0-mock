package authapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/mfa"
)

func postChallenge(r chi.Router, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/mfa/challenge", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func challengeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &m), w.Body.String())
	return m
}

func requireChallengeError(t *testing.T, w *httptest.ResponseRecorder, status int, code, desc string) {
	t.Helper()
	require.Equal(t, status, w.Code, w.Body.String())
	m := challengeBody(t, w)
	assert.Equal(t, code, m["error"])
	assert.Equal(t, desc, m["error_description"])
}

func legacyToken(t *testing.T) (chi.Router, *mfa.Store, string) {
	t.Helper()
	r, _, _, ms := newRefreshRouter(t)
	ms.SetRequired(true)
	return r, ms, challengeToken(t, r, userPasswordForm("alice@example.com"))
}

func TestChallenge_Legacy(t *testing.T) {
	r, ms, tok := legacyToken(t)

	w := postChallenge(r, url.Values{"mfa_token": {tok}, "challenge_type": {"otp oob"}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, map[string]any{"challenge_type": "otp"}, challengeBody(t, w))

	w = postChallenge(r, url.Values{"mfa_token": {tok}, "challenge_type": {"oob"}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	m := challengeBody(t, w)
	assert.Equal(t, "oob", m["challenge_type"])
	assert.Equal(t, "prompt", m["binding_method"])
	code, _ := m["oob_code"].(string)
	require.NotEmpty(t, code)
	got, ok := ms.OOB(code)
	require.True(t, ok)
	assert.Equal(t, tok, got.MFAToken)
}

func TestChallenge_TokenErrors(t *testing.T) {
	r, _, tok := legacyToken(t)

	requireChallengeError(t, postChallenge(r, url.Values{}), http.StatusBadRequest, "invalid_request", "missing mfa_token")
	requireChallengeError(t, postChallenge(r, url.Values{"mfa_token": {"nope"}}),
		http.StatusForbidden, "invalid_grant", "Malformed or expired mfa_token")

	require.Equal(t, http.StatusOK, postChallenge(r, url.Values{"mfa_token": {tok}}).Code)
	w := postTokenForm(r, mfaGrantForm(grantMFAOTP, tok, url.Values{"otp": {mfa.AcceptedOTP}}))
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestChallenge_UnsupportedType(t *testing.T) {
	r, _, tok := legacyToken(t)
	w := postChallenge(r, url.Values{"mfa_token": {tok}, "challenge_type": {"email"}})
	requireChallengeError(t, w, http.StatusBadRequest, "unsupported_challenge_type", "Unsupported challenge type: email")
}

func TestChallenge_StrictPicksFirstMatching(t *testing.T) {
	r, ms, cfg, tok := strictRouter(t,
		mfa.AuthenticatorConfig{Type: mfa.TypeOOB, OOBChannel: mfa.ChannelSMS},
		mfa.AuthenticatorConfig{Type: mfa.TypeOTP},
	)

	w := postChallenge(r, url.Values{"mfa_token": {tok}, "challenge_type": {"otp"}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, map[string]any{"challenge_type": "otp"}, challengeBody(t, w))

	w = postChallenge(r, url.Values{"mfa_token": {tok}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	m := challengeBody(t, w)
	assert.Equal(t, "oob", m["challenge_type"])
	assert.Equal(t, "prompt", m["binding_method"])
	got, ok := ms.OOB(m["oob_code"].(string))
	require.True(t, ok)
	assert.Equal(t, cfg.Authenticators[0].ID, got.AuthenticatorID)
	assert.Equal(t, tok, got.MFAToken)
}

func TestChallenge_StrictAuthenticatorID(t *testing.T) {
	inactive := false
	r, _, cfg, tok := strictRouter(t,
		mfa.AuthenticatorConfig{Type: mfa.TypeOTP},
		mfa.AuthenticatorConfig{Type: mfa.TypeOOB, OOBChannel: mfa.ChannelSMS, Active: &inactive},
		mfa.AuthenticatorConfig{Type: mfa.TypeRecoveryCode},
	)
	with := func(id string, types string) *httptest.ResponseRecorder {
		return postChallenge(r, url.Values{"mfa_token": {tok}, "authenticator_id": {id}, "challenge_type": {types}})
	}

	requireChallengeError(t, with("otp|dev_nope", ""), http.StatusBadRequest, "invalid_request", "Authenticator not found")
	requireChallengeError(t, with(cfg.Authenticators[1].ID, ""), http.StatusBadRequest, "invalid_request", "Authenticator not found")
	requireChallengeError(t, with(cfg.Authenticators[2].ID, ""),
		http.StatusBadRequest, "unsupported_challenge_type", challengeUnsupportedDesc)
	requireChallengeError(t, with(cfg.Authenticators[0].ID, "oob"),
		http.StatusBadRequest, "unsupported_challenge_type", challengeUnsupportedDesc)

	w := with(cfg.Authenticators[0].ID, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, map[string]any{"challenge_type": "otp"}, challengeBody(t, w))
}

func TestChallenge_AssociationRequired(t *testing.T) {
	inactive := false
	r, _, _, tok := strictRouter(t,
		mfa.AuthenticatorConfig{Type: mfa.TypeOTP, Active: &inactive},
		mfa.AuthenticatorConfig{Type: mfa.TypeRecoveryCode},
	)
	requireChallengeError(t, postChallenge(r, url.Values{"mfa_token": {tok}}), http.StatusBadRequest, "association_required",
		"User is not enrolled. You can use /mfa/associate endpoint to enroll the first authenticator.")
}

func TestChallenge_StrictNoMatchingType(t *testing.T) {
	r, _, _, tok := strictRouter(t, mfa.AuthenticatorConfig{Type: mfa.TypeOTP})
	w := postChallenge(r, url.Values{"mfa_token": {tok}, "challenge_type": {"oob"}})
	requireChallengeError(t, w, http.StatusBadRequest, "unsupported_challenge_type", challengeUnsupportedDesc)
}

func TestChallenge_PushOmitsBindingMethod(t *testing.T) {
	r, ms, cfg, tok := strictRouter(t, mfa.AuthenticatorConfig{Type: mfa.TypeOOB, OOBChannel: mfa.ChannelPush})
	w := postChallenge(r, url.Values{"mfa_token": {tok}, "challenge_type": {"oob"}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	m := challengeBody(t, w)
	assert.NotContains(t, m, "binding_method")
	got, ok := ms.OOB(m["oob_code"].(string))
	require.True(t, ok)
	assert.Equal(t, cfg.Authenticators[0].ID, got.AuthenticatorID)
}

func TestChallenge_JSONBody(t *testing.T) {
	r, _, tok := legacyToken(t)
	body := `{"mfa_token":"` + tok + `","challenge_type":"otp"}`
	req := httptest.NewRequest(http.MethodPost, "/mfa/challenge", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, map[string]any{"challenge_type": "otp"}, challengeBody(t, w))
}

func TestChallenge_ThenOOBGrant(t *testing.T) {
	r, _, _, tok := strictRouter(t, mfa.AuthenticatorConfig{Type: mfa.TypeOOB, OOBChannel: mfa.ChannelPush})
	w := postChallenge(r, url.Values{"mfa_token": {tok}, "challenge_type": {"oob"}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	code := challengeBody(t, w)["oob_code"].(string)

	w = postTokenForm(r, mfaGrantForm(grantMFAOOB, tok, url.Values{"oob_code": {code}}))
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestChallenge_JSONNonStringFields(t *testing.T) {
	r, _, tok := legacyToken(t)
	for field, desc := range map[string]string{
		"challenge_type":   "challenge_type must be a string",
		"authenticator_id": "authenticator_id must be a string",
	} {
		body := `{"mfa_token":"` + tok + `","` + field + `":5}`
		req := httptest.NewRequest(http.MethodPost, "/mfa/challenge", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		requireChallengeError(t, w, http.StatusBadRequest, "invalid_request", desc)
	}
}
