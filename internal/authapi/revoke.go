package authapi

import "net/http"

// RevokeHandler is a no-op mock; refresh tokens aren't tracked.
type RevokeHandler struct{}

func (h *RevokeHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}
