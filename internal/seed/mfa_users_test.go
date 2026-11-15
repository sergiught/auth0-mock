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
		{ID: "otp|fixed", Type: "otp", Group: "g1"},
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
	require.Equal(t, want, got)

	require.NotContains(t, string(mustBytes(t)(seed.Export(newStores().Stores, false).JSON())), "mfa_users")
}

func TestExport_MFAUsersKeepsGroup(t *testing.T) {
	st := newStores()
	added, err := st.MFA.Enroll("alice", []mfa.AuthenticatorConfig{{Type: mfa.TypeOOB, OOBChannel: mfa.ChannelPush}, {Type: mfa.TypeOTP}})
	require.NoError(t, err)
	var pushID string
	for _, a := range added {
		if a.Type == mfa.TypeOOB {
			pushID = a.ID
		}
	}
	require.NotEmpty(t, pushID)

	js := mustBytes(t)(seed.Export(st.Stores, false).JSON())
	fresh := newStores()
	s, err := seed.Parse(js, newValidator(t))
	require.NoError(t, err)
	s.Apply(fresh.Stores)

	require.True(t, fresh.MFA.Activate("alice", pushID))
	cfg, ok := fresh.MFA.GetUser("alice")
	require.True(t, ok)
	grouped := 0
	for _, a := range cfg.Authenticators {
		if a.Type == mfa.TypeOTP || a.ID == pushID {
			grouped++
			require.NotNil(t, a.Active)
			require.True(t, *a.Active, a.ID)
		}
	}
	require.Equal(t, 2, grouped, "expected the otp and push authenticators")
}
