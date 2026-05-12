package jwks

import (
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is the parsed token payload exposed to callers.
type Claims struct {
	Issuer   string
	Subject  string
	Audience []string
	Scope    string
	Extra    map[string]any
}

// Verify parses and validates a JWT against this KeySet.
// Checks: signature, exp, and iss == ks.Issuer. Audience is NOT enforced.
func (k *KeySet) Verify(tokenStr string) (*Claims, error) {
	parsed, err := jwt.Parse(
		tokenStr,
		func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, fmt.Errorf("unexpected signing method %v", t.Method.Alg())
			}
			return &k.priv.PublicKey, nil
		},
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(k.cfg.Issuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	if !parsed.Valid {
		return nil, errors.New("token invalid")
	}
	mc, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("unexpected claims type")
	}

	out := &Claims{Extra: map[string]any{}}
	if v, ok := mc["iss"].(string); ok {
		out.Issuer = v
	}
	if v, ok := mc["sub"].(string); ok {
		out.Subject = v
	}
	switch a := mc["aud"].(type) {
	case string:
		out.Audience = []string{a}
	case []any:
		for _, x := range a {
			if s, ok := x.(string); ok {
				out.Audience = append(out.Audience, s)
			}
		}
	}
	if v, ok := mc["scope"].(string); ok {
		out.Scope = v
	}
	for kk, vv := range mc {
		switch kk {
		case "iss", "sub", "aud", "iat", "exp", "scope":
			continue
		}
		out.Extra[kk] = vv
	}
	return out, nil
}
