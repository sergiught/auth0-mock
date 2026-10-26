// Package session keeps the in-memory state that ties a logged-in user to
// the tokens the mock has issued, so later requests (/userinfo) can see the
// same user the id_token described.
package session

import (
	"crypto/rand"
	"encoding/base64"
	"maps"
	"slices"
	"sync"
	"time"
)

// MaxEntries bounds how many user profiles, and separately how many
// refresh grants, opaque access tokens and login sessions, the store keeps; the oldest
// inserted entry of a kind is evicted when a new one arrives at the limit.
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

	logins     map[string]Login
	loginOrder []string // Login ids in insertion order, oldest first.
}

// Login is a mock login session: who signed in and when, kept server-side
// behind the auth0 cookie so a later prompt=none request can answer for them.
type Login struct {
	Subject  string
	AuthTime time.Time
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
	return &Store{profiles: map[string]map[string]any{}, refresh: map[string]Grant{}, access: map[string]Access{}, logins: map[string]Login{}}
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
	s.logins = map[string]Login{}
	s.loginOrder = nil
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

// StartLogin records a login session for subject and returns its random id.
// It is bounded by MaxEntries like the other maps, evicting the oldest first.
func (s *Store) StartLogin(subject string, authTime time.Time) string {
	// The system random source does not fail on supported platforms.
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	id := base64.RawURLEncoding.EncodeToString(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.loginOrder) >= MaxEntries {
		delete(s.logins, s.loginOrder[0])
		s.loginOrder = s.loginOrder[1:]
	}
	s.loginOrder = append(s.loginOrder, id)
	s.logins[id] = Login{Subject: subject, AuthTime: authTime}
	return id
}

// Login returns the login session with the given id.
func (s *Store) Login(id string) (Login, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.logins[id]
	return l, ok
}

// EndLogin forgets the login session with the given id; unknown ids are ignored.
func (s *Store) EndLogin(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.logins[id]; !ok {
		return
	}
	delete(s.logins, id)
	if i := slices.Index(s.loginOrder, id); i >= 0 {
		s.loginOrder = slices.Delete(s.loginOrder, i, i+1)
	}
}
