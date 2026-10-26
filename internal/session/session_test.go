package session

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestStore_RememberAndProfile(t *testing.T) {
	s := NewStore()
	_, ok := s.Profile("alice")
	assert.False(t, ok)

	s.RememberProfile("alice", map[string]any{"name": "Alice"})
	got, ok := s.Profile("alice")
	assert.True(t, ok)
	assert.Equal(t, map[string]any{"name": "Alice"}, got)
}

func TestStore_ProfileIsCopy(t *testing.T) {
	s := NewStore()
	in := map[string]any{"name": "Alice"}
	s.RememberProfile("alice", in)
	in["name"] = "mutated-input"
	got, _ := s.Profile("alice")
	assert.Equal(t, "Alice", got["name"])
	got["name"] = "mutated-output"
	again, _ := s.Profile("alice")
	assert.Equal(t, "Alice", again["name"])
}

func TestStore_LastWriteWins(t *testing.T) {
	s := NewStore()
	s.RememberProfile("alice", map[string]any{"name": "One"})
	s.RememberProfile("alice", map[string]any{"name": "Two"})
	got, _ := s.Profile("alice")
	assert.Equal(t, "Two", got["name"])
}

func TestStore_Clear(t *testing.T) {
	s := NewStore()
	s.RememberProfile("alice", map[string]any{"name": "Alice"})
	s.Clear()
	_, ok := s.Profile("alice")
	assert.False(t, ok)
}

func TestStore_EvictsOldestWhenFull(t *testing.T) {
	s := NewStore()
	for i := 0; i <= MaxEntries; i++ {
		s.RememberProfile(fmt.Sprintf("u%d", i), map[string]any{"i": i})
	}
	_, ok := s.Profile("u0")
	assert.False(t, ok, "oldest evicted")
	_, ok = s.Profile("u1")
	assert.True(t, ok)
	_, ok = s.Profile(fmt.Sprintf("u%d", MaxEntries))
	assert.True(t, ok)

	// Rewriting an existing subject must not evict anyone.
	s.RememberProfile("u1", map[string]any{"i": -1})
	_, ok = s.Profile("u2")
	assert.True(t, ok)
}

func TestStore_Concurrent(t *testing.T) {
	s := NewStore()
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 200 {
				sub := fmt.Sprintf("u%d", (i+j)%10)
				s.RememberProfile(sub, map[string]any{"j": j})
				s.Profile(sub)
			}
		}()
	}
	wg.Wait()
}

func TestStore_RefreshRoundTripAndCopy(t *testing.T) {
	s := NewStore()
	_, ok := s.Refresh("tok")
	assert.False(t, ok)

	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	g := Grant{Subject: "alice", ClientID: "demo", Audience: "api", Scope: "openid", Realm: "db", AuthTime: at,
		Profile: map[string]any{"name": "Alice"}}
	s.RememberRefresh("tok", g)
	g.Profile["name"] = "mutated-input"

	got, ok := s.Refresh("tok")
	assert.True(t, ok)
	assert.Equal(t, "alice", got.Subject)
	assert.Equal(t, at, got.AuthTime)
	assert.Equal(t, "Alice", got.Profile["name"])

	got.Profile["name"] = "mutated-output"
	again, _ := s.Refresh("tok")
	assert.Equal(t, "Alice", again.Profile["name"])
}

func TestStore_ClearForgetsRefreshGrants(t *testing.T) {
	s := NewStore()
	s.RememberRefresh("tok", Grant{Subject: "alice"})
	s.Clear()
	_, ok := s.Refresh("tok")
	assert.False(t, ok)
}

func TestStore_RefreshEvictsOldestAtCap(t *testing.T) {
	s := NewStore()
	for i := 0; i < MaxEntries+1; i++ {
		s.RememberRefresh(fmt.Sprintf("t%d", i), Grant{Subject: "u"})
	}
	_, ok := s.Refresh("t0")
	assert.False(t, ok)
	_, ok = s.Refresh("t1")
	assert.True(t, ok)
	_, ok = s.Refresh(fmt.Sprintf("t%d", MaxEntries))
	assert.True(t, ok)
}
