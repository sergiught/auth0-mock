package session

import (
	"encoding/base64"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var userCodeRE = regexp.MustCompile(`^[BCDFGHJKLMNPQRSTVWXZ]{4}-[BCDFGHJKLMNPQRSTVWXZ]{4}$`)

func startDev(s *Store, now time.Time) Device {
	return s.StartDevice(Device{ClientID: "app", Audience: "https://api", Scope: "openid", Expires: now.Add(15 * time.Minute)})
}

func TestStartDevice_Codes(t *testing.T) {
	s := NewStore()
	now := time.Now()
	a, b := startDev(s, now), startDev(s, now)
	raw, err := base64.RawURLEncoding.DecodeString(a.DeviceCode)
	require.NoError(t, err)
	assert.Len(t, raw, 32)
	assert.Regexp(t, userCodeRE, a.UserCode)
	assert.NotEqual(t, a.DeviceCode, b.DeviceCode)
	assert.NotEqual(t, a.UserCode, b.UserCode)
	assert.Equal(t, DevicePending, a.Status)
}

func TestPollDevice_Outcomes(t *testing.T) {
	s := NewStore()
	now := time.Now()

	_, res := s.PollDevice("nope", "app", now)
	assert.Equal(t, PollUnknown, res)

	d := startDev(s, now)
	_, res = s.PollDevice(d.DeviceCode, "other", now)
	assert.Equal(t, PollUnknown, res)
	_, res = s.PollDevice(d.DeviceCode, "app", now)
	assert.Equal(t, PollPending, res)

	require.True(t, s.DenyDevice(d.DeviceCode))
	_, res = s.PollDevice(d.DeviceCode, "app", now)
	assert.Equal(t, PollDenied, res)
	_, res = s.PollDevice(d.DeviceCode, "app", now)
	assert.Equal(t, PollDenied, res)

	d = startDev(s, now)
	require.True(t, s.ApproveDevice(d.DeviceCode, "auth0|u", now))
	got, res := s.PollDevice(d.DeviceCode, "app", now)
	assert.Equal(t, PollApproved, res)
	assert.Equal(t, "auth0|u", got.Subject)
	assert.Equal(t, now, got.AuthTime)
	_, res = s.PollDevice(d.DeviceCode, "app", now)
	assert.Equal(t, PollUnknown, res)

	d = startDev(s, now)
	_, res = s.PollDevice(d.DeviceCode, "app", d.Expires)
	assert.Equal(t, PollExpired, res)
	_, res = s.PollDevice(d.DeviceCode, "app", d.Expires)
	assert.Equal(t, PollUnknown, res)
}

func TestDeviceByUserCode_Normalises(t *testing.T) {
	s := NewStore()
	now := time.Now()
	d := startDev(s, now)
	plain := d.UserCode[:4] + d.UserCode[5:]
	lower := ""
	for _, c := range d.UserCode {
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		lower += string(c)
	}
	for _, v := range []string{d.UserCode, plain, lower, lower[:4] + " " + lower[5:]} {
		got, ok := s.DeviceByUserCode(v, now)
		assert.True(t, ok, v)
		assert.Equal(t, d.DeviceCode, got.DeviceCode)
	}
	_, ok := s.DeviceByUserCode("ZZZZ-ZZZZ", now)
	assert.False(t, ok)
	_, ok = s.DeviceByUserCode(d.UserCode, d.Expires)
	assert.False(t, ok)

	d2 := startDev(s, now)
	require.True(t, s.DenyDevice(d2.DeviceCode))
	_, ok = s.DeviceByUserCode(d2.UserCode, now)
	assert.False(t, ok)
}

func TestDeviceStep(t *testing.T) {
	s := NewStore()
	now := time.Now()
	d := startDev(s, now)

	step, ok := s.SetDeviceStep(d.DeviceCode, "auth0|u", now, now)
	require.True(t, ok)
	raw, err := base64.RawURLEncoding.DecodeString(step)
	require.NoError(t, err)
	assert.Len(t, raw, 32)

	got, ok := s.DeviceByStep(step, now)
	require.True(t, ok)
	assert.Equal(t, "auth0|u", got.Subject)
	assert.Equal(t, now, got.AuthTime)
	_, ok = s.DeviceByStep(step, now)
	assert.True(t, ok)
	_, ok = s.DeviceByStep(step, d.Expires)
	assert.False(t, ok)

	_, ok = s.TakeDeviceStep(step, now)
	assert.True(t, ok)
	_, ok = s.TakeDeviceStep(step, now)
	assert.False(t, ok)

	old, _ := s.SetDeviceStep(d.DeviceCode, "", time.Time{}, now)
	fresh, _ := s.SetDeviceStep(d.DeviceCode, "", time.Time{}, now)
	_, ok = s.DeviceByStep(old, now)
	assert.False(t, ok)
	_, ok = s.DeviceByStep(fresh, now)
	assert.True(t, ok)

	step, ok = s.SetDeviceStep("unknown", "", time.Time{}, now)
	assert.Empty(t, step)
	assert.False(t, ok)
}

func TestSetDeviceStep_OnlyPendingAndLive(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name  string
		setup func(s *Store, d Device)
		at    func(d Device) time.Time
	}{
		{"approved", func(s *Store, d Device) { require.True(t, s.ApproveDevice(d.DeviceCode, "auth0|real", now)) }, func(Device) time.Time { return now }},
		{"denied", func(s *Store, d Device) { require.True(t, s.DenyDevice(d.DeviceCode)) }, func(Device) time.Time { return now }},
		{"expired", func(*Store, Device) {}, func(d Device) time.Time { return d.Expires }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewStore()
			d := startDev(s, now)
			tc.setup(s, d)
			step, ok := s.SetDeviceStep(d.DeviceCode, "auth0|attacker", now, tc.at(d))
			assert.False(t, ok)
			assert.Empty(t, step)
			after, _ := s.PollDevice(d.DeviceCode, "app", now)
			if tc.name == "approved" {
				assert.Equal(t, "auth0|real", after.Subject)
				assert.True(t, after.AuthTime.Equal(now))
			} else {
				assert.Empty(t, after.Subject)
				assert.True(t, after.AuthTime.IsZero())
			}
		})
	}
}

