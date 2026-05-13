package authapi

import "net/http"

// revoke is a no-op mock; refresh tokens aren't tracked.
func revoke(_ Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}
}
