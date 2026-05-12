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
	assert.NotNil(t, ks.PrivateKey())
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
	assert.Equal(t, priv.N, ks.PrivateKey().N)
}
