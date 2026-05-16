// Package pkce implements the in-memory store of PKCE challenges that link a
// /authorize redirect to its later /oauth/token exchange.
//
// Auth0 (and OAuth 2.1 generally) requires SPAs and native clients to send a
// code_challenge + code_challenge_method on /authorize and the matching
// code_verifier on /oauth/token. The server stores the challenge keyed by the
// generated code; when the client exchanges that code it must present a
// verifier that hashes back to the stored challenge.
package pkce

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sync"
	"time"
)

// Method is the code_challenge_method value the client used at /authorize.
type Method string

const (
	// MethodS256 hashes the verifier with SHA-256.
	MethodS256 Method = "S256"
	// MethodPlain accepts the verifier as-is. Discouraged; supported for parity.
	MethodPlain Method = "plain"
)

// DefaultTTL is how long a stored challenge is valid before the matching code
// becomes unredeemable.
const DefaultTTL = 10 * time.Minute

// Entry is the data stashed at /authorize and consulted at /oauth/token.
type Entry struct {
	Challenge string
	Method    Method
	ClientID  string
	Redirect  string
	expiresAt time.Time
}

// Store maps the random code returned by /authorize to its PKCE Entry.
// Entries expire after DefaultTTL. Safe for concurrent use.
type Store struct {
	mu      sync.Mutex
	entries map[string]Entry
	ttl     time.Duration
	now     func() time.Time
}

// NewStore returns an empty Store with the default TTL.
func NewStore() *Store {
	return &Store{
		entries: make(map[string]Entry),
		ttl:     DefaultTTL,
		now:     time.Now,
	}
}

// Put records a challenge for the given code. The entry is single-use and
// expires after DefaultTTL.
func (s *Store) Put(code string, e Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	e.expiresAt = s.now().Add(s.ttl)
	s.entries[code] = e
}

// Consume returns the Entry registered against code and removes it. Returns
// false if the code is unknown or its entry has expired.
func (s *Store) Consume(code string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[code]
	if !ok {
		return Entry{}, false
	}
	delete(s.entries, code)
	if s.now().After(e.expiresAt) {
		return Entry{}, false
	}
	return e, true
}

// Verify reports whether the supplied verifier matches the entry's challenge
// per the entry's Method. Returns a descriptive error on mismatch.
func (e *Entry) Verify(verifier string) error {
	if verifier == "" {
		return fmt.Errorf("missing code_verifier")
	}
	switch e.Method {
	case MethodS256:
		sum := sha256.Sum256([]byte(verifier))
		got := base64.RawURLEncoding.EncodeToString(sum[:])
		if got != e.Challenge {
			return fmt.Errorf("S256 mismatch")
		}
	case MethodPlain:
		if verifier != e.Challenge {
			return fmt.Errorf("plain mismatch")
		}
	default:
		return fmt.Errorf("unsupported code_challenge_method %q", e.Method)
	}
	return nil
}

// Reset drops every stored challenge.
func (s *Store) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = make(map[string]Entry)
}

// sweepLocked removes expired entries. Caller must hold s.mu.
func (s *Store) sweepLocked() {
	now := s.now()
	for code, e := range s.entries {
		if now.After(e.expiresAt) {
			delete(s.entries, code)
		}
	}
}
