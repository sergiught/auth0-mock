package pkce

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStore_PutConsume(t *testing.T) {
	s := NewStore()
	s.Put("code-1", Entry{Challenge: "ch", Method: MethodS256, ClientID: "abc"})

	e, ok := s.Consume("code-1")
	require.True(t, ok)
	assert.Equal(t, "ch", e.Challenge)

	// One-shot: second consume misses.
	_, ok = s.Consume("code-1")
	assert.False(t, ok)
}

func TestStore_ConsumeUnknown(t *testing.T) {
	s := NewStore()
	_, ok := s.Consume("nope")
	assert.False(t, ok)
}

func TestStore_Expiry(t *testing.T) {
	s := NewStore()
	s.ttl = 50 * time.Millisecond
	s.Put("code-1", Entry{Challenge: "ch", Method: MethodS256})
	time.Sleep(80 * time.Millisecond)

	_, ok := s.Consume("code-1")
	assert.False(t, ok, "expired entries should be rejected")
}

func TestEntry_Verify_S256_Match(t *testing.T) {
	verifier := "the-quick-brown-fox-jumps-over-the-lazy-dog-43-chars"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	e := Entry{Challenge: challenge, Method: MethodS256}

	assert.NoError(t, e.Verify(verifier))
}

func TestEntry_Verify_S256_Mismatch(t *testing.T) {
	e := Entry{Challenge: "wrong", Method: MethodS256}
	err := e.Verify("any-verifier")
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "S256 mismatch")
	}
}

func TestEntry_Verify_Plain_Match(t *testing.T) {
	e := Entry{Challenge: "literal", Method: MethodPlain}
	assert.NoError(t, e.Verify("literal"))
}

func TestEntry_Verify_Plain_Mismatch(t *testing.T) {
	e := Entry{Challenge: "literal", Method: MethodPlain}
	err := e.Verify("other")
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "plain mismatch")
	}
}

func TestEntry_Verify_MissingVerifier(t *testing.T) {
	e := Entry{Challenge: "ch", Method: MethodS256}
	err := e.Verify("")
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "missing code_verifier")
	}
}

func TestEntry_Verify_UnsupportedMethod(t *testing.T) {
	e := Entry{Challenge: "ch", Method: "weird"}
	assert.Error(t, e.Verify("anything"))
}

func TestStore_Reset(t *testing.T) {
	s := NewStore()
	s.Put("a", Entry{Challenge: "ch", Method: MethodS256})
	s.Put("b", Entry{Challenge: "ch", Method: MethodS256})

	s.Reset()
	_, ok := s.Consume("a")
	assert.False(t, ok)
	_, ok = s.Consume("b")
	assert.False(t, ok)
}
