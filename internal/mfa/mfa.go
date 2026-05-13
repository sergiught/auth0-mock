// Package mfa implements the in-memory state for Auth0's MFA challenge dance.
//
// Auth0 enforces MFA via a two-step /oauth/token flow:
//
//  1. The initial password / password-realm request returns
//     403 { "error": "mfa_required", "mfa_token": "..." }
//     instead of minting.
//  2. The client re-calls /oauth/token with one of the MFA grants
//     (mfa-otp, mfa-oob, mfa-recovery-code) and presents the user's factor.
//
// Whether MFA is required at all is controlled by a runtime flag mutated via
// /admin0/mfa-required. The accepted challenges are fixed canned values
// (matching the spirit of /passwordless/verify accepting "000000"):
//
//	OTP            = "123456"
//	BindingCode    = "123456"
//	RecoveryCode   = "ABCDEFGHIJKLMNOP"
package mfa

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// DefaultTokenTTL is how long an issued mfa_token stays valid before the
// matching challenge becomes unredeemable.
const DefaultTokenTTL = 10 * time.Minute

// Fixed canned challenges accepted by the mock. Tests can rely on them.
const (
	AcceptedOTP          = "123456"
	AcceptedBindingCode  = "123456"
	AcceptedRecoveryCode = "ABCDEFGHIJKLMNOP"
)

// Context carries the token-issuance state from step 1 (the 403 response) to
// step 2 (the MFA grant exchange) so the second request can mint a token
// equivalent to what the first would have produced.
type Context struct {
	ClientID  string
	Audience  string
	Scope     string
	Subject   string
	Realm     string // Empty for plain password grant.
	expiresAt time.Time
}

// Store holds active mfa_tokens and the global "MFA required" flag.
// Safe for concurrent use.
type Store struct {
	mu       sync.Mutex
	tokens   map[string]Context
	ttl      time.Duration
	now      func() time.Time
	required atomic.Bool
}

// NewStore returns an empty Store with the default TTL and MFA disabled.
func NewStore() *Store {
	return &Store{
		tokens: make(map[string]Context),
		ttl:    DefaultTokenTTL,
		now:    time.Now,
	}
}

// SetRequired toggles the global flag. When true, the password and
// password-realm grants return 403 mfa_required + an mfa_token instead of
// minting a token directly.
func (s *Store) SetRequired(b bool) { s.required.Store(b) }

// IsRequired reports whether MFA enforcement is currently on.
func (s *Store) IsRequired() bool { return s.required.Load() }

// Issue creates a fresh mfa_token bound to ctx, valid for the configured TTL.
// Returns the opaque token string the client should present in step 2.
func (s *Store) Issue(ctx Context) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	tok := uuid.NewString()
	ctx.expiresAt = s.now().Add(s.ttl)
	s.tokens[tok] = ctx
	return tok
}

// Consume returns the Context registered against an mfa_token and removes it
// (single-use). Returns false if the token is unknown or expired.
func (s *Store) Consume(token string) (Context, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, ok := s.tokens[token]
	if !ok {
		return Context{}, false
	}
	delete(s.tokens, token)
	if s.now().After(ctx.expiresAt) {
		return Context{}, false
	}
	return ctx, true
}

// Reset clears every issued mfa_token AND turns enforcement off.
func (s *Store) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens = make(map[string]Context)
	s.required.Store(false)
}

func (s *Store) sweepLocked() {
	now := s.now()
	for tok, ctx := range s.tokens {
		if now.After(ctx.expiresAt) {
			delete(s.tokens, tok)
		}
	}
}
