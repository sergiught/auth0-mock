// Package bearer provides middleware that validates an Authorization: Bearer
// JWT against a jwks.KeySet and attaches the claims to the request context.
package bearer

import (
	"context"
	"net/http"
	"strings"

	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/jwks"
)

type claimsKey struct{}

// ClaimsFromContext returns the verified token claims, or nil if absent.
func ClaimsFromContext(ctx context.Context) *jwks.Claims {
	if v, ok := ctx.Value(claimsKey{}).(*jwks.Claims); ok {
		return v
	}
	return nil
}

// Middleware enforces a verifiable bearer token on every wrapped handler.
func Middleware(ks *jwks.KeySet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, "Bearer ") {
				httperr.WriteMgmt(w, http.StatusUnauthorized, "Unauthorized",
					"missing bearer token", "missing_bearer")
				return
			}
			claims, err := ks.Verify(strings.TrimPrefix(h, "Bearer "))
			if err != nil {
				httperr.WriteMgmt(w, http.StatusUnauthorized, "Unauthorized",
					"invalid bearer token", "invalid_bearer")
				return
			}
			ctx := context.WithValue(r.Context(), claimsKey{}, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
