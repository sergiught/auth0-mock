// Package permissions owns a per-audience map of permission strings. When the
// authapi package mints an access token, it consults this store to inject the
// matching audience's permissions as a "permissions" claim on the JWT.
//
// Tests use PUT /admin0/permissions/{audience} (and friends) to shape RBAC at
// runtime without restarting the service.
package permissions

import (
	"slices"
	"sync"
)

// Store holds permissions keyed by audience. Safe for concurrent use.
type Store struct {
	mu    sync.RWMutex
	perms map[string][]string
}

// NewStore returns an empty Store.
func NewStore() *Store {
	return &Store{perms: make(map[string][]string)}
}

// Set replaces the permissions for an audience with the given list.
func (s *Store) Set(audience string, perms []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.perms[audience] = slices.Clone(perms)
}

// Get returns a copy of the permissions for an audience, or nil if none are
// registered.
func (s *Store) Get(audience string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	perms, ok := s.perms[audience]
	if !ok {
		return nil
	}
	return slices.Clone(perms)
}

// Delete removes any permissions registered for an audience.
func (s *Store) Delete(audience string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.perms, audience)
}

// Clear removes permissions for every audience.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.perms = make(map[string][]string)
}

// All returns a snapshot of every audience and its permissions.
func (s *Store) All() map[string][]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string][]string, len(s.perms))
	for k, v := range s.perms {
		out[k] = slices.Clone(v)
	}
	return out
}
