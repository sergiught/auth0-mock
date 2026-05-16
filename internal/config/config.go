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
	Environment          string         `env:"ENVIRONMENT" envDefault:"development"`
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
	MaxRequestBodyBytes  int64          `env:"MAX_REQUEST_BODY_BYTES" envDefault:"1048576"` // 1 MiB
	ShutdownTimeout      time.Duration  `env:"SHUTDOWN_TIMEOUT" envDefault:"5s"`

	// LogoutAllowedURLs is the comma-separated allow-list of absolute
	// returnTo URLs that /v2/logout will redirect to. Relative URLs are
	// always allowed (they can't escape the mock's origin). Mirrors
	// Auth0's "Allowed Logout URLs" tenant setting.
	LogoutAllowedURLs []string `env:"LOGOUT_ALLOWED_URLS" envSeparator:","`
}

// Load populates a Specification from process environment.
func Load() (*Specification, error) {
	var spec Specification
	if err := env.Parse(&spec); err != nil {
		return nil, fmt.Errorf("env parse: %w", err)
	}
	return &spec, nil
}
