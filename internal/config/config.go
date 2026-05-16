// Package config loads runtime settings from environment variables.
package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/sergiught/auth0-mock/internal/tlscert"
)

// Specification holds all runtime settings.
type Specification struct {
	HTTPAddress          string         `env:"HTTP_ADDR" envDefault:"127.0.0.1:8080"`
	HTTPSAddress         string         `env:"HTTPS_ADDR" envDefault:"127.0.0.1:8443"`
	TLS                  tlscert.Config // Nested: env tags live on tlscert.Config itself.
	SigningKeyFile       string         `env:"SIGNING_KEY_FILE"`
	IssuerURL            string         `env:"ISSUER_URL" envDefault:"https://localhost:8443/"`
	DefaultAudience      string         `env:"DEFAULT_AUDIENCE" envDefault:"https://localhost:8443/api/v2/"`
	AccessTokenTTL       time.Duration  `env:"ACCESS_TOKEN_TTL" envDefault:"24h"`
	IDTokenTTL           time.Duration  `env:"ID_TOKEN_TTL" envDefault:"24h"`
	LogLevel             string         `env:"LOG_LEVEL" envDefault:"info"`
	SpecValidationStrict bool           `env:"SPEC_VALIDATION_STRICT" envDefault:"true"`
	ReadHeaderTimeout    time.Duration  `env:"READ_HEADER_TIMEOUT" envDefault:"5s"`
	WriteTimeout         time.Duration  `env:"WRITE_TIMEOUT" envDefault:"30s"`
	IdleTimeout          time.Duration  `env:"IDLE_TIMEOUT" envDefault:"120s"`
	MaxRequestBodyBytes  int64          `env:"MAX_REQUEST_BODY_BYTES" envDefault:"1048576"` // 1 MiB.
	ShutdownTimeout      time.Duration  `env:"SHUTDOWN_TIMEOUT" envDefault:"5s"`

	// LogoutAllowedURLs is the comma-separated allow-list of absolute
	// returnTo URLs that /v2/logout will redirect to. Relative URLs are
	// always allowed (they can't escape the mock's origin). Mirrors
	// Auth0's "Allowed Logout URLs" tenant setting.
	LogoutAllowedURLs []string `env:"LOGOUT_ALLOWED_URLS" envSeparator:","`

	// AuthorizeAllowedCallbacks is the comma-separated allow-list of
	// absolute redirect_uri values that /authorize will 302 to. Same
	// threat model as LogoutAllowedURLs but on the higher-value endpoint:
	// /authorize carries `code` / `access_token` in the URL, so an
	// unvalidated redirect_uri leaks them to attacker-controlled hosts.
	// Mirrors Auth0's per-application "Allowed Callback URLs" setting.
	// Empty = no enforcement (the test-friendly default — clients can
	// register any callback).
	AuthorizeAllowedCallbacks []string `env:"AUTHORIZE_ALLOWED_CALLBACKS" envSeparator:","`

	// BearerRequireAudience opts the Mgmt-API bearer middleware into Auth0-
	// like strict audience binding. When non-empty, tokens whose `aud`
	// claim doesn't contain this value get a 401. Empty (default) keeps
	// the "echoed, not enforced" behaviour the README documents so test
	// suites can swap audiences freely.
	BearerRequireAudience string `env:"BEARER_REQUIRE_AUDIENCE"`
}

// Load populates a Specification from process environment and validates it.
// Errors out on impossible combinations like "both listeners disabled" so
// callers don't have to repeat the sanity checks (and so the process
// doesn't silently idle forever waiting for a signal that nothing's
// listening for).
func Load() (*Specification, error) {
	var spec Specification
	if err := env.Parse(&spec); err != nil {
		return nil, fmt.Errorf("env parse: %w", err)
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	return &spec, nil
}

// Validate checks the loaded Specification for mutually-exclusive or
// nonsensical combinations. Run automatically by Load(); exported so
// callers building a Specification programmatically (tests, embeds)
// get the same sanity net.
func (s *Specification) Validate() error {
	// Both listeners off means the orchestrator launches with zero
	// servers and idles forever. Surface the misconfiguration up-front
	// instead of leaving the operator guessing why nothing's listening.
	if listenerOff(s.HTTPAddress) && listenerOff(s.HTTPSAddress) {
		return fmt.Errorf("HTTP_ADDR and HTTPS_ADDR are both disabled — at least one must be a bind address (use \"off\" on only one to run single-protocol)")
	}
	return nil
}

// listenerOff returns true when an HTTP_ADDR / HTTPS_ADDR value means
// "do not bind." Matches the cmd/api/main.go check.
func listenerOff(addr string) bool {
	return addr == "" || addr == "off"
}
