package jwks

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerify_AllowExpiredAcceptsExpiredToken(t *testing.T) {
	ks := newTestKeySet(t)
	tok, err := ks.Mint(MintOpts{Subject: "auth0|u1", Audience: []string{"a"}, TTL: -time.Hour})
	require.NoError(t, err)

	_, err = ks.Verify(tok, VerifyOpts{})
	require.Error(t, err)

	claims, err := ks.Verify(tok, VerifyOpts{AllowExpired: true})
	require.NoError(t, err)
	assert.Equal(t, "auth0|u1", claims.Subject)
}

func TestVerify_AllowExpiredStillChecksSignatureAndIssuer(t *testing.T) {
	ks := newTestKeySet(t)

	sameIssuer, err := NewKeySet(Config{Issuer: "https://mock/", AccessTokenTTL: time.Hour})
	require.NoError(t, err)
	tok, err := sameIssuer.Mint(MintOpts{Subject: "x", Audience: []string{"a"}, TTL: time.Hour})
	require.NoError(t, err)
	_, err = ks.Verify(tok, VerifyOpts{AllowExpired: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signature")

	other, err := NewKeySet(Config{Issuer: "https://other/", AccessTokenTTL: time.Hour})
	require.NoError(t, err)
	tok, err = other.Mint(MintOpts{Subject: "x", Audience: []string{"a"}, TTL: time.Hour})
	require.NoError(t, err)
	_, err = other.Verify(tok, VerifyOpts{AllowExpired: true})
	require.NoError(t, err)

	// A pure issuer mismatch needs matching key material: verify with a copy
	// of the signer whose configured issuer differs from the token's.
	cp := *other
	cp.cfg.Issuer = "https://mock/"
	_, err = cp.Verify(tok, VerifyOpts{AllowExpired: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "issuer")
}

func TestVerify_AllowExpiredStillChecksAudience(t *testing.T) {
	ks := newTestKeySet(t)
	tok, err := ks.Mint(MintOpts{Subject: "x", Audience: []string{"a"}, TTL: -time.Hour})
	require.NoError(t, err)

	_, err = ks.Verify(tok, VerifyOpts{AllowExpired: true, RequireAudience: "nope"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "audience")
}

// leewayKeySet returns a KeySet whose clock is the returned *time.Time.
func leewayKeySet(t *testing.T, leeway time.Duration, t0 time.Time) (*KeySet, *time.Time) {
	t.Helper()
	now := t0
	ks, err := NewKeySet(Config{
		Issuer:         "https://mock/",
		AccessTokenTTL: time.Hour,
		Leeway:         leeway,
		Now:            func() time.Time { return now },
	})
	require.NoError(t, err)
	return ks, &now
}

func mintAtT0(t *testing.T, ks *KeySet) string {
	t.Helper()
	tok, err := ks.Mint(MintOpts{Subject: "x", Audience: []string{"a"}, TTL: time.Hour})
	require.NoError(t, err)
	return tok
}

func TestVerify_ExpiresOneSecondAfterExp(t *testing.T) {
	t0 := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	ks, now := leewayKeySet(t, 0, t0)
	tok := mintAtT0(t, ks)

	*now = t0.Add(time.Hour - time.Second)
	_, err := ks.Verify(tok, VerifyOpts{})
	require.NoError(t, err)

	*now = t0.Add(time.Hour + time.Second)
	_, err = ks.Verify(tok, VerifyOpts{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expired")
}

func TestVerify_LeewayExtendsExpiry(t *testing.T) {
	t0 := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	ks, now := leewayKeySet(t, 60*time.Second, t0)
	tok := mintAtT0(t, ks)

	*now = t0.Add(time.Hour + 30*time.Second)
	_, err := ks.Verify(tok, VerifyOpts{})
	require.NoError(t, err)

	*now = t0.Add(time.Hour + 61*time.Second)
	_, err = ks.Verify(tok, VerifyOpts{})
	require.Error(t, err)
}

func TestVerify_IssuedInFutureNeedsLeeway(t *testing.T) {
	t0 := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		leeway  time.Duration
		wantErr bool
	}{
		"no leeway rejects": {0, true},
		"leeway accepts":    {60 * time.Second, false},
	} {
		t.Run(name, func(t *testing.T) {
			ks, now := leewayKeySet(t, tc.leeway, t0)
			tok := mintAtT0(t, ks)
			*now = t0.Add(-time.Second)
			_, err := ks.Verify(tok, VerifyOpts{})
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
