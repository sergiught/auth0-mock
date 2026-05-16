package jwks

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

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
