package bearer

import (
	"net/http"
	"slices"
	"strings"

	"github.com/sergiught/auth0-mock/internal/httperr"
)

// RequireScopes rejects a request whose verified token lacks every one of
// required (Auth0's "expected any of" rule) with Auth0's 403
// insufficient_scope error. It must run inside Middleware, which puts the
// claims in the context. With no required scopes it is a pass-through.
func RequireScopes(required []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if len(required) == 0 {
			return next
		}
		msg := "Insufficient scope, expected any of: " + strings.Join(required, ",")
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var granted []string
			if c := ClaimsFromContext(r.Context()); c != nil {
				granted = strings.Fields(c.Scope)
			}
			for _, s := range required {
				if slices.Contains(granted, s) {
					next.ServeHTTP(w, r)
					return
				}
			}
			httperr.WriteMgmt(w, http.StatusForbidden, "Forbidden", msg, "insufficient_scope")
		})
	}
}
