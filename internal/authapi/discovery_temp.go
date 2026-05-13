package authapi

import "net/http"

// discovery is replaced in M3.4 with the real OIDC discovery handler.
func discovery(_ Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
	}
}
