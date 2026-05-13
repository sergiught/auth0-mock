package permissions

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStore_SetGet(t *testing.T) {
	s := NewStore()
	assert.Nil(t, s.Get("api"))

	s.Set("api", []string{"read:users", "write:users"})
	assert.ElementsMatch(t, []string{"read:users", "write:users"}, s.Get("api"))
}

func TestStore_GetReturnsSnapshot(t *testing.T) {
	s := NewStore()
	s.Set("api", []string{"read:users"})

	got := s.Get("api")
	got[0] = "mutated"
	assert.Equal(t, "read:users", s.Get("api")[0], "Get returns a copy")
}

func TestStore_All(t *testing.T) {
	s := NewStore()
	s.Set("api1", []string{"a", "b"})
	s.Set("api2", []string{"c"})

	all := s.All()
	assert.Len(t, all, 2)
	assert.ElementsMatch(t, []string{"a", "b"}, all["api1"])
	assert.ElementsMatch(t, []string{"c"}, all["api2"])
}

func TestStore_Delete(t *testing.T) {
	s := NewStore()
	s.Set("api1", []string{"a"})
	s.Set("api2", []string{"b"})

	s.Delete("api1")
	assert.Nil(t, s.Get("api1"))
	assert.NotNil(t, s.Get("api2"))
}

func TestStore_Clear(t *testing.T) {
	s := NewStore()
	s.Set("api1", []string{"a"})
	s.Set("api2", []string{"b"})

	s.Clear()
	assert.Empty(t, s.All())
}

func TestStore_ConcurrentAccess(t *testing.T) {
	s := NewStore()
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(2)
		go func(i int) { defer wg.Done(); s.Set("api", []string{"p"}) }(i)
		go func() { defer wg.Done(); _ = s.Get("api") }()
	}
	wg.Wait()
}
