package seed_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/mfa"
	"github.com/sergiught/auth0-mock/internal/seed"
)

const mfaUsersSrc = `
mfa_users:
  alice:
    required: true
    authenticators:
      - type: otp
      - type: oob
        oob_channel: sms
`

func TestSeed_MFAUsers(t *testing.T) {
	st := applied(t, mfaUsersSrc)
	cfg, ok := st.MFA.GetUser("alice")
	require.True(t, ok)
	require.NotNil(t, cfg.Required)
	require.True(t, *cfg.Required)
	require.Len(t, cfg.Authenticators, 2)
	for _, a := range cfg.Authenticators {
		require.NotNil(t, a.Active)
		require.True(t, *a.Active)
	}
	require.Equal(t, "1 mfa user", parse(t, mfaUsersSrc).Summary())
	require.Equal(t, "2 mfa users", parse(t, mfaUsersSrc+"  bob:\n    authenticators: [{type: otp}]\n").Summary())
}

func TestSeed_MFAUsersInvalid(t *testing.T) {
	require.Equal(t, `mfa_users["alice"]: authenticators[0]: oob_channel must be one of sms, voice, email, auth0`,
		parseErr(t, `
mfa_users:
  alice:
    authenticators:
      - type: oob
        oob_channel: pigeon
`))
}

func TestSeed_MFAUsersKeyCase(t *testing.T) {
	require.Equal(t, `unknown key "OOB_Channel" (keys are case-sensitive)`, parseErr(t, `
mfa_users:
  alice:
    authenticators:
      - type: oob
        OOB_Channel: sms
`))
}

func TestExport_MFAUsersRoundTrip(t *testing.T) {
	st := newStores()
	off := false
	_, err := st.MFA.SetUser("alice", mfa.UserConfig{Required: &off, Authenticators: []mfa.AuthenticatorConfig{
		{ID: "otp|fixed", Type: "otp"},
		{Type: "oob", OOBChannel: "sms", Active: &off},
	}})
	require.NoError(t, err)
	on := true
	_, err = st.MFA.SetUser("bob", mfa.UserConfig{Required: &on})
	require.NoError(t, err)
	want := st.MFA.Users()

	js := mustBytes(t)(seed.Export(st.Stores, false).JSON())
	fresh := newStores()
	s, err := seed.Parse(js, newValidator(t))
	require.NoError(t, err)
	s.Apply(fresh.Stores)
	got := fresh.MFA.Users()
	// Group is internal and not exported, so it is not compared.
	for _, users := range []map[string]mfa.UserConfig{want, got} {
		for _, cfg := range users {
			for i := range cfg.Authenticators {
				cfg.Authenticators[i].Group = ""
			}
		}
	}
	require.Equal(t, want, got)

	require.NotContains(t, string(mustBytes(t)(seed.Export(newStores().Stores, false).JSON())), "mfa_users")
}
