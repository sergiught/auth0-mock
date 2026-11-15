package mfa

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptr(b bool) *bool { return &b }

func TestSetUser_FillsIDsAndDefaults(t *testing.T) {
	t.Parallel()
	s := NewStore()
	got, err := s.SetUser("alice", UserConfig{Authenticators: []AuthenticatorConfig{
		{Type: TypeOTP},
		{Type: TypeOOB, OOBChannel: ChannelPush},
		{Type: TypeRecoveryCode},
		{Type: TypeOOB, OOBChannel: ChannelSMS, Active: ptr(false)},
		{ID: "totp|dev_fixed", Type: TypeOTP},
	}})
	require.NoError(t, err)
	require.Len(t, got.Authenticators, 5)

	assert.Regexp(t, `^totp\|dev_[A-Za-z0-9]{16}$`, got.Authenticators[0].ID)
	assert.Regexp(t, `^push\|dev_[A-Za-z0-9]{16}$`, got.Authenticators[1].ID)
	assert.Regexp(t, `^recovery-code\|dev_[A-Za-z0-9]{16}$`, got.Authenticators[2].ID)
	assert.Regexp(t, `^sms\|dev_[A-Za-z0-9]{16}$`, got.Authenticators[3].ID)
	assert.Equal(t, "totp|dev_fixed", got.Authenticators[4].ID)

	assert.True(t, *got.Authenticators[0].Active)
	assert.True(t, *got.Authenticators[1].Active)
	assert.True(t, *got.Authenticators[2].Active)
	assert.False(t, *got.Authenticators[3].Active)

	stored, ok := s.GetUser("alice")
	require.True(t, ok)
	assert.Equal(t, got, stored)
}

func TestSetUser_NilAuthenticatorsStoredAsEmpty(t *testing.T) {
	t.Parallel()
	s := NewStore()
	got, err := s.SetUser("alice", UserConfig{})
	require.NoError(t, err)
	assert.NotNil(t, got.Authenticators)
	assert.Empty(t, got.Authenticators)
}

func TestSetUser_ReplacesAndGroupsPerEntry(t *testing.T) {
	t.Parallel()
	s := NewStore()
	got, err := s.SetUser("alice", UserConfig{Authenticators: []AuthenticatorConfig{{Type: TypeOTP}, {Type: TypeOTP}}})
	require.NoError(t, err)
	assert.NotEmpty(t, got.Authenticators[0].Group)
	assert.NotEmpty(t, got.Authenticators[1].Group)
	assert.NotEqual(t, got.Authenticators[0].Group, got.Authenticators[1].Group)

	got, err = s.SetUser("alice", UserConfig{Required: ptr(true)})
	require.NoError(t, err)
	assert.Empty(t, got.Authenticators)
	assert.Len(t, s.Users(), 1)
}

