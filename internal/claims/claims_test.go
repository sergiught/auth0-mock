package claims

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStore_SetGetClear(t *testing.T) {
	t.Parallel()
	s := NewStore()
	assert.Empty(t, s.Get())

	s.Set(map[string]any{"foo": "bar", "n": 1.0})
	got := s.Get()
	assert.Equal(t, "bar", got["foo"])
	assert.Equal(t, 1.0, got["n"])

	// Snapshot is a copy — mutation doesn't leak back.
	got["foo"] = "mutated"
	assert.Equal(t, "bar", s.Get()["foo"])

	s.Clear()
	assert.Empty(t, s.Get())
}

func TestStore_MergeInto_Overwrites(t *testing.T) {
	t.Parallel()
	s := NewStore()
	s.Set(map[string]any{"role": "admin", "gty": "override"})

	dst := map[string]any{"gty": "client-credentials", "azp": "abc"}
	s.MergeInto(dst)

	assert.Equal(t, "admin", dst["role"])
	assert.Equal(t, "override", dst["gty"], "store claims overwrite existing keys")
	assert.Equal(t, "abc", dst["azp"], "keys not in store are preserved")
}

func TestStore_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	s := NewStore()
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(2)
		go func(i int) { defer wg.Done(); s.Set(map[string]any{"i": i}) }(i)
		go func() { defer wg.Done(); _ = s.Get() }()
	}
	wg.Wait()
}
