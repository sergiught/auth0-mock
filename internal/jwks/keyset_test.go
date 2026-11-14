package jwks

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewKeySet_GeneratesNewKey(t *testing.T) {
	ks, err := NewKeySet(Config{Issuer: "https://test/"})
	require.NoError(t, err)
	require.NotNil(t, ks)
	assert.NotEmpty(t, ks.KeyID())
	assert.NotNil(t, ks.priv)
	assert.NotNil(t, ks.PublicKey())
}

func TestNewKeySet_LoadsExistingKey(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der := x509.MarshalPKCS1PrivateKey(priv)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})

	dir := t.TempDir()
	path := filepath.Join(dir, "signing.key")
	require.NoError(t, os.WriteFile(path, pemBytes, 0o600))

	ks, err := NewKeySet(Config{Issuer: "https://test/", KeyFile: path})
	require.NoError(t, err)
	assert.Equal(t, priv.N, ks.priv.N)
}

func TestNewKeySet_FirstRunPersistsToMissingPath(t *testing.T) {
	// First-run semantic: SIGNING_KEY_FILE is set but the file doesn't
	// exist yet (typical for `make watch` on a fresh checkout). We must
	// generate AND persist so the next boot loads the same key.
	dir := t.TempDir()
	path := filepath.Join(dir, "signing.key")

	ks1, err := NewKeySet(Config{Issuer: "https://test/", KeyFile: path})
	require.NoError(t, err)
	require.NotNil(t, ks1.priv)

	// File now exists with restrictive perms.
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "persisted key must be 0600")

	// Second boot reads the same key.
	ks2, err := NewKeySet(Config{Issuer: "https://test/", KeyFile: path})
	require.NoError(t, err)
	assert.Equal(t, ks1.priv.N, ks2.priv.N, "second boot must reuse the persisted key")
	assert.Equal(t, ks1.KeyID(), ks2.KeyID(), "kid must be stable across reboots")
}

func TestNewKeySet_FailsOnUnreadablePath(t *testing.T) {
	// A path inside a non-existent directory is a real I/O error (not
	// "file missing, please generate") — surface that distinction so a
	// fat-fingered SIGNING_KEY_FILE fails loud instead of silently
	// generating a key in the wrong place.
	_, err := NewKeySet(Config{
		Issuer:  "https://test/",
		KeyFile: "/this/directory/definitely/does/not/exist/signing.key",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "persist signing key")
}

func TestResolveIssuer(t *testing.T) {
	tlsReq := httptest.NewRequest("GET", "http://LocalHost:32768/x", nil)
	tlsReq.TLS = &tls.ConnectionState{}
	tests := []struct {
		name       string
		configured string
		req        *http.Request
		want       string
	}{
		{"fixed", "https://localhost:8443/", httptest.NewRequest("GET", "http://other:1/", nil), "https://localhost:8443/"},
		{"auto http lowercases host", AutoIssuer, httptest.NewRequest("GET", "http://LocalHost:32768/x", nil), "http://localhost:32768/"},
		{"auto tls", AutoIssuer, tlsReq, "https://localhost:32768/"},
		{"auto no port", AutoIssuer, httptest.NewRequest("GET", "http://auth0-mock/", nil), "http://auth0-mock/"},
		{"capitalised Auto passes through", "Auto", httptest.NewRequest("GET", "http://x/", nil), "Auto"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ResolveIssuer(tc.configured, tc.req))
		})
	}
}

func TestResolveIssuer_PathFromContext(t *testing.T) {
	r := httptest.NewRequest("GET", "/x", nil)
	r = r.WithContext(WithIssuerPath(r.Context(), "ns/abc/"))
	assert.Equal(t, "https://mock/ns/abc/", ResolveIssuer("https://mock/", r))

	r = httptest.NewRequest("GET", "http://localhost:8080/x", nil)
	r = r.WithContext(WithIssuerPath(r.Context(), "ns/abc/"))
	assert.Equal(t, "http://localhost:8080/ns/abc/", ResolveIssuer(AutoIssuer, r))

	plain := httptest.NewRequest("GET", "/x", nil)
	assert.Equal(t, "https://mock/", ResolveIssuer("https://mock/", plain))
}

func TestKeySet_WithNow(t *testing.T) {
	ks, err := NewKeySet(Config{Issuer: "https://mock/", AccessTokenTTL: time.Hour})
	require.NoError(t, err)
	fixed := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	view := ks.WithNow(func() time.Time { return fixed })

	tok, err := view.Mint(MintOpts{Subject: "u", Audience: []string{"a"}, TTL: time.Hour})
	require.NoError(t, err)
	parsed := jwt.MapClaims{}
	_, _, err = jwt.NewParser().ParseUnverified(tok, parsed)
	require.NoError(t, err)
	assert.EqualValues(t, fixed.Unix(), parsed["iat"])

	assert.NotEqual(t, fixed.Unix(), ks.Cfg().Now().Unix())
	assert.Equal(t, ks.JWKSJSON(), view.JWKSJSON())
	_, err = ks.Verify(tok, VerifyOpts{AllowExpired: true})
	require.NoError(t, err)
}
