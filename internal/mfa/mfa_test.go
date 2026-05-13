package mfa

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStore_RequiredFlag(t *testing.T) {
	s := NewStore()
	assert.False(t, s.IsRequired())

	s.SetRequired(true)
	assert.True(t, s.IsRequired())

	s.SetRequired(false)
	assert.False(t, s.IsRequired())
}

func TestStore_IssueConsume(t *testing.T) {
	s := NewStore()
	tok := s.Issue(Context{ClientID: "abc", Audience: "api", Subject: "alice"})
	require.NotEmpty(t, tok)

	ctx, ok := s.Consume(tok)
	require.True(t, ok)
	assert.Equal(t, "alice", ctx.Subject)

	// Single-use: second consume misses.
	_, ok = s.Consume(tok)
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
	tok := s.Issue(Context{Subject: "alice"})
	time.Sleep(80 * time.Millisecond)

	_, ok := s.Consume(tok)
	assert.False(t, ok, "expired tokens should be rejected")
}

func TestStore_Reset_ClearsTokensAndFlag(t *testing.T) {
	s := NewStore()
	s.SetRequired(true)
	tok := s.Issue(Context{Subject: "alice"})

	s.Reset()
	assert.False(t, s.IsRequired())
	_, ok := s.Consume(tok)
	assert.False(t, ok)
}

func TestStore_ConcurrentAccess(t *testing.T) {
	s := NewStore()
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(2)
		go func() { defer wg.Done(); s.Issue(Context{Subject: "x"}) }()
		go func() { defer wg.Done(); s.SetRequired(!s.IsRequired()) }()
	}
	wg.Wait()
}
