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

func associate(r chi.Router, token, contentType, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "http://example.com:8080/mfa/associate", strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func associateJSON(r chi.Router, token, body string) *httptest.ResponseRecorder {
	return associate(r, token, "application/json", body)
}

func decodeMap(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &m), w.Body.String())
	return m
}

func associateSetup(t *testing.T) (chi.Router, *mfa.Store, string) {
	t.Helper()
	r, _, _, ms := newRefreshRouter(t)
	ms.SetRequired(true)
	return r, ms, challengeToken(t, r, userPasswordForm("alice@example.com"))
}

func TestAssociate_OTP(t *testing.T) {
	r, ms, tok := associateSetup(t)
	w := associateJSON(r, tok, `{"authenticator_types":["otp"]}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	got := decodeMap(t, w)
	assert.Equal(t, "otp", got["authenticator_type"])
	assert.Equal(t, "JBSWY3DPEHPK3PXP", got["secret"])
	assert.Equal(t, "otpauth://totp/example.com:alice@example.com?secret=JBSWY3DPEHPK3PXP&issuer=example.com&algorithm=SHA1&digits=6&period=30", got["barcode_uri"])
	assert.Equal(t, []any{"ABCDEFGHIJKLMNOP"}, got["recovery_codes"])
	assert.Equal(t, map[string]bool{mfa.TypeOTP: false, mfa.TypeRecoveryCode: false}, activeTypes(t, ms))
}

func TestAssociate_PushHasNoSecret(t *testing.T) {
	r, _, tok := associateSetup(t)
	w := associateJSON(r, tok, `{"authenticator_types":["oob"],"oob_channels":["auth0"]}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	got := decodeMap(t, w)
	assert.NotContains(t, got, "secret")
	assert.NotEmpty(t, got["barcode_uri"])
}

func TestAssociate_SMS(t *testing.T) {
	r, ms, tok := associateSetup(t)
	w := associateJSON(r, tok, `{"authenticator_types":["oob"],"oob_channels":["sms"],"phone_number":"+15551234567"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	got := decodeMap(t, w)
	assert.Equal(t, "oob", got["authenticator_type"])
	assert.Equal(t, "sms", got["oob_channel"])
	assert.NotEmpty(t, got["oob_code"])
	assert.Equal(t, "prompt", got["binding_method"])
	cfg, ok := ms.GetUser("alice@example.com")
	require.True(t, ok)
	assert.Equal(t, "+15551234567", cfg.Authenticators[0].Name)
}

func TestAssociate_Push(t *testing.T) {
	r, ms, tok := associateSetup(t)
	w := associateJSON(r, tok, `{"authenticator_types":["oob"],"oob_channels":["auth0"]}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	got := decodeMap(t, w)
	assert.NotContains(t, got, "binding_method")
	assert.NotEmpty(t, got["barcode_uri"])
	cfg, ok := ms.GetUser("alice@example.com")
	require.True(t, ok)
	require.Len(t, cfg.Authenticators, 3)
	assert.Equal(t, mfa.ChannelPush, cfg.Authenticators[0].OOBChannel)
	assert.Equal(t, mfa.TypeOTP, cfg.Authenticators[1].Type)
	assert.Equal(t, cfg.Authenticators[0].Group, cfg.Authenticators[1].Group)
	code, _ := got["oob_code"].(string)
	rec, found := ms.OOB(code)
	require.True(t, found)
	assert.Equal(t, cfg.Authenticators[0].ID, rec.AuthenticatorID)
	assert.Equal(t, tok, rec.MFAToken)
}

func TestAssociate_SecondCallNoRecoveryCodes(t *testing.T) {
	r, ms, tok := associateSetup(t)
	first := associateJSON(r, tok, `{"authenticator_types":["otp"]}`)
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	assert.Contains(t, decodeMap(t, first), "recovery_codes")

	second := associateJSON(r, tok, `{"authenticator_types":["otp"]}`)
	require.Equal(t, http.StatusOK, second.Code, second.Body.String())
	assert.NotContains(t, decodeMap(t, second), "recovery_codes")

	cfg, ok := ms.GetUser("alice@example.com")
	require.True(t, ok)
	n := 0
	for _, a := range cfg.Authenticators {
		if a.Type == mfa.TypeRecoveryCode {
			n++
		}
	}
	assert.Equal(t, 1, n)
}

func TestAssociate_RepeatedThenConfirmedActivatesRecoveryCode(t *testing.T) {
	r, _, tok := associateSetup(t)
	require.Equal(t, http.StatusOK, associateJSON(r, tok, `{"authenticator_types":["otp"]}`).Code)
	require.Equal(t, http.StatusOK, associateJSON(r, tok, `{"authenticator_types":["otp"]}`).Code)

	w := postTokenForm(r, mfaGrantForm(grantMFAOTP, tok, url.Values{"otp": {mfa.AcceptedOTP}}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	next := challengeToken(t, r, userPasswordForm("alice@example.com"))
	w = postTokenForm(r, mfaGrantForm(grantMFARecovery, next, url.Values{"recovery_code": {mfa.AcceptedRecoveryCode}}))
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestAssociate_Errors(t *testing.T) {
	r, ms, tok := associateSetup(t)
	cases := []struct {
		name, token, body string
		status            int
		code, desc        string
	}{
		{"no bearer", "", `{"authenticator_types":["otp"]}`, 401, "invalid_token", "missing bearer token"},
		{"unknown token", "nope", `{"authenticator_types":["otp"]}`, 401, "invalid_token", "Malformed or expired mfa_token"},
		{"bad type", tok, `{"authenticator_types":["sms"]}`, 400, "invalid_request", `authenticator_types must be ["otp"] or ["oob"]`},
		{"oob without channel", tok, `{"authenticator_types":["oob"]}`, 400, "invalid_request", "oob_channels must be one of sms, voice, email, auth0"},
		{"sms without phone", tok, `{"authenticator_types":["oob"],"oob_channels":["sms"]}`, 400, "invalid_request", "phone_number is required for the sms and voice channels"},
		{"email without email", tok, `{"authenticator_types":["oob"],"oob_channels":["email"]}`, 400, "invalid_request", "email is required for the email channel"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := associateJSON(r, tc.token, tc.body)
			assert.Equal(t, tc.status, w.Code, w.Body.String())
			got := decodeMap(t, w)
			assert.Equal(t, tc.code, got["error"])
			assert.Equal(t, tc.desc, got["error_description"])
		})
	}

	t.Run("already enrolled", func(t *testing.T) {
		_, err := ms.SetUser("alice@example.com", mfa.UserConfig{Authenticators: []mfa.AuthenticatorConfig{
			{Type: mfa.TypeOOB, OOBChannel: mfa.ChannelSMS, Active: boolPtr(true)},
		}})
		require.NoError(t, err)
		w := associateJSON(r, tok, `{"authenticator_types":["otp"]}`)
		assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
		got := decodeMap(t, w)
		assert.Equal(t, "access_denied", got["error"])
		assert.Equal(t, "User is already enrolled.", got["error_description"])
	})
}

func TestAssociate_FormAndJSONLists(t *testing.T) {
	r, _, tok := associateSetup(t)
	form := url.Values{"authenticator_types": {"oob"}, "oob_channels": {"email"}, "email": {"a@b.c"}}
	w := associate(r, tok, "application/x-www-form-urlencoded", form.Encode())
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

	r, _, tok = associateSetup(t)
	w = associateJSON(r, tok, `{"authenticator_types":"otp"}`)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestAssociate_TokenNotConsumed(t *testing.T) {
	r, _, tok := associateSetup(t)
	require.Equal(t, http.StatusOK, associateJSON(r, tok, `{"authenticator_types":["otp"]}`).Code)
	w := postTokenForm(r, mfaGrantForm(grantMFAOTP, tok, url.Values{"otp": {mfa.AcceptedOTP}}))
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}
