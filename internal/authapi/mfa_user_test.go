package authapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/mfa"
)

func boolPtr(b bool) *bool { return &b }

func userPasswordForm(user string) url.Values {
	return url.Values{
		"grant_type": {"password"}, "client_id": {"demo"},
		"username": {user}, "password": {"x"}, "audience": {"https://api/"},
	}
}

func challengeToken(t *testing.T, r chi.Router, form url.Values) string {
	t.Helper()
	w := postTokenForm(r, form)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	var ch map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &ch))
	tok, _ := ch["mfa_token"].(string)
	require.NotEmpty(t, tok)
	return tok
}

func TestMFAGating_PerUserOverridesGlobal(t *testing.T) {
	r, _, _, ms := newRefreshRouter(t)
	_, err := ms.SetUser("alice@example.com", mfa.UserConfig{Required: boolPtr(true)})
	require.NoError(t, err)
	_, err = ms.SetUser("bob@example.com", mfa.UserConfig{Required: boolPtr(false)})
	require.NoError(t, err)

	assert.Equal(t, http.StatusForbidden, postTokenForm(r, userPasswordForm("alice@example.com")).Code)
	assert.Equal(t, http.StatusOK, postTokenForm(r, userPasswordForm("carol@example.com")).Code)

	ms.SetRequired(true)
	assert.Equal(t, http.StatusOK, postTokenForm(r, userPasswordForm("bob@example.com")).Code)
	assert.Equal(t, http.StatusForbidden, postTokenForm(r, userPasswordForm("carol@example.com")).Code)
}

func TestMFAGating_PasswordlessMatchesLogin(t *testing.T) {
	r, _, _, ms := newRefreshRouter(t)
	_, err := ms.SetUser("alice@example.com", mfa.UserConfig{Required: boolPtr(true)})
	require.NoError(t, err)

	assert.Equal(t, http.StatusForbidden, postTokenForm(r, passwordlessForm(nil)).Code)
	other := passwordlessForm(url.Values{"username": {"bob@example.com"}})
	assert.Equal(t, http.StatusOK, postTokenForm(r, other).Code)
}

func strictRouter(t *testing.T, auths ...mfa.AuthenticatorConfig) (chi.Router, *mfa.Store, mfa.UserConfig, string) {
	t.Helper()
	r, _, _, ms := newRefreshRouter(t)
	cfg, err := ms.SetUser("alice@example.com", mfa.UserConfig{Required: boolPtr(true), Authenticators: auths})
	require.NoError(t, err)
	return r, ms, cfg, challengeToken(t, r, userPasswordForm("alice@example.com"))
}

func TestStrictOTP(t *testing.T) {
	otp := url.Values{"otp": {mfa.AcceptedOTP}}

	t.Run("active otp passes", func(t *testing.T) {
		r, _, _, tok := strictRouter(t, mfa.AuthenticatorConfig{Type: mfa.TypeOTP})
		assert.Equal(t, http.StatusOK, postTokenForm(r, mfaGrantForm(grantMFAOTP, tok, otp)).Code)
	})

	t.Run("no otp rejected but token survives for oob", func(t *testing.T) {
		r, ms, cfg, tok := strictRouter(t, mfa.AuthenticatorConfig{Type: mfa.TypeOOB, OOBChannel: mfa.ChannelSMS})
		w := postTokenForm(r, mfaGrantForm(grantMFAOTP, tok, otp))
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "User does not have an active otp authenticator")

		code := ms.IssueOOB(tok, cfg.Authenticators[0].ID)
		w = postTokenForm(r, mfaGrantForm(grantMFAOOB, tok, url.Values{
			"oob_code": {code}, "binding_code": {mfa.AcceptedBindingCode},
		}))
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	})

	t.Run("inactive otp is confirmed", func(t *testing.T) {
		r, ms, _, tok := strictRouter(t, mfa.AuthenticatorConfig{Type: mfa.TypeOTP, Active: boolPtr(false)})
		w := postTokenForm(r, mfaGrantForm(grantMFAOTP, tok, otp))
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.True(t, activeTypes(t, ms)[mfa.TypeOTP])
	})
}

func TestStrictRecoveryCode(t *testing.T) {
	rc := url.Values{"recovery_code": {mfa.AcceptedRecoveryCode}}

	r, _, _, tok := strictRouter(t, mfa.AuthenticatorConfig{Type: mfa.TypeOTP})
	w := postTokenForm(r, mfaGrantForm(grantMFARecovery, tok, rc))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "User does not have an active recovery code")

	r, _, _, tok = strictRouter(t, mfa.AuthenticatorConfig{Type: mfa.TypeRecoveryCode})
	assert.Equal(t, http.StatusOK, postTokenForm(r, mfaGrantForm(grantMFARecovery, tok, rc)).Code)
}

