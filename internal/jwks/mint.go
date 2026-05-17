package jwks

import (
	"fmt"
	"maps"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// MintOpts controls a single token mint.
type MintOpts struct {
	Subject  string
	Audience []string
	Scope    string
	TTL      time.Duration
	Extra    map[string]any // Additional claims (e.g. gty, azp, name).
}

// Mint issues a signed RS256 JWT.
func (k *KeySet) Mint(opts MintOpts) (string, error) {
	now := k.cfg.Now()
	claims := jwt.MapClaims{
		"iss": k.cfg.Issuer,
		"sub": opts.Subject,
		"aud": opts.Audience,
		"iat": now.Unix(),
		"exp": now.Add(opts.TTL).Unix(),
	}
	if opts.Scope != "" {
		claims["scope"] = opts.Scope
	}
	maps.Copy(claims, opts.Extra)
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = k.keyID
	signed, err := tok.SignedString(k.priv)
	if err != nil {
		return "", fmt.Errorf("sign: %w", err)
	}
	return signed, nil
}
