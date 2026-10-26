// Package session keeps the in-memory state that ties a logged-in user to
// the tokens the mock has issued, so later requests (/userinfo) can see the
// same user the id_token described.
package session

import (
	"maps"
	"sync"
)

// MaxEntries bounds how many user profiles the store keeps; the oldest
// inserted subject is evicted when a new one arrives at the limit.
const MaxEntries = 10000

// Store is a mutex-guarded session registry.
type Store struct {
	mu       sync.Mutex
	profiles map[string]map[string]any
	order    []string // subjects in insertion order, oldest first
}

// NewStore returns an empty Store.
func NewStore() *Store {
	return &Store{profiles: map[string]map[string]any{}}
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
}
