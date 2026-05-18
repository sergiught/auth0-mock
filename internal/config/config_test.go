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
	assert.Equal(t, "127.0.0.1:8080", spec.HTTPAddress)
	assert.Equal(t, "127.0.0.1:8443", spec.HTTPSAddress)
	assert.Equal(t, "https://localhost:8443/", spec.IssuerURL)
	assert.Equal(t, "https://localhost:8443/api/v2/", spec.DefaultAudience)
	assert.True(t, spec.SpecValidationStrict)
	assert.Equal(t, []string{"localhost", "127.0.0.1", "::1"}, spec.TLS.Hostnames)
}

func TestLoad_OverridesFromEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("HTTP_ADDR", ":9000")
	t.Setenv("SPEC_VALIDATION_STRICT", "false")
	t.Setenv("TLS_CACHE_DIR", "/tmp/tls")
	spec, err := Load()
	require.NoError(t, err)
	assert.Equal(t, ":9000", spec.HTTPAddress)
	assert.False(t, spec.SpecValidationStrict)
	assert.Equal(t, "/tmp/tls", spec.TLS.CacheDir)
}

func TestLoad_RejectsBothListenersOff(t *testing.T) {
	clearEnv(t)
	t.Setenv("HTTP_ADDR", "off")
	t.Setenv("HTTPS_ADDR", "off")
	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP_ADDR and HTTPS_ADDR are both disabled")
}

func TestValidate_RejectsBothListenersEmptyProgrammatically(t *testing.T) {
	// Load() can't reach this case because caarlos0/env replaces an empty
	// string with envDefault — but programmatically-constructed specs
	// (tests, embeds) can, so Validate still guards the shape.
	spec := &Specification{HTTPAddress: "", HTTPSAddress: ""}
	err := spec.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "both disabled")
}

func TestValidate_OneListenerOffIsFine(t *testing.T) {
	// HTTPS-only.
	spec := &Specification{HTTPAddress: "off", HTTPSAddress: "127.0.0.1:8443"}
	require.NoError(t, spec.Validate())
	// HTTP-only.
	spec = &Specification{HTTPAddress: "127.0.0.1:8080", HTTPSAddress: "off"}
	require.NoError(t, spec.Validate())
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"HTTP_ADDR", "HTTPS_ADDR", "TLS_CERT_FILE", "TLS_KEY_FILE",
		"TLS_CACHE_DIR", "TLS_HOSTNAMES", "SIGNING_KEY_FILE", "ISSUER_URL",
		"DEFAULT_AUDIENCE", "ACCESS_TOKEN_TTL", "ID_TOKEN_TTL", "LOG_LEVEL",
		"SPEC_VALIDATION_STRICT", "READ_HEADER_TIMEOUT", "SHUTDOWN_TIMEOUT",
		"EVENTS_REPLAY_BUFFER",
	} {
		_ = os.Unsetenv(k)
	}
}

func TestLoad_EventsReplayBuffer_DefaultsTo100(t *testing.T) {
	clearEnv(t)
	spec, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 100, spec.EventsReplayBuffer)
}

func TestLoad_EventsReplayBuffer_HonoursOverride(t *testing.T) {
	clearEnv(t)
	t.Setenv("EVENTS_REPLAY_BUFFER", "500")
	spec, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 500, spec.EventsReplayBuffer)
}
