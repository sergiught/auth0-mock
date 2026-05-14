package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFromEnv_Defaults(t *testing.T) {
	clearEnv(t)
	cfg, err := LoadFromEnv()
	require.NoError(t, err)
	assert.Equal(t, "development", cfg.Environment)
	assert.Equal(t, "127.0.0.1:8080", cfg.HTTPAddress)
	assert.Equal(t, "127.0.0.1:8443", cfg.HTTPSAddress)
	assert.Equal(t, "https://localhost:8443/", cfg.IssuerURL)
	assert.Equal(t, "https://localhost:8443/api/v2/", cfg.DefaultAudience)
	assert.True(t, cfg.SpecValidationStrict)
}

func TestLoadFromEnv_OverridesFromEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("HTTP_ADDR", ":9000")
	t.Setenv("SPEC_VALIDATION_STRICT", "false")
	cfg, err := LoadFromEnv()
	require.NoError(t, err)
	assert.Equal(t, "production", cfg.Environment)
	assert.Equal(t, ":9000", cfg.HTTPAddress)
	assert.False(t, cfg.SpecValidationStrict)
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"ENVIRONMENT", "HTTP_ADDR", "HTTPS_ADDR", "TLS_CERT_FILE", "TLS_KEY_FILE",
		"TLS_CACHE_DIR", "TLS_HOSTNAMES", "SIGNING_KEY_FILE", "ISSUER_URL",
		"DEFAULT_AUDIENCE", "ACCESS_TOKEN_TTL", "ID_TOKEN_TTL", "LOG_LEVEL",
		"SPEC_VALIDATION_STRICT", "READ_HEADER_TIMEOUT", "SHUTDOWN_TIMEOUT",
	} {
		_ = os.Unsetenv(k)
	}
}
