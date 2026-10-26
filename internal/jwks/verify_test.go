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
	assert.Error(t, err)

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
	assert.Error(t, err)
}
