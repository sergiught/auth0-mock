package jwks

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// verifyLeeway is the time-skew leeway applied to exp / nbf / iat checks.
// One minute matches what the major OIDC providers (Auth0, Okta, Google)
// allow and gives clock-skewed CI runners breathing room without
// meaningfully widening the replay window.
const verifyLeeway = 60 * time.Second

// Claims is the parsed token payload exposed to callers.
type Claims struct {
	Issuer   string
	Subject  string
	Audience []string
	Scope    string
	Extra    map[string]any
}

// VerifyOpts narrows what Verify will accept. RequireAudience, when
// non-empty, demands that the token's `aud` claim contains that exact
// value (matching Auth0's tenant-API-audience binding). Empty means
// no audience check — keeps the deliberate "echoed, not enforced"
// behavior the README describes for the /userinfo flow and tests.
type VerifyOpts struct {
	RequireAudience string
}

// Verify parses and validates a JWT against this KeySet.
// Checks: signature, exp, iss == ks.Issuer, iat present, ±60s clock skew,
// and (when opts.RequireAudience is set) aud contains that value.
func (k *KeySet) Verify(tokenStr string, opts VerifyOpts) (*Claims, error) {
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
		jwt.WithIssuedAt(),
		jwt.WithLeeway(verifyLeeway),
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
	if opts.RequireAudience != "" && !slices.Contains(out.Audience, opts.RequireAudience) {
		return nil, fmt.Errorf("audience %q not in token aud %v", opts.RequireAudience, out.Audience)
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
