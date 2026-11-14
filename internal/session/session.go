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

	refresh      map[string]refreshEntry // Refresh tokens with their chain and used flag.
	refreshOrder []string                // Refresh tokens in insertion order, oldest first.

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
	Subject, ClientID, Scope, Issuer string
	ExpiresAt                        time.Time
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
	return &Store{profiles: map[string]map[string]any{}, refresh: map[string]refreshEntry{}, access: map[string]Access{}, logins: map[string]Login{}}
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
	s.refresh = map[string]refreshEntry{}
	s.refreshOrder = nil
	s.access = map[string]Access{}
	s.accessOrder = nil
	s.logins = map[string]Login{}
	s.loginOrder = nil
}

// refreshEntry is a stored refresh token: its grant, the chain it belongs to
// (the first token issued by a login) and whether strict mode has redeemed it.
type refreshEntry struct {
	grant Grant
	chain string
	used  bool
}

// RedeemResult is the outcome of RedeemRefresh.
type RedeemResult int

// Outcomes of RedeemRefresh.
const (
	RedeemOK             RedeemResult = iota
	RedeemUnknown                     // Never issued, evicted, or its chain was revoked.
	RedeemClientMismatch              // Grant returned so the caller can name the issuing client; nothing changes.
	RedeemReused                      // Token was already used; its chain is now revoked.
)

// storeRefresh inserts e under token, evicting the oldest token at the cap.
// The caller holds s.mu.
func (s *Store) storeRefresh(token string, e refreshEntry) {
	if _, exists := s.refresh[token]; !exists {
		if len(s.refreshOrder) >= MaxEntries {
			delete(s.refresh, s.refreshOrder[0])
			s.refreshOrder = s.refreshOrder[1:]
		}
		s.refreshOrder = append(s.refreshOrder, token)
	}
	e.grant.Profile = maps.Clone(e.grant.Profile)
	s.refresh[token] = e
}

// revokeChain deletes every token of chain. The caller holds s.mu.
func (s *Store) revokeChain(chain string) {
	for tok, e := range s.refresh {
		if e.chain == chain {
			delete(s.refresh, tok)
		}
	}
	s.refreshOrder = slices.DeleteFunc(s.refreshOrder, func(tok string) bool {
		_, ok := s.refresh[tok]
		return !ok
	})
}

// RememberRefresh stores a copy of g under the refresh token, starting a new
// chain. It is bounded by MaxEntries like profiles, evicting the oldest token
// first.
func (s *Store) RememberRefresh(token string, g Grant) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.storeRefresh(token, refreshEntry{grant: g, chain: token})
}

// Refresh returns a copy of the grant remembered for the refresh token, used
// or not.
func (s *Store) Refresh(token string) (Grant, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.refresh[token]
	if !ok {
		return Grant{}, false
	}
	g := e.grant
	g.Profile = maps.Clone(g.Profile)
	return g, true
}

// RedeemRefresh atomically judges a strict refresh. Order: unknown,
// client mismatch (only when clientID and the stored client are both
// non-empty), already used (revokes the chain), else marks the token used.
func (s *Store) RedeemRefresh(token, clientID string) (Grant, RedeemResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.refresh[token]
	if !ok {
		return Grant{}, RedeemUnknown
	}
	g := e.grant
	g.Profile = maps.Clone(g.Profile)
	if clientID != "" && g.ClientID != "" && clientID != g.ClientID {
		return g, RedeemClientMismatch
	}
	if e.used {
		s.revokeChain(e.chain)
		return g, RedeemReused
	}
	e.used = true
	s.refresh[token] = e
	return g, RedeemOK
}

// RotateRefresh stores token as the next member of old's chain. When old is
// no longer stored (evicted or revoked) it stores nothing.
func (s *Store) RotateRefresh(old, token string, g Grant) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, ok := s.refresh[old]
	if !ok {
		return
	}
	s.storeRefresh(token, refreshEntry{grant: g, chain: prev.chain})
}

// RevokeRefreshChain deletes every token in token's chain. Unknown tokens are ignored.
func (s *Store) RevokeRefreshChain(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.refresh[token]; ok {
		s.revokeChain(e.chain)
	}
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
