// Package jwks owns the in-process RS256 signing key, JWT minting,
// JWT verification, and JWKS publication.
package jwks

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
	"time"
)

// Config controls KeySet construction.
type Config struct {
	Issuer         string        // The iss claim and OIDC discovery base.
	KeyFile        string        // Optional: PEM-encoded RSA private key.
	AccessTokenTTL time.Duration // Default TTL for mint.
	IDTokenTTL     time.Duration
}

// KeySet owns the active RS256 signing key.
type KeySet struct {
	cfg   Config
	priv  *rsa.PrivateKey
	keyID string
}

// NewKeySet either loads the key from cfg.KeyFile or generates a fresh one.
func NewKeySet(cfg Config) (*KeySet, error) {
	priv, err := loadOrGenerate(cfg.KeyFile)
	if err != nil {
		return nil, err
	}
	return &KeySet{
		cfg:   cfg,
		priv:  priv,
		keyID: keyIDFromKey(priv),
	}, nil
}

// PublicKey exposes the active RSA public key.
func (k *KeySet) PublicKey() *rsa.PublicKey { return &k.priv.PublicKey }

// KeyID returns the kid published in JWKS and added to JWT headers.
func (k *KeySet) KeyID() string { return k.keyID }

// Issuer returns the configured iss value.
func (k *KeySet) Issuer() string { return k.cfg.Issuer }

// Cfg exposes the underlying config (useful for token TTLs).
func (k *KeySet) Cfg() Config { return k.cfg }

func loadOrGenerate(path string) (*rsa.PrivateKey, error) {
	if path == "" {
		return rsa.GenerateKey(rand.Reader, 2048)
	}
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read signing key: %w", err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("signing key %s: not PEM-encoded", path)
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse PKCS8: %w", err)
		}
		rk, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("signing key %s: not an RSA key", path)
		}
		return rk, nil
	default:
		return nil, fmt.Errorf("signing key %s: unsupported PEM type %q", path, block.Type)
	}
}

// keyIDFromKey produces a stable kid by hashing the public modulus.
func keyIDFromKey(priv *rsa.PrivateKey) string {
	sum := sha256.Sum256(priv.N.Bytes())
	return base64.RawURLEncoding.EncodeToString(sum[:8])
}
