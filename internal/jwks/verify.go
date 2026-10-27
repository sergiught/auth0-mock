package jwks

import (
	"errors"
	"fmt"
	"slices"

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

// VerifyOpts narrows what Verify will accept. RequireAudience, when
// non-empty, demands that the token's `aud` claim contains that exact
// value (matching Auth0's tenant-API-audience binding). Empty means
// no audience check — keeps the deliberate "echoed, not enforced"
// behavior the README describes for the /userinfo flow and tests.
//
// AllowExpired skips the exp, iat and nbf checks (and the exp-required
// check) so an expired token can still be identified, as OIDC
// RP-initiated logout permits for id_token_hint. Signature, algorithm,
// issuer and RequireAudience are still enforced.
type VerifyOpts struct {
	RequireAudience string
	AllowExpired    bool
}

// Verify parses and validates a JWT against this KeySet.
// Checks: signature, exp (required), iss == ks.Issuer, iat value (if
// present, with Config.Leeway clock skew), nbf (if present, with skew), and
// (when opts.RequireAudience is set) aud contains that value. With
// opts.AllowExpired the exp, iat and nbf checks are skipped; signature,
// alg, iss and aud are still checked.
//
// Note: jwt/v5's `WithIssuedAt()` validates the iat *value* against the
// clock — it does not make iat presence-required. The mock's own Mint
// always sets iat, so in practice this is the same thing, but adopters
// who plug in externally-minted tokens should know.
//
//nolint:gocyclo // Security-critical; explicit aud/iss/exp/iat/sig branches are easier to audit inline than as a helper chain.
func (k *KeySet) Verify(tokenStr string, opts VerifyOpts) (*Claims, error) {
	parserOpts := []jwt.ParserOption{jwt.WithValidMethods([]string{"RS256"})}
	if opts.AllowExpired {
		parserOpts = append(parserOpts, jwt.WithoutClaimsValidation())
	} else {
		parserOpts = append(parserOpts,
			jwt.WithIssuer(k.cfg.Issuer),
			jwt.WithExpirationRequired(),
			jwt.WithIssuedAt(),
			jwt.WithLeeway(k.cfg.Leeway),
			jwt.WithTimeFunc(k.cfg.Now),
		)
	}
	parsed, err := jwt.Parse(
		tokenStr,
		func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, fmt.Errorf("unexpected signing method %v", t.Method.Alg())
			}
			return &k.priv.PublicKey, nil
		},
		parserOpts...,
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

	if opts.AllowExpired {
		if got, _ := mc["iss"].(string); got != k.cfg.Issuer {
			return nil, fmt.Errorf("issuer %q does not match %q", got, k.cfg.Issuer)
		}
	}

	out := &Claims{}
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
		if out.Extra == nil {
			out.Extra = make(map[string]any, len(mc))
		}
		out.Extra[kk] = vv
	}
	return out, nil
}
