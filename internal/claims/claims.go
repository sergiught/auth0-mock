// Package claims owns a per-process map of custom JWT claims that get merged
// into every access token minted by the authapi package.
//
// Tests use POST /admin0/claims (and friends) to shape the claim payload at
// runtime without restarting the service.
package claims

import (
	"maps"
	"sync"
)

// Store holds the active custom claims. Safe for concurrent use.
type Store struct {
	mu     sync.RWMutex
	claims map[string]any
}

// NewStore returns an empty Store.
func NewStore() *Store {
	return &Store{claims: make(map[string]any)}
}

// Set replaces the entire claim map.
func (s *Store) Set(claims map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make(map[string]any, len(claims))
	maps.Copy(cp, claims)
	s.claims = cp
}

// Get returns a snapshot of the current claims.
func (s *Store) Get() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make(map[string]any, len(s.claims))
	maps.Copy(cp, s.claims)
	return cp
}

// Clear removes all claims.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claims = make(map[string]any)
}

// MergeInto writes the stored claims into dst. Stored claims overwrite any
// existing key in dst — letting tests override e.g. gty or azp if they want.
func (s *Store) MergeInto(dst map[string]any) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	maps.Copy(dst, s.claims)
}
