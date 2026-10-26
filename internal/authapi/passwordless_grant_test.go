package authapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/mfa"
)

func passwordlessForm(extra url.Values) url.Values {
	form := url.Values{
		"grant_type": {grantPasswordlessOTP}, "client_id": {"demo"}, "realm": {"email"},
		"username": {"alice@example.com"}, "otp": {acceptedPasswordlessOTP},
	}
	for k, v := range extra {
		form[k] = v
	}
	return form
}

func verifyTok(t *testing.T, ks *jwks.KeySet, tok string) *jwks.Claims {
	t.Helper()
	c, err := ks.Verify(tok, jwks.VerifyOpts{})
	require.NoError(t, err)
	return c
}

func TestToken_PasswordlessEmail(t *testing.T) {
	r, ks := newAuthRouter(t)
	resp := decodeToken(t, postTokenForm(r, passwordlessForm(nil)))
	assert.NotEmpty(t, resp.AccessToken)
	assert.NotEmpty(t, resp.IDToken)
	assert.NotEmpty(t, resp.RefreshToken)
	assert.Equal(t, "Bearer", resp.TokenType)

	sub, _ := passwordlessUser("email", "alice@example.com")
	ac := verifyTok(t, ks, resp.AccessToken)
	assert.Equal(t, sub, ac.Subject)
	assert.Contains(t, ac.Audience, "https://mock/api/v2/")
	assert.Equal(t, "passwordless", ac.Extra["gty"])
	assert.Equal(t, "demo", ac.Extra["azp"])

	ic := verifyTok(t, ks, resp.IDToken)
	assert.Equal(t, []string{"demo"}, ic.Audience)
	assert.Equal(t, "alice@example.com", ic.Extra["email"])
	assert.Equal(t, "email", ic.Extra["connection"])
	assert.Contains(t, ic.Extra, "auth_time")
	assert.NotContains(t, ic.Extra, "gty")
}

func TestToken_PasswordlessAudienceAndScope(t *testing.T) {
	r, ks := newAuthRouter(t)
	resp := decodeToken(t, postTokenForm(r, passwordlessForm(url.Values{
		"audience": {"https://api/"}, "scope": {"openid offline_access"},
	})))
	ac := verifyTok(t, ks, resp.AccessToken)
	assert.Contains(t, ac.Audience, "https://api/")
	assert.Equal(t, "openid offline_access", ac.Scope)
}

func TestToken_PasswordlessSMSProfileSurvivesRefreshAndUserinfo(t *testing.T) {
	r, ks, _, _, _ := newRefreshRouterWithSessions(t)
	form := passwordlessForm(url.Values{"realm": {"sms"}, "username": {"+15551234567"}, "scope": {"openid offline_access"}})
	resp := decodeToken(t, postTokenForm(r, form))
	ic := verifyTok(t, ks, resp.IDToken)
	assert.Equal(t, "+15551234567", ic.Extra["phone_number"])
	assert.NotContains(t, ic.Extra, "email")

	info := getUserinfo(t, r, resp.AccessToken)
	assert.Equal(t, "+15551234567", info["phone_number"])
	assert.NotContains(t, info, "email")

	rf := decodeToken(t, refreshWith(r, resp.RefreshToken, nil))
	rc := verifyTok(t, ks, rf.IDToken)
	assert.Equal(t, ic.Subject, rc.Subject)
	assert.Equal(t, "+15551234567", rc.Extra["phone_number"])
}

func TestToken_PasswordlessMissingParams(t *testing.T) {
	r, _ := newAuthRouter(t)
	for _, name := range []string{"client_id", "username", "otp", "realm"} {
		t.Run(name, func(t *testing.T) {
			form := passwordlessForm(nil)
			form.Del(name)
			w := postTokenForm(r, form)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			var body map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, "invalid_request", body["error"])
			assert.Equal(t, "Missing required parameter: "+name, body["error_description"])
		})
	}
	t.Run("blank username", func(t *testing.T) {
		w := postTokenForm(r, passwordlessForm(url.Values{"username": {" "}}))
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, "invalid_request", body["error"])
		assert.Equal(t, "Missing required parameter: username", body["error_description"])
	})
}

func TestToken_PasswordlessBadRealm(t *testing.T) {
	r, _ := newAuthRouter(t)
	w := postTokenForm(r, passwordlessForm(url.Values{"realm": {"magic"}}))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `realm must be \"email\" or \"sms\"`)
}

func TestToken_PasswordlessWrongOTP(t *testing.T) {
	r, _ := newAuthRouter(t)
	for realm, desc := range map[string]string{
		"email": "Wrong email or verification code.",
		"sms":   "Wrong phone number or verification code.",
	} {
		t.Run(realm, func(t *testing.T) {
			w := postTokenForm(r, passwordlessForm(url.Values{"realm": {realm}, "otp": {"111111"}}))
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			var body map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, "invalid_grant", body["error"])
			assert.Equal(t, desc, body["error_description"])
		})
	}
}

func TestToken_PasswordlessJSONBody(t *testing.T) {
	r, _ := newAuthRouter(t)
	body := `{"grant_type":"` + grantPasswordlessOTP + `","client_id":"demo","realm":"email","username":"alice@example.com","otp":"000000"}`
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.NotEmpty(t, decodeToken(t, w).IDToken)
}

func TestToken_PasswordlessMFAKeepsProfile(t *testing.T) {
	r, ks, store := newAuthRouterWithMFA(t)
	store.SetRequired(true)
	w := postTokenForm(r, passwordlessForm(nil))
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	var ch map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &ch))
	tok, _ := ch["mfa_token"].(string)
	require.NotEmpty(t, tok)

	resp := decodeToken(t, postTokenForm(r, url.Values{
		"grant_type": {"http://auth0.com/oauth/grant-type/mfa-otp"}, "client_id": {"demo"},
		"mfa_token": {tok}, "otp": {mfa.AcceptedOTP},
	}))
	sub, _ := passwordlessUser("email", "alice@example.com")
	ic := verifyTok(t, ks, resp.IDToken)
	assert.Equal(t, "alice@example.com", ic.Extra["email"])
	assert.Equal(t, sub, ic.Subject)
	assert.Equal(t, "email", ic.Extra["connection"])
}
