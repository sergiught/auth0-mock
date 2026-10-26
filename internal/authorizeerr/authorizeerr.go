// Package authorizeerr keeps the in-memory rules that make /authorize fail
// on purpose: a rule with an optional match on the /authorize query returns
// an OAuth error to the redirect_uri instead of logging the user in.
package authorizeerr

import (
	"errors"
	"maps"
	"net/url"
	"sync"

	"github.com/google/uuid"

	"github.com/sergiught/auth0-mock/internal/matches"
)

// Rule is one registered authorize failure.
type Rule struct {
	ID               string            `json:"id"`
	Match            map[string]string `json:"match,omitempty"`
	Error            string            `json:"error"`
	ErrorDescription string            `json:"error_description,omitempty"`
}

// Store is a mutex-guarded rule registry.
type Store struct {
	mu    sync.Mutex
	rules []Rule // Registration order, oldest first.
}

// NewStore returns an empty Store.
func NewStore() *Store {
	return &Store{}
}

// Add stores a copy of r under a fresh ID (any inbound ID is overwritten)
// and returns the stored copy.
func (s *Store) Add(r Rule) Rule {
	r.ID = uuid.NewString()
	r.Match = maps.Clone(r.Match)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules = append(s.rules, r)
	return clone(r)
}

// List returns copies of every rule in registration order.
func (s *Store) List() []Rule {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Rule, len(s.rules))
	for i, r := range s.rules {
		out[i] = clone(r)
	}
	return out
}

// Delete removes the rule with the given ID and reports whether it existed.
func (s *Store) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.rules {
		if r.ID == id {
			s.rules = append(s.rules[:i:i], s.rules[i+1:]...)
			return true
		}
	}
	return false
}

// Clear removes every rule.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules = nil
}

// Find returns a copy of the newest rule whose Match is a subset of q, or
// nil when none applies.
func (s *Store) Find(q url.Values) *Rule {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.rules) - 1; i >= 0; i-- {
		r := s.rules[i]
		m := matches.RequestMatcher{Query: r.Match}
		if m.Matches(matches.MatchableRequest{Query: q}) {
			c := clone(r)
			return &c
		}
	}
	return nil
}

func clone(r Rule) Rule {
	r.Match = maps.Clone(r.Match)
	return r
}

// Validate rejects rules that cannot be put in a redirect Location safely.
// Both fields are limited to the printable ASCII OAuth permits in error
// values (RFC 6749 section 4.1.2.1): no quote, backslash, control or
// non-ASCII characters.
func Validate(r Rule) error {
	if r.Error == "" {
		return errors.New("error is required")
	}
	if !oauthSafe(r.Error) {
		return errors.New("error contains characters OAuth does not allow")
	}
	if !oauthSafe(r.ErrorDescription) {
		return errors.New("error_description contains characters OAuth does not allow")
	}
	return nil
}

func oauthSafe(s string) bool {
	for _, c := range s {
		if c < 0x20 || c > 0x7E || c == '"' || c == '\\' {
			return false
		}
	}
	return true
}
