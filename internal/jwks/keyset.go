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
	Issuer string // The iss claim and OIDC discovery base.
	// KeyFile is the on-disk path for the RS256 signing key (PEM-encoded).
	// Behaviour:
	//   - empty           → generate a fresh ephemeral key on every boot
	//                       (tokens stop verifying across restarts).
	//   - path exists     → load the existing key.
	//   - path missing    → generate a fresh key, persist it to that
	//                       path (0600), use it. Subsequent boots read
	//                       from disk so previously-minted tokens stay
	//                       verifiable across `make watch` hot reloads.
	KeyFile        string
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
	pemBytes, err := os.ReadFile(path) //nolint:gosec // path is the SIGNING_KEY_FILE env var supplied by the operator; intentional read of a configured file.
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read signing key %s: %w", path, err)
		}
		// First-run path: SIGNING_KEY_FILE is set but the file isn't
		// there yet. Generate a key and persist it so subsequent boots
		// (in particular `make watch` hot reloads) reuse the same key
		// and previously-minted tokens stay verifiable.
		return generateAndPersist(path)
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

// generateAndPersist mints a fresh 2048-bit RSA key, writes it to path
// as PKCS#1 PEM with 0600 perms, and returns it. Used on the first-run
// path when SIGNING_KEY_FILE is set but the file doesn't exist yet.
func generateAndPersist(path string) (*rsa.PrivateKey, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate signing key: %w", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(priv),
	})
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil { //nolint:gosec // path is the SIGNING_KEY_FILE env var supplied by the operator; intentional write to a configured file.
		return nil, fmt.Errorf("persist signing key to %s: %w", path, err)
	}
	return priv, nil
}

// keyIDFromKey produces a stable kid by hashing the public modulus.
func keyIDFromKey(priv *rsa.PrivateKey) string {
	sum := sha256.Sum256(priv.N.Bytes())
	return base64.RawURLEncoding.EncodeToString(sum[:8])
}
