package jwks

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestKeySet(t *testing.T) *KeySet {
	t.Helper()
	ks, err := NewKeySet(Config{
		Issuer:         "https://mock/",
		AccessTokenTTL: time.Hour,
	})
	require.NoError(t, err)
	return ks
}

func TestMint_RoundTripsThroughVerify(t *testing.T) {
	ks := newTestKeySet(t)
	tok, err := ks.Mint(MintOpts{
		Subject:  "abc@clients",
		Audience: []string{"https://api/"},
		Scope:    "read:users",
		TTL:      time.Hour,
		Extra:    map[string]any{"gty": "client-credentials"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, tok)

	claims, err := ks.Verify(tok)
	require.NoError(t, err)
	assert.Equal(t, "abc@clients", claims.Subject)
	assert.Equal(t, "https://mock/", claims.Issuer)
	assert.Contains(t, claims.Audience, "https://api/")
	assert.Equal(t, "read:users", claims.Scope)
	assert.Equal(t, "client-credentials", claims.Extra["gty"])
}

func TestVerify_RejectsExpired(t *testing.T) {
	ks := newTestKeySet(t)
	tok, err := ks.Mint(MintOpts{Subject: "x", Audience: []string{"a"}, TTL: -time.Minute})
	require.NoError(t, err)

	_, err = ks.Verify(tok)
	assert.Error(t, err)
}

func TestVerify_RejectsWrongIssuer(t *testing.T) {
	ks1 := newTestKeySet(t)
	ks2, err := NewKeySet(Config{Issuer: "https://other/", AccessTokenTTL: time.Hour})
	require.NoError(t, err)

	tok, err := ks2.Mint(MintOpts{Subject: "x", Audience: []string{"a"}, TTL: time.Hour})
	require.NoError(t, err)

	_, err = ks1.Verify(tok)
	assert.Error(t, err)
}
