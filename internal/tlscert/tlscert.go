// Package tlscert produces a *tls.Config from either user-supplied cert/key
// files or an auto-generated self-signed cert covering configured hostnames.
//
// Three modes, checked in order:
//
//  1. If CertFile and KeyFile are both set, load them directly. The user owns
//     the cert's lifecycle.
//  2. Otherwise, if CacheDir is set, attempt to load `<CacheDir>/tls.crt` +
//     `<CacheDir>/tls.key`. If they don't exist, generate a fresh cert and
//     persist it there so subsequent boots reuse it.
//  3. Otherwise, generate a fresh cert in-memory for this process only.
package tlscert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Config selects how the TLS config is built.
//
// The env tags let this struct be populated directly by the env-parsing
// in config.Specification; consumers that don't care about env vars can
// ignore them.
type Config struct {
	CertFile  string   `env:"TLS_CERT_FILE"`                                      // If set together with KeyFile → load from disk.
	KeyFile   string   `env:"TLS_KEY_FILE"`                                       //
	CacheDir  string   `env:"TLS_CACHE_DIR"`                                      // If set, persist the auto-generated cert here and reuse on restart.
	Hostnames []string `env:"TLS_HOSTNAMES" envDefault:"localhost,127.0.0.1,::1"` // SANs for the auto-generated cert.
}

// Load returns a *tls.Config ready for an http.Server.
func Load(cfg Config) (*tls.Config, error) {
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load cert/key: %w", err)
		}
		return tlsConfig(cert), nil
	}

	if cfg.CacheDir != "" {
		if cert, ok, err := loadFromCache(cfg.CacheDir); err != nil {
			return nil, err
		} else if ok {
			return tlsConfig(cert), nil
		}
	}

	gen, err := generateSelfSigned(cfg.Hostnames)
	if err != nil {
		return nil, err
	}

	if cfg.CacheDir != "" {
		if err := persistToCache(cfg.CacheDir, gen); err != nil {
			return nil, err
		}
	}

	cert, err := tls.X509KeyPair(gen.certPEM, gen.keyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse generated cert: %w", err)
	}
	return tlsConfig(cert), nil
}

func tlsConfig(cert tls.Certificate) *tls.Config {
	// MinVersion is TLS 1.3 per RFC 9325 (2024) — TLS 1.2 is still widely
	// used but RFC guidance for new code is 1.3+. Every Go 1.20+ TLS client
	// the mock targets supports 1.3.
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}
}

// loadFromCache returns (cert, true, nil) on success, (zero, false, nil) when
// the cache files are missing, and (zero, false, err) for hard errors.
func loadFromCache(dir string) (tls.Certificate, bool, error) {
	certPath := filepath.Join(dir, "tls.crt")
	keyPath := filepath.Join(dir, "tls.key")
	if _, err := os.Stat(certPath); errors.Is(err, os.ErrNotExist) {
		return tls.Certificate{}, false, nil
	}
	if _, err := os.Stat(keyPath); errors.Is(err, os.ErrNotExist) {
		return tls.Certificate{}, false, nil
	}
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return tls.Certificate{}, false, fmt.Errorf("load cached cert/key: %w", err)
	}
	return cert, true, nil
}

func persistToCache(dir string, gen *generated) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create cache dir %s: %w", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tls.crt"), gen.certPEM, 0o644); err != nil {
		return fmt.Errorf("write cert: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tls.key"), gen.keyPEM, 0o600); err != nil {
		return fmt.Errorf("write key: %w", err)
	}
	return nil
}

type generated struct{ certPEM, keyPEM []byte }

func generateSelfSigned(hostnames []string) (*generated, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate serial: %w", err)
	}
	tpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "auth0-mock"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, h := range hostnames {
		if ip := net.ParseIP(h); ip != nil {
			tpl.IPAddresses = append(tpl.IPAddresses, ip)
		} else {
			tpl.DNSNames = append(tpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, &tpl, &tpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, fmt.Errorf("create cert: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("marshal key: %w", err)
	}
	return &generated{
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		keyPEM:  pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	}, nil
}
