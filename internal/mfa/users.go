package mfa

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"time"

	"github.com/google/uuid"
)

// Authenticator types.
const (
	TypeOTP          = "otp"
	TypeOOB          = "oob"
	TypeRecoveryCode = "recovery-code"
)

// Out-of-band channels.
const (
	ChannelSMS   = "sms"
	ChannelVoice = "voice"
	ChannelEmail = "email"
	ChannelPush  = "auth0"
)

const (
	idAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	idLen      = 16
)

// AuthenticatorConfig is one enrolled authenticator of a user.
type AuthenticatorConfig struct {
	ID         string `json:"id,omitempty"`
	Type       string `json:"type"`
	OOBChannel string `json:"oob_channel,omitempty"`
	Name       string `json:"name,omitempty"`
	Active     *bool  `json:"active,omitempty"`
	// Group ties authenticators that were enrolled together.
	Group string `json:"group,omitempty"`
}

// ErrAlreadyEnrolled means the user already has an active authenticator.
var ErrAlreadyEnrolled = errors.New("user is already enrolled")

// UserConfig is the per-user MFA configuration.
type UserConfig struct {
	Required       *bool                 `json:"required,omitempty"`
	Authenticators []AuthenticatorConfig `json:"authenticators"`
}

// OOBCode is the state behind an issued oob_code.
type OOBCode struct {
	MFAToken        string
	AuthenticatorID string
}

type oobEntry struct {
	OOBCode
	expiresAt time.Time
}

func channelValid(c string) bool {
	return slices.Contains([]string{ChannelSMS, ChannelVoice, ChannelEmail, ChannelPush}, c)
}

// ValidateUserConfig checks key and cfg without storing anything.
func ValidateUserConfig(key string, cfg UserConfig) error {
	if key == "" {
		return errors.New("user must not be empty")
	}
	seen := make(map[string]bool)
	for i, a := range cfg.Authenticators {
		switch a.Type {
		case TypeOTP, TypeRecoveryCode:
			if a.OOBChannel != "" {
				return fmt.Errorf("authenticators[%d]: oob_channel is only allowed for oob", i)
			}
		case TypeOOB:
			if !channelValid(a.OOBChannel) {
				return fmt.Errorf("authenticators[%d]: oob_channel must be one of sms, voice, email, auth0", i)
			}
		default:
			return fmt.Errorf("authenticators[%d]: type must be one of otp, oob, recovery-code", i)
		}
		if a.ID != "" {
			if seen[a.ID] {
				return fmt.Errorf("authenticators[%d]: duplicate id %q", i, a.ID)
			}
			seen[a.ID] = true
		}
	}
	return nil
}

func idPrefix(a AuthenticatorConfig) string {
	switch a.Type {
	case TypeOTP:
		return "totp"
	case TypeRecoveryCode:
		return TypeRecoveryCode
	}
	if a.OOBChannel == ChannelPush {
		return "push"
	}
	return a.OOBChannel
}

func randomSuffix() string {
	limit := big.NewInt(int64(len(idAlphabet)))
	b := make([]byte, idLen)
	for i := range b {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			panic(err)
		}
		b[i] = idAlphabet[n.Int64()]
	}
	return string(b)
}

func cloneConfig(cfg UserConfig) UserConfig {
	out := UserConfig{Authenticators: make([]AuthenticatorConfig, len(cfg.Authenticators))}
	if cfg.Required != nil {
		v := *cfg.Required
		out.Required = &v
	}
	for i, a := range cfg.Authenticators {
		if a.Active != nil {
			v := *a.Active
			a.Active = &v
		}
		out.Authenticators[i] = a
	}
	return out
}

// fillIDs gives every authenticator without an ID a unique generated one.
func fillIDs(auths []AuthenticatorConfig) {
	used := make(map[string]bool)
	for _, a := range auths {
		if a.ID != "" {
			used[a.ID] = true
		}
	}
	for i := range auths {
		a := &auths[i]
		if a.ID != "" {
			continue
		}
		for {
			id := idPrefix(*a) + "|dev_" + randomSuffix()
			if !used[id] {
				a.ID = id
				used[id] = true
				break
			}
		}
	}
}

// SetUser validates and stores cfg for key, replacing any existing config. It
// fills missing authenticator IDs, defaults Active to true and returns the
// stored copy.
func (s *Store) SetUser(key string, cfg UserConfig) (UserConfig, error) {
	if err := ValidateUserConfig(key, cfg); err != nil {
		return UserConfig{}, err
	}
	cfg = cloneConfig(cfg)
	fillIDs(cfg.Authenticators)
	for i := range cfg.Authenticators {
		a := &cfg.Authenticators[i]
		if a.Active == nil {
			v := true
			a.Active = &v
		}
	}
	s.mu.Lock()
	s.users[key] = cfg
	s.mu.Unlock()
	return cloneConfig(cfg), nil
}

