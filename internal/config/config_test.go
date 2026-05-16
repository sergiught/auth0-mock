package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_Defaults(t *testing.T) {
	clearEnv(t)
	spec, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "development", spec.Environment)
	assert.Equal(t, "127.0.0.1:8080", spec.HTTPAddress)
	assert.Equal(t, "127.0.0.1:8443", spec.HTTPSAddress)
	assert.Equal(t, "https://localhost:8443/", spec.IssuerURL)
	assert.Equal(t, "https://localhost:8443/api/v2/", spec.DefaultAudience)
	assert.True(t, spec.SpecValidationStrict)
	assert.Equal(t, []string{"localhost", "127.0.0.1", "::1"}, spec.TLS.Hostnames)
}

func TestLoad_OverridesFromEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("HTTP_ADDR", ":9000")
	t.Setenv("SPEC_VALIDATION_STRICT", "false")
	t.Setenv("TLS_CACHE_DIR", "/tmp/tls")
	spec, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "production", spec.Environment)
	assert.Equal(t, ":9000", spec.HTTPAddress)
	assert.False(t, spec.SpecValidationStrict)
	assert.Equal(t, "/tmp/tls", spec.TLS.CacheDir)
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
