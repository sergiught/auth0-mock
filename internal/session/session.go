// Package session keeps the in-memory state that ties a logged-in user to
// the tokens the mock has issued, so later requests (/userinfo) can see the
// same user the id_token described.
package session

import (
	"maps"
	"sync"
	"time"
)

// MaxEntries bounds how many user profiles, and separately how many
// refresh grants and opaque access tokens, the store keeps; the oldest inserted entry of a kind is
// evicted when a new one arrives at the limit.
const MaxEntries = 10000

// Store is a mutex-guarded session registry.
type Store struct {
	mu       sync.Mutex
	profiles map[string]map[string]any
	order    []string // Subjects in insertion order, oldest first.

	refresh      map[string]Grant
	refreshOrder []string // Refresh tokens in insertion order, oldest first.

	access      map[string]Access
	accessOrder []string // Opaque access tokens in insertion order, oldest first.
}

// Access is what the mock remembers about an opaque access token, issued when
// there is no audience to mint a JWT for, so /userinfo can resolve it.
type Access struct {
	Subject, ClientID, Scope string
	ExpiresAt                time.Time
}

// Grant is what the mock remembers about a refresh token it issued, so the
// refresh_token grant can return the same user.
type Grant struct {
	Subject, ClientID, Audience, Scope, Realm string
	AuthTime                                  time.Time
	Profile                                   map[string]any
}

// NewStore returns an empty Store.
func NewStore() *Store {
	return &Store{profiles: map[string]map[string]any{}, refresh: map[string]Grant{}, access: map[string]Access{}}
}

// RememberProfile stores a copy of profile for subject; last write wins.
func (s *Store) RememberProfile(subject string, profile map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.profiles[subject]; !exists {
		if len(s.order) >= MaxEntries {
			delete(s.profiles, s.order[0])
			s.order = s.order[1:]
		}
		s.order = append(s.order, subject)
	}
	s.profiles[subject] = maps.Clone(profile)
}

// Profile returns a copy of the profile last remembered for subject.
func (s *Store) Profile(subject string) (map[string]any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.profiles[subject]
	if !ok {
		return nil, false
	}
	return maps.Clone(p), true
}

// Clear forgets everything.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.profiles = map[string]map[string]any{}
	s.order = nil
	s.refresh = map[string]Grant{}
	s.refreshOrder = nil
	s.access = map[string]Access{}
	s.accessOrder = nil
}

// RememberRefresh stores a copy of g under the refresh token. It is bounded
// by MaxEntries like profiles, evicting the oldest token first.
func (s *Store) RememberRefresh(token string, g Grant) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.refresh[token]; !exists {
		if len(s.refreshOrder) >= MaxEntries {
			delete(s.refresh, s.refreshOrder[0])
			s.refreshOrder = s.refreshOrder[1:]
		}
		s.refreshOrder = append(s.refreshOrder, token)
	}
	g.Profile = maps.Clone(g.Profile)
	s.refresh[token] = g
}

// Refresh returns a copy of the grant remembered for the refresh token.
func (s *Store) Refresh(token string) (Grant, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.refresh[token]
	if !ok {
		return Grant{}, false
	}
	g.Profile = maps.Clone(g.Profile)
	return g, true
}

// RememberAccess stores a under the opaque access token. It is bounded by
// MaxEntries like refresh grants, evicting the oldest token first.
func (s *Store) RememberAccess(token string, a Access) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.access[token]; !exists {
		if len(s.accessOrder) >= MaxEntries {
			delete(s.access, s.accessOrder[0])
			s.accessOrder = s.accessOrder[1:]
		}
		s.accessOrder = append(s.accessOrder, token)
	}
	s.access[token] = a
}

// Access returns the record remembered for the opaque access token.
func (s *Store) Access(token string) (Access, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.access[token]
	return a, ok
}