func TestStrictOOB(t *testing.T) {
	sms := mfa.AuthenticatorConfig{Type: mfa.TypeOOB, OOBChannel: mfa.ChannelSMS}
	push := mfa.AuthenticatorConfig{Type: mfa.TypeOOB, OOBChannel: mfa.ChannelPush}
	bind := func(code, binding string) url.Values {
		v := url.Values{"oob_code": {code}}
		if binding != "" {
			v.Set("binding_code", binding)
		}
		return v
	}

	t.Run("matching code and binding", func(t *testing.T) {
		r, ms, cfg, tok := strictRouter(t, sms)
		code := ms.IssueOOB(tok, cfg.Authenticators[0].ID)
		w := postTokenForm(r, mfaGrantForm(grantMFAOOB, tok, bind(code, mfa.AcceptedBindingCode)))
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	})

	t.Run("code from another token or unknown is rejected, retry works", func(t *testing.T) {
		r, ms, cfg, tok := strictRouter(t, sms)
		id := cfg.Authenticators[0].ID
		foreign := ms.IssueOOB("some-other-token", id)
		for _, bad := range []string{foreign, "unknown"} {
			w := postTokenForm(r, mfaGrantForm(grantMFAOOB, tok, bind(bad, mfa.AcceptedBindingCode)))
			assert.Equal(t, http.StatusForbidden, w.Code)
			assert.Contains(t, w.Body.String(), "Invalid oob_code")
		}
		code := ms.IssueOOB(tok, id)
		w := postTokenForm(r, mfaGrantForm(grantMFAOOB, tok, bind(code, mfa.AcceptedBindingCode)))
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	})

	t.Run("push needs no binding code", func(t *testing.T) {
		r, ms, cfg, tok := strictRouter(t, push)
		code := ms.IssueOOB(tok, cfg.Authenticators[0].ID)
		w := postTokenForm(r, mfaGrantForm(grantMFAOOB, tok, bind(code, "")))
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	})

	t.Run("sms wrong binding", func(t *testing.T) {
		r, ms, cfg, tok := strictRouter(t, sms)
		code := ms.IssueOOB(tok, cfg.Authenticators[0].ID)
		w := postTokenForm(r, mfaGrantForm(grantMFAOOB, tok, bind(code, "000000")))
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "Wrong binding code")
	})

	t.Run("missing oob_code", func(t *testing.T) {
		r, _, _, tok := strictRouter(t, sms)
		w := postTokenForm(r, mfaGrantForm(grantMFAOOB, tok, url.Values{"binding_code": {mfa.AcceptedBindingCode}}))
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "missing oob_code")
	})
}

func TestLegacyUserUnaffectedByOtherConfigs(t *testing.T) {
	r, _, _, ms := newRefreshRouter(t)
	ms.SetRequired(true)
	_, err := ms.SetUser("alice@example.com", mfa.UserConfig{})
	require.NoError(t, err)

	tok := challengeToken(t, r, userPasswordForm("bob@example.com"))
	w := postTokenForm(r, mfaGrantForm(grantMFAOOB, tok, url.Values{
		"oob_code": {"anything"}, "binding_code": {mfa.AcceptedBindingCode},
	}))
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

	tok = challengeToken(t, r, userPasswordForm("bob@example.com"))
	w = postTokenForm(r, mfaGrantForm(grantMFAOTP, tok, url.Values{"otp": {mfa.AcceptedOTP}}))
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestMFAGating_PasswordlessTrimsLogin(t *testing.T) {
	r, _, _, ms := newRefreshRouter(t)
	_, err := ms.SetUser("alice@example.com", mfa.UserConfig{Required: boolPtr(true)})
	require.NoError(t, err)

	padded := passwordlessForm(url.Values{"username": {" alice@example.com"}})
	assert.Equal(t, http.StatusForbidden, postTokenForm(r, padded).Code)
}

// pendingRouter turns global MFA on and enrolls pending authenticators for
// alice directly on the store, then returns a fresh mfa_token for her.
func pendingRouter(t *testing.T, add ...mfa.AuthenticatorConfig) (chi.Router, *mfa.Store, []mfa.AuthenticatorConfig, string) {
	t.Helper()
	r, _, _, ms := newRefreshRouter(t)
	ms.SetRequired(true)
	enrolled, err := ms.Enroll("alice@example.com", add)
	require.NoError(t, err)
	return r, ms, enrolled, challengeToken(t, r, userPasswordForm("alice@example.com"))
}

func activeTypes(t *testing.T, ms *mfa.Store) map[string]bool {
	t.Helper()
	cfg, ok := ms.GetUser("alice@example.com")
	require.True(t, ok)
	got := map[string]bool{}
	for _, a := range cfg.Authenticators {
		got[a.Type] = a.Active != nil && *a.Active
	}
	return got
}

func TestConfirm_OTPActivatesPendingGroup(t *testing.T) {
	r, ms, _, tok := pendingRouter(t, mfa.AuthenticatorConfig{Type: mfa.TypeOTP})
	assert.Equal(t, map[string]bool{mfa.TypeOTP: false, mfa.TypeRecoveryCode: false}, activeTypes(t, ms))

	w := postTokenForm(r, mfaGrantForm(grantMFAOTP, tok, url.Values{"otp": {mfa.AcceptedOTP}}))
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, map[string]bool{mfa.TypeOTP: true, mfa.TypeRecoveryCode: true}, activeTypes(t, ms))
}