func TestValidateUserConfig_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  string
		cfg  UserConfig
		want string
	}{
		{"empty key", "", UserConfig{}, "user must not be empty"},
		{"bad type", "a", UserConfig{Authenticators: []AuthenticatorConfig{{Type: "sms"}}},
			"authenticators[0]: type must be one of otp, oob, recovery-code"},
		{"bad channel", "a", UserConfig{Authenticators: []AuthenticatorConfig{{Type: TypeOTP}, {Type: TypeOOB, OOBChannel: "pigeon"}}},
			"authenticators[1]: oob_channel must be one of sms, voice, email, auth0"},
		{"missing channel", "a", UserConfig{Authenticators: []AuthenticatorConfig{{Type: TypeOOB}}},
			"authenticators[0]: oob_channel must be one of sms, voice, email, auth0"},
		{"channel on otp", "a", UserConfig{Authenticators: []AuthenticatorConfig{{Type: TypeOTP, OOBChannel: ChannelSMS}}},
			"authenticators[0]: oob_channel is only allowed for oob"},
		{"duplicate id", "a", UserConfig{Authenticators: []AuthenticatorConfig{{ID: "x", Type: TypeOTP}, {ID: "x", Type: TypeOTP}}},
			`authenticators[1]: duplicate id "x"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateUserConfig(tt.key, tt.cfg)
			require.EqualError(t, err, tt.want)
			_, err = NewStore().SetUser(tt.key, tt.cfg)
			require.EqualError(t, err, tt.want)
		})
	}
	require.NoError(t, ValidateUserConfig("a", UserConfig{Authenticators: []AuthenticatorConfig{{Type: TypeOOB, OOBChannel: ChannelEmail}}}))
}

func TestLookup_SubjectThenLogin(t *testing.T) {
	t.Parallel()
	s := NewStore()
	_, err := s.SetUser("alice@example.com", UserConfig{Required: ptr(true)})
	require.NoError(t, err)

	key, _, ok := s.Lookup(Context{Subject: "email|abc", Login: "alice@example.com"})
	require.True(t, ok)
	assert.Equal(t, "alice@example.com", key)

	_, err = s.SetUser("email|abc", UserConfig{Required: ptr(false)})
	require.NoError(t, err)
	key, cfg, ok := s.Lookup(Context{Subject: "email|abc", Login: "alice@example.com"})
	require.True(t, ok)
	assert.Equal(t, "email|abc", key)
	assert.False(t, *cfg.Required)

	_, _, ok = s.Lookup(Context{})
	assert.False(t, ok)
}

func TestNeeded(t *testing.T) {
	t.Parallel()
	s := NewStore()
	ctx := Context{Subject: "u1"}
	assert.False(t, s.Needed(ctx))

	_, err := s.SetUser("u1", UserConfig{Required: ptr(true)})
	require.NoError(t, err)
	assert.True(t, s.Needed(ctx))

	s.SetRequired(true)
	_, err = s.SetUser("u1", UserConfig{Required: ptr(false)})
	require.NoError(t, err)
	assert.False(t, s.Needed(ctx))

	_, err = s.SetUser("u1", UserConfig{})
	require.NoError(t, err)
	assert.True(t, s.Needed(ctx))

	assert.True(t, s.Needed(Context{Subject: "other"}))
	s.SetRequired(false)
	assert.False(t, s.Needed(Context{Subject: "other"}))
}

func TestOOB_IssueAndExpiry(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := NewStore(WithNow(func() time.Time { return now }))
	code := s.IssueOOB("tok", "id1")
	require.NotEmpty(t, code)

	got, ok := s.OOB(code)
	require.True(t, ok)
	assert.Equal(t, OOBCode{MFAToken: "tok", AuthenticatorID: "id1"}, got)

	_, ok = s.OOB("nope")
	assert.False(t, ok)

	now = now.Add(DefaultTokenTTL + time.Second)
	_, ok = s.OOB(code)
	assert.False(t, ok)
}

func TestOOB_SweptOnIssue(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := NewStore(WithNow(func() time.Time { return now }))
	s.IssueOOB("tok", "id1")
	now = now.Add(DefaultTokenTTL + time.Second)
	s.Issue(Context{})
	s.mu.Lock()
	defer s.mu.Unlock()
	assert.Empty(t, s.oob)
}

func TestReset_ClearsUsersAndOOB(t *testing.T) {
	t.Parallel()
	s := NewStore()
	_, err := s.SetUser("alice", UserConfig{})
	require.NoError(t, err)
	code := s.IssueOOB("tok", "id1")
	s.SetRequired(true)

	s.Reset()

	assert.Empty(t, s.Users())
	_, ok := s.OOB(code)
	assert.False(t, ok)
	assert.False(t, s.IsRequired())
}

func TestGetUser_ReturnsCopy(t *testing.T) {
	t.Parallel()
	s := NewStore()
	_, err := s.SetUser("alice", UserConfig{Authenticators: []AuthenticatorConfig{{Type: TypeOTP}}})
	require.NoError(t, err)

	got, _ := s.GetUser("alice")
	got.Authenticators[0].Type = TypeOOB
	*got.Authenticators[0].Active = false

	again, ok := s.GetUser("alice")
	require.True(t, ok)
	assert.Equal(t, TypeOTP, again.Authenticators[0].Type)
	assert.True(t, *again.Authenticators[0].Active)

	assert.True(t, s.DeleteUser("alice"))
	assert.False(t, s.DeleteUser("alice"))
}