// Enroll appends pending authenticators sharing one new group to the config
// for key, creating it when missing. A recovery code is added when the config
// has none. It returns copies of the appended entries.
func (s *Store) Enroll(key string, add []AuthenticatorConfig) ([]AuthenticatorConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := UserConfig{}
	if cur, ok := s.users[key]; ok {
		cfg = cloneConfig(cur)
	}
	hasRecovery := false
	for _, a := range cfg.Authenticators {
		if a.Type == TypeRecoveryCode {
			hasRecovery = true
		} else if a.Active != nil && *a.Active {
			return nil, ErrAlreadyEnrolled
		}
	}
	added := cloneConfig(UserConfig{Authenticators: add}).Authenticators
	if !hasRecovery {
		added = append(added, AuthenticatorConfig{Type: TypeRecoveryCode})
	}
	group := uuid.NewString()
	for i := range added {
		added[i].Active = new(bool)
		added[i].Group = group
	}
	cfg.Authenticators = append(cfg.Authenticators, added...)
	if err := ValidateUserConfig(key, cfg); err != nil {
		return nil, err
	}
	fillIDs(cfg.Authenticators)
	s.users[key] = cfg
	return cloneConfig(UserConfig{Authenticators: cfg.Authenticators[len(cfg.Authenticators)-len(added):]}).Authenticators, nil
}

// Activate marks id and every authenticator in its group active. The user's
// recovery code is activated too: it belongs to the user, not to one
// enrollment, so a repeated /mfa/associate before confirmation still leaves
// the recovery code from the first call usable.
func (s *Store) Activate(key, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, ok := s.users[key]
	if !ok {
		return false
	}
	idx := slices.IndexFunc(cfg.Authenticators, func(a AuthenticatorConfig) bool { return a.ID == id })
	if idx < 0 {
		return false
	}
	group := cfg.Authenticators[idx].Group
	for i := range cfg.Authenticators {
		a := cfg.Authenticators[i]
		if i == idx || (group != "" && a.Group == group) || a.Type == TypeRecoveryCode {
			v := true
			cfg.Authenticators[i].Active = &v
		}
	}
	return true
}

// RemoveAuthenticator deletes id. Removing a push authenticator also removes
// the otp entries of its group.
func (s *Store) RemoveAuthenticator(key, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, ok := s.users[key]
	if !ok {
		return false
	}
	idx := slices.IndexFunc(cfg.Authenticators, func(a AuthenticatorConfig) bool { return a.ID == id })
	if idx < 0 {
		return false
	}
	target := cfg.Authenticators[idx]
	push := target.Type == TypeOOB && target.OOBChannel == ChannelPush && target.Group != ""
	cfg.Authenticators = slices.DeleteFunc(cfg.Authenticators, func(a AuthenticatorConfig) bool {
		return a.ID == id || (push && a.Type == TypeOTP && a.Group == target.Group)
	})
	s.users[key] = cfg
	return true
}

// GetUser returns a copy of the config stored for key.
func (s *Store) GetUser(key string) (UserConfig, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, ok := s.users[key]
	if !ok {
		return UserConfig{}, false
	}
	return cloneConfig(cfg), true
}

// DeleteUser removes the config for key and reports whether one existed.
func (s *Store) DeleteUser(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.users[key]
	delete(s.users, key)
	return ok
}

// Users returns a copy of every stored config.
func (s *Store) Users() map[string]UserConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]UserConfig, len(s.users))
	for k, cfg := range s.users {
		out[k] = cloneConfig(cfg)
	}
	return out
}

// Lookup finds the config for ctx by Subject, then by Login when non-empty.
func (s *Store) Lookup(ctx Context) (string, UserConfig, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range []string{ctx.Subject, ctx.Login} {
		if key == "" {
			continue
		}
		if cfg, ok := s.users[key]; ok {
			return key, cloneConfig(cfg), true
		}
	}
	return "", UserConfig{}, false
}

// Needed reports whether MFA is required for ctx: the user's own Required
// setting when present, else the global flag.
func (s *Store) Needed(ctx Context) bool {
	if _, cfg, ok := s.Lookup(ctx); ok && cfg.Required != nil {
		return *cfg.Required
	}
	return s.IsRequired()
}

// IssueOOB creates an oob_code bound to an mfa_token and authenticator.
func (s *Store) IssueOOB(mfaToken, authenticatorID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	code := uuid.NewString()
	s.oob[code] = oobEntry{
		OOBCode:   OOBCode{MFAToken: mfaToken, AuthenticatorID: authenticatorID},
		expiresAt: s.now().Add(s.ttl),
	}
	return code
}

// OOB returns the state behind code; false when unknown or expired.
func (s *Store) OOB(code string) (OOBCode, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.oob[code]
	if !ok {
		return OOBCode{}, false
	}
	if s.now().After(e.expiresAt) {
		delete(s.oob, code)
		return OOBCode{}, false
	}
	return e.OOBCode, true
}
