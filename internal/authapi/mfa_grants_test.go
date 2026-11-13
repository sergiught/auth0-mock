package authapi

import (
	"net/http"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/mfa"
)

const (
	grantMFAOTP      = "http://auth0.com/oauth/grant-type/mfa-otp"
	grantMFAOOB      = "http://auth0.com/oauth/grant-type/mfa-oob"
	grantMFARecovery = "http://auth0.com/oauth/grant-type/mfa-recovery-code"
)

func mfaGrantForm(grant, token string, params url.Values) url.Values {
	form := url.Values{"grant_type": {grant}, "client_id": {"demo"}, "mfa_token": {token}}
	for k, v := range params {
		form[k] = v
	}
	return form
}

func TestMFAGrants_WrongCodeCanBeRetried(t *testing.T) {
	cases := []struct {
		name  string
		grant string
		wrong url.Values
		right url.Values
	}{
		{"otp", grantMFAOTP, url.Values{"otp": {"000000"}}, url.Values{"otp": {mfa.AcceptedOTP}}},
		{
			"oob", grantMFAOOB,
			url.Values{"oob_code": {"push"}, "binding_code": {"000000"}},
			url.Values{"oob_code": {"push"}, "binding_code": {mfa.AcceptedBindingCode}},
		},
		{
			"recovery", grantMFARecovery,
			url.Values{"recovery_code": {"WRONG"}},
			url.Values{"recovery_code": {mfa.AcceptedRecoveryCode}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _, _, ms := newRefreshRouter(t)
			tok := ms.Issue(mfa.Context{ClientID: "demo", Audience: "https://api/", Scope: "openid", Subject: "alice"})

			w := postTokenForm(r, mfaGrantForm(tc.grant, tok, tc.wrong))
			assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())

			w = postTokenForm(r, mfaGrantForm(tc.grant, tok, tc.right))
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())

			// A token used successfully cannot be reused.
			w = postTokenForm(r, mfaGrantForm(tc.grant, tok, tc.right))
			assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), "Malformed or expired mfa_token")
		})
	}
}

func TestMFAGrants_ConcurrentSuccessMintsOnce(t *testing.T) {
	r, _, _, ms := newRefreshRouter(t)
	tok := ms.Issue(mfa.Context{ClientID: "demo", Audience: "https://api/", Scope: "openid", Subject: "alice"})

	const n = 20
	codes := make([]int, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			w := postTokenForm(r, mfaGrantForm(grantMFAOTP, tok, url.Values{"otp": {mfa.AcceptedOTP}}))
			codes[i] = w.Code
		}()
	}
	close(start)
	wg.Wait()

	ok := 0
	for _, c := range codes {
		if c == http.StatusOK {
			ok++
		}
	}
	assert.Equal(t, 1, ok, "exactly one concurrent submission may succeed: %v", codes)
}
