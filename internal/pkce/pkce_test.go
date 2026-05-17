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
	t.Parallel()
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
	t.Parallel()
	s := NewStore()
	_, ok := s.Consume("nope")
	assert.False(t, ok)
}

func TestStore_Expiry(t *testing.T) {
	t.Parallel()
	s := NewStore()
	s.ttl = 50 * time.Millisecond
	s.Put("code-1", Entry{Challenge: "ch", Method: MethodS256})
	time.Sleep(80 * time.Millisecond)

	_, ok := s.Consume("code-1")
	assert.False(t, ok, "expired entries should be rejected")
}

// TestEntry_Verify is the full matrix for Entry.Verify across both
// challenge methods (S256 + plain) and every documented failure mode.
// Cases marked wantErrSubstr expect that substring inside the returned
// error; empty wantErrSubstr means success.
func TestEntry_Verify(t *testing.T) {
	t.Parallel()

	s256OK := "the-quick-brown-fox-jumps-over-the-lazy-dog-43-chars"
	sum := sha256.Sum256([]byte(s256OK))
	s256Challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	cases := []struct {
		name          string
		challenge     string
		method        Method
		verifier      string
		wantErrSubstr string // Empty = expect nil error.
	}{
		{
			name:      "S256 match",
			challenge: s256Challenge,
			method:    MethodS256,
			verifier:  s256OK,
		},
		{
			name:          "S256 mismatch",
			challenge:     "wrong",
			method:        MethodS256,
			verifier:      "any-verifier",
			wantErrSubstr: "S256 mismatch",
		},
		{
			name:      "plain match",
			challenge: "literal",
			method:    MethodPlain,
			verifier:  "literal",
		},
		{
			name:          "plain mismatch",
			challenge:     "literal",
			method:        MethodPlain,
			verifier:      "other",
			wantErrSubstr: "plain mismatch",
		},
		{
			name:          "missing verifier",
			challenge:     "ch",
			method:        MethodS256,
			verifier:      "",
			wantErrSubstr: "missing code_verifier",
		},
		{
			name:          "unsupported method",
			challenge:     "ch",
			method:        "weird",
			verifier:      "anything",
			wantErrSubstr: "unsupported", // From "unsupported code_challenge_method".
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			e := Entry{Challenge: c.challenge, Method: c.method}
			err := e.Verify(c.verifier)
			if c.wantErrSubstr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.wantErrSubstr)
		})
	}
}

func TestStore_Reset(t *testing.T) {
	t.Parallel()
	s := NewStore()
	s.Put("a", Entry{Challenge: "ch", Method: MethodS256})
	s.Put("b", Entry{Challenge: "ch", Method: MethodS256})

	s.Reset()
	_, ok := s.Consume("a")
	assert.False(t, ok)
	_, ok = s.Consume("b")
	assert.False(t, ok)
}

func TestStore_Consume_RespectsInjectedClock(t *testing.T) {
	t.Parallel()
	var now time.Time
	s := NewStore(WithNow(func() time.Time { return now }))

	now = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	s.Put("code-1", Entry{
		Challenge: "abc",
		Method:    MethodPlain,
		ClientID:  "demo",
		Redirect:  "https://test/cb",
	})

	// Still within TTL — Consume succeeds.
	e, ok := s.Consume("code-1")
	require.True(t, ok)
	assert.Equal(t, "abc", e.Challenge)

	// Re-put and advance past TTL — Consume fails.
	s.Put("code-2", Entry{Challenge: "abc", Method: MethodPlain})
	now = now.Add(DefaultTTL + time.Second)
	_, ok = s.Consume("code-2")
	assert.False(t, ok)
}

func TestStore_NewStore_NoOptions_UsesTimeNow(t *testing.T) {
	t.Parallel()
	// Backwards-compat smoke test: the no-arg form still works.
	s := NewStore()
	s.Put("c", Entry{Challenge: "abc", Method: MethodPlain})
	_, ok := s.Consume("c")
	assert.True(t, ok)
}