func TestConfirm_WrongCodeLeavesPending(t *testing.T) {
	r, ms, _, tok := pendingRouter(t, mfa.AuthenticatorConfig{Type: mfa.TypeOTP})
	w := postTokenForm(r, mfaGrantForm(grantMFAOTP, tok, url.Values{"otp": {"000000"}}))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "Wrong otp")
	assert.Equal(t, map[string]bool{mfa.TypeOTP: false, mfa.TypeRecoveryCode: false}, activeTypes(t, ms))

	w = postTokenForm(r, mfaGrantForm(grantMFAOTP, tok, url.Values{"otp": {mfa.AcceptedOTP}}))
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, activeTypes(t, ms)[mfa.TypeOTP])
}

func TestConfirm_LastPendingOTPWins(t *testing.T) {
	r, ms, _, _ := pendingRouter(t, mfa.AuthenticatorConfig{Type: mfa.TypeOTP})
	_, err := ms.Enroll("alice@example.com", []mfa.AuthenticatorConfig{{Type: mfa.TypeOTP}})
	require.NoError(t, err)
	tok := challengeToken(t, r, userPasswordForm("alice@example.com"))

	w := postTokenForm(r, mfaGrantForm(grantMFAOTP, tok, url.Values{"otp": {mfa.AcceptedOTP}}))
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	cfg, _ := ms.GetUser("alice@example.com")
	var otps []bool
	for _, a := range cfg.Authenticators {
		if a.Type == mfa.TypeOTP {
			otps = append(otps, a.Active != nil && *a.Active)
		}
	}
	assert.Equal(t, []bool{false, true}, otps)
}

func TestConfirm_OOBActivatesPending(t *testing.T) {
	r, ms, added, tok := pendingRouter(t, mfa.AuthenticatorConfig{Type: mfa.TypeOOB, OOBChannel: mfa.ChannelSMS})
	code := ms.IssueOOB(tok, added[0].ID)
	w := postTokenForm(r, mfaGrantForm(grantMFAOOB, tok, url.Values{
		"oob_code": {code}, "binding_code": {mfa.AcceptedBindingCode},
	}))
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, map[string]bool{mfa.TypeOOB: true, mfa.TypeRecoveryCode: true}, activeTypes(t, ms))
}

func TestConfirm_OOBCodeBoundToToken(t *testing.T) {
	r, ms, added, tok := pendingRouter(t, mfa.AuthenticatorConfig{Type: mfa.TypeOOB, OOBChannel: mfa.ChannelSMS})
	foreign := ms.IssueOOB("some-other-token", added[0].ID)
	w := postTokenForm(r, mfaGrantForm(grantMFAOOB, tok, url.Values{
		"oob_code": {foreign}, "binding_code": {mfa.AcceptedBindingCode},
	}))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid oob_code")
	assert.False(t, activeTypes(t, ms)[mfa.TypeOOB])
}

func TestRecoveryCode_PendingStillRejected(t *testing.T) {
	r, _, _, tok := pendingRouter(t, mfa.AuthenticatorConfig{Type: mfa.TypeOTP})
	w := postTokenForm(r, mfaGrantForm(grantMFARecovery, tok, url.Values{"recovery_code": {mfa.AcceptedRecoveryCode}}))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "User does not have an active recovery code")
}
