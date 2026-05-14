// Package config loads runtime settings from environment variables.
package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config holds all runtime settings.
type Config struct {
	Environment          string        `env:"ENVIRONMENT" envDefault:"development"`
	HTTPAddress          string        `env:"HTTP_ADDR" envDefault:"127.0.0.1:8080"`
	HTTPSAddress         string        `env:"HTTPS_ADDR" envDefault:"127.0.0.1:8443"`
	TLSCertFile          string        `env:"TLS_CERT_FILE"`
	TLSKeyFile           string        `env:"TLS_KEY_FILE"`
	TLSCacheDir          string        `env:"TLS_CACHE_DIR"`
	TLSHostnames         []string      `env:"TLS_HOSTNAMES" envDefault:"localhost,127.0.0.1,::1"`
	SigningKeyFile       string        `env:"SIGNING_KEY_FILE"`
	IssuerURL            string        `env:"ISSUER_URL" envDefault:"https://localhost:8443/"`
	DefaultAudience      string        `env:"DEFAULT_AUDIENCE" envDefault:"https://localhost:8443/api/v2/"`
	AccessTokenTTL       time.Duration `env:"ACCESS_TOKEN_TTL" envDefault:"24h"`
	IDTokenTTL           time.Duration `env:"ID_TOKEN_TTL" envDefault:"24h"`
	LogLevel             string        `env:"LOG_LEVEL" envDefault:"info"`
	SpecValidationStrict bool          `env:"SPEC_VALIDATION_STRICT" envDefault:"true"`
	ReadHeaderTimeout    time.Duration `env:"READ_HEADER_TIMEOUT" envDefault:"5s"`
	ShutdownTimeout      time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"5s"`
}

// LoadFromEnv populates a Config from process environment.
func LoadFromEnv() (*Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("env parse: %w", err)
	}
	return &cfg, nil
}