func TestSetDeviceStep_ApprovedSubjectSurvives(t *testing.T) {
	s := NewStore()
	now := time.Now()
	d := startDev(s, now)
	require.True(t, s.ApproveDevice(d.DeviceCode, "auth0|real", now))
	_, ok := s.SetDeviceStep(d.DeviceCode, "auth0|attacker", now, now)
	require.False(t, ok)
	got, res := s.PollDevice(d.DeviceCode, "app", now)
	assert.Equal(t, PollApproved, res)
	assert.Equal(t, "auth0|real", got.Subject)
}

func TestApproveDevice_OnlyPending(t *testing.T) {
	s := NewStore()
	now := time.Now()
	d := startDev(s, now)
	require.True(t, s.DenyDevice(d.DeviceCode))
	assert.False(t, s.ApproveDevice(d.DeviceCode, "x", now))
	assert.False(t, s.DenyDevice("nope"))
}

func TestDevice_ClearAndEviction(t *testing.T) {
	s := NewStore()
	now := time.Now()
	first := startDev(s, now)
	step, _ := s.SetDeviceStep(first.DeviceCode, "", time.Time{}, now)
	for i := 0; i < MaxEntries; i++ {
		startDev(s, now)
	}
	_, res := s.PollDevice(first.DeviceCode, "app", now)
	assert.Equal(t, PollUnknown, res)
	_, ok := s.DeviceByStep(step, now)
	assert.False(t, ok)

	last := startDev(s, now)
	s.Clear()
	_, res = s.PollDevice(last.DeviceCode, "app", now)
	assert.Equal(t, PollUnknown, res)
	_, ok = s.DeviceByUserCode(last.UserCode, now)
	assert.False(t, ok)
}
