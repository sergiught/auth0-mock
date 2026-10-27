package config

import (
	"os"
	"testing"
	"time"

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
	assert.Empty(t, spec.CORSAllowedOrigins)
}

func TestLoad_OverridesFromEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("HTTP_ADDR", ":9000")
	t.Setenv("ADMIN_ALLOW_REMOTE", "true")
	t.Setenv("SPEC_VALIDATION_STRICT", "false")
	t.Setenv("TLS_CACHE_DIR", "/tmp/tls")
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://localhost:3000,https://*.example.com")
	spec, err := Load()
	require.NoError(t, err)
	assert.Equal(t, ":9000", spec.HTTPAddress)
	assert.True(t, spec.AdminAllowRemote)
	assert.False(t, spec.SpecValidationStrict)
	assert.Equal(t, "/tmp/tls", spec.TLS.CacheDir)
	assert.Equal(t, []string{"http://localhost:3000", "https://*.example.com"}, spec.CORSAllowedOrigins)
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
		"DEFAULT_AUDIENCE", "ACCESS_TOKEN_TTL", "ID_TOKEN_TTL", "TOKEN_LEEWAY", "ADMIN_ALLOW_REMOTE", "LOG_LEVEL",
		"SPEC_VALIDATION_STRICT", "READ_HEADER_TIMEOUT", "SHUTDOWN_TIMEOUT",
		"EVENTS_REPLAY_BUFFER", "EVENTS_RECONNECT_HINT", "REQUEST_JOURNAL_SIZE",
		"CORS_ALLOWED_ORIGINS",
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

func TestLoad_EventsReconnectHint_DefaultsTo3s(t *testing.T) {
	clearEnv(t)
	spec, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 3*time.Second, spec.EventsReconnectHint)
}

func TestLoad_EventsReconnectHint_HonoursOverride(t *testing.T) {
	clearEnv(t)
	t.Setenv("EVENTS_RECONNECT_HINT", "10s")
	spec, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 10*time.Second, spec.EventsReconnectHint)
}

func TestLoad_RequestJournalSize_DefaultsTo1000(t *testing.T) {
	clearEnv(t)
	spec, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 1000, spec.RequestJournalSize)
}

func TestLoad_RequestJournalSize_HonoursOverride(t *testing.T) {
	clearEnv(t)
	t.Setenv("REQUEST_JOURNAL_SIZE", "25")
	spec, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 25, spec.RequestJournalSize)
}

func TestLoad_BlankDefaultAudience(t *testing.T) {
	clearEnv(t)
	t.Setenv("DEFAULT_AUDIENCE", "")
	spec, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "", spec.DefaultAudience)

	t.Setenv("DEFAULT_AUDIENCE", "  ")
	spec, err = Load()
	require.NoError(t, err)
	assert.Equal(t, "", spec.DefaultAudience)

	require.NoError(t, os.Unsetenv("DEFAULT_AUDIENCE"))
	spec, err = Load()
	require.NoError(t, err)
	assert.Equal(t, "https://localhost:8443/api/v2/", spec.DefaultAudience)
}

func TestLoad_TokenLeeway_DefaultsToZero(t *testing.T) {
	clearEnv(t)
	spec, err := Load()
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), spec.TokenLeeway)
}

func TestLoad_TokenLeeway_HonoursOverride(t *testing.T) {
	clearEnv(t)
	t.Setenv("TOKEN_LEEWAY", "60s")
	spec, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 60*time.Second, spec.TokenLeeway)
}

func TestValidate_RejectsNegativeTokenLeeway(t *testing.T) {
	err := (&Specification{HTTPAddress: "127.0.0.1:8080", HTTPSAddress: "off", TokenLeeway: -time.Second}).Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TOKEN_LEEWAY must not be negative")
}

func TestIsRemoteAddr(t *testing.T) {
	tests := map[string]bool{
		":8080":                   true,
		"0.0.0.0:8080":            true,
		"[::]:8443":               true,
		"192.168.1.5:8080":        true,
		"myhost:8080":             true,
		"example.com:443":         true,
		"127.0.0.1:8080":          false,
		"127.0.0.2:8080":          false,
		"[::1]:8080":              false,
		"localhost:8080":          false,
		"LOCALHOST:8080":          false,
		"not-an-address":          false,
		"[::1%lo0]:8080":          false,
		"[fe80::1%eth0]:8080":     true,
		"[::ffff:127.0.0.1]:8080": false,
	}
	for addr, want := range tests {
		t.Run(addr, func(t *testing.T) {
			assert.Equal(t, want, isRemoteAddr(addr))
		})
	}
}

func TestRemoteListeners(t *testing.T) {
	spec := &Specification{HTTPAddress: "0.0.0.0:8080", HTTPSAddress: "off"}
	assert.Equal(t, []string{"HTTP_ADDR=0.0.0.0:8080"}, spec.RemoteListeners())

	spec = &Specification{HTTPAddress: "0.0.0.0:8080", HTTPSAddress: "0.0.0.0:8443"}
	assert.Equal(t, []string{"HTTP_ADDR=0.0.0.0:8080", "HTTPS_ADDR=0.0.0.0:8443"}, spec.RemoteListeners())

	spec = &Specification{HTTPAddress: "127.0.0.1:8080", HTTPSAddress: "[::1]:8443"}
	assert.Empty(t, spec.RemoteListeners())
}

func TestValidate_RejectsRemoteListenerWithoutFlag(t *testing.T) {
	err := (&Specification{HTTPAddress: "0.0.0.0:8080", HTTPSAddress: "0.0.0.0:8443"}).Validate()
	assert.EqualError(t, err, "HTTP_ADDR=0.0.0.0:8080 and HTTPS_ADDR=0.0.0.0:8443 can be reached from other machines, but /admin0 is unauthenticated and can mint tokens; bind to 127.0.0.1, or set ADMIN_ALLOW_REMOTE=true to accept the risk (Docker needs this)")
}

func TestValidate_RemoteListenerAllowedWithFlag(t *testing.T) {
	spec := &Specification{HTTPAddress: "0.0.0.0:8080", HTTPSAddress: "0.0.0.0:8443", AdminAllowRemote: true}
	assert.NoError(t, spec.Validate())
}

func TestValidate_OneRemoteListenerStillRejected(t *testing.T) {
	err := (&Specification{HTTPAddress: "off", HTTPSAddress: ":8443"}).Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTPS_ADDR=:8443")
}

func TestLoad_AdminAllowRemote_DefaultsToFalse(t *testing.T) {
	clearEnv(t)
	spec, err := Load()
	require.NoError(t, err)
	assert.False(t, spec.AdminAllowRemote)
}
