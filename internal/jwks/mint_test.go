package jwks

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
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

	claims, err := ks.Verify(tok, VerifyOpts{})
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

	_, err = ks.Verify(tok, VerifyOpts{})
	assert.Error(t, err)
}

func TestVerify_RejectsWrongIssuer(t *testing.T) {
	ks1 := newTestKeySet(t)
	ks2, err := NewKeySet(Config{Issuer: "https://other/", AccessTokenTTL: time.Hour})
	require.NoError(t, err)

	tok, err := ks2.Mint(MintOpts{Subject: "x", Audience: []string{"a"}, TTL: time.Hour})
	require.NoError(t, err)

	_, err = ks1.Verify(tok, VerifyOpts{})
	assert.Error(t, err)
}

// TestVerify_RejectsAlgConfusion_HS256 catches the classic "I'll re-sign your
// RS256 token with HMAC-SHA256 using the public key as the secret" attack.
// Verify pins SigningMethodRSA + the WithValidMethods({"RS256"}) jwt option;
// both gates must reject the HS256 token before it reaches signature check.
func TestVerify_RejectsAlgConfusion_HS256(t *testing.T) {
	t.Parallel()
	ks := newTestKeySet(t)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": "https://mock/",
		"sub": "evil@clients",
		"aud": "https://api/",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	signed, err := tok.SignedString([]byte("would-be-public-key-bytes"))
	require.NoError(t, err)

	_, err = ks.Verify(signed, VerifyOpts{})
	require.Error(t, err)
}

// TestVerify_RejectsNoneAlg catches the equally classic alg=none attack: a
// token with no signature at all, where a naive verifier might believe the
// header.
func TestVerify_RejectsNoneAlg(t *testing.T) {
	t.Parallel()
	ks := newTestKeySet(t)
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"iss": "https://mock/",
		"sub": "evil@clients",
		"aud": "https://api/",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	signed, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	_, err = ks.Verify(signed, VerifyOpts{})
	require.Error(t, err)
}

// TestVerify_RejectsMalformed covers structural failures: garbage strings,
// missing segments, and base64-noise. Each must return an error rather than
// panic.
func TestVerify_RejectsMalformed(t *testing.T) {
	t.Parallel()
	ks := newTestKeySet(t)
	for _, in := range []string{
		"",
		"not-a-jwt",
		"only.two",
		"a.b",                     // Two-segment, almost-token.
		"a.b.c.d",                 // Four-segment.
		strings.Repeat("a", 4096), // Long garbage.
	} {
		t.Run(in, func(t *testing.T) {
			_, err := ks.Verify(in, VerifyOpts{})
			assert.Error(t, err)
		})
	}
}

// TestMint_UsesConfigNow_FrozenIat confirms that Config.Now drives
// the iat/exp values Mint stamps on the token — the load-bearing
// wire for clock control on the issuance side.
func TestMint_UsesConfigNow_FrozenIat(t *testing.T) {
	t0 := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	ks, err := NewKeySet(Config{
		Issuer:         "https://mock/",
		AccessTokenTTL: time.Hour,
		Now:            func() time.Time { return t0 },
	})
	require.NoError(t, err)

	tok, err := ks.Mint(MintOpts{
		Subject:  "demo",
		Audience: []string{"https://api/"},
		TTL:      24 * time.Hour,
	})
	require.NoError(t, err)

	parsed, _, err := new(jwt.Parser).ParseUnverified(tok, jwt.MapClaims{})
	require.NoError(t, err)
	claims := parsed.Claims.(jwt.MapClaims)

	assert.Equal(t, float64(t0.Unix()), claims["iat"])
	assert.Equal(t, float64(t0.Add(24*time.Hour).Unix()), claims["exp"])
}

// TestVerify_UsesConfigNow_RejectsExpiredAfterAdvance confirms that
// Config.Now drives the validator's notion of "now" as well — the
// load-bearing wire for clock control on the validation side.
func TestVerify_UsesConfigNow_RejectsExpiredAfterAdvance(t *testing.T) {
	mintAt := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	var verifyNow time.Time
	ks, err := NewKeySet(Config{
		Issuer:         "https://mock/",
		AccessTokenTTL: time.Hour,
		Now:            func() time.Time { return verifyNow },
	})
	require.NoError(t, err)

	verifyNow = mintAt
	tok, err := ks.Mint(MintOpts{
		Subject:  "demo",
		Audience: []string{"https://api/"},
		TTL:      time.Hour,
	})
	require.NoError(t, err)

	// Verify immediately succeeds.
	_, err = ks.Verify(tok, VerifyOpts{})
	require.NoError(t, err)

	// Advance verifier's clock past exp + leeway (verifyLeeway = 60s).
	verifyNow = mintAt.Add(time.Hour + 5*time.Minute)
	_, err = ks.Verify(tok, VerifyOpts{})
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "expired")
}

// TestNewKeySet_NilNow_DefaultsToTimeNow keeps the zero-value
// Config{} usable so existing tests don't break.
func TestNewKeySet_NilNow_DefaultsToTimeNow(t *testing.T) {
	ks, err := NewKeySet(Config{
		Issuer:         "https://mock/",
		AccessTokenTTL: time.Hour,
		// Now intentionally omitted.
	})
	require.NoError(t, err)

	tok, err := ks.Mint(MintOpts{Subject: "demo", Audience: []string{"a"}, TTL: time.Hour})
	require.NoError(t, err)
	parsed, _, err := new(jwt.Parser).ParseUnverified(tok, jwt.MapClaims{})
	require.NoError(t, err)
	claims := parsed.Claims.(jwt.MapClaims)

	iat := int64(claims["iat"].(float64))
	wall := time.Now().Unix()
	diff := wall - iat
	if diff < 0 {
		diff = -diff
	}
	assert.LessOrEqual(t, diff, int64(5))
}
