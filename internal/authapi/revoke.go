package authapi

import (
	"net/http"

	"github.com/sergiught/auth0-mock/internal/session"
)

// RevokeHandler serves /oauth/revoke. With Strict it deletes the refresh-token
// chain of the posted token; otherwise it is a no-op. It always answers 200
// with an empty body, even for unknown tokens.
type RevokeHandler struct {
	Strict   bool
	Sessions *session.Store
}

func (h *RevokeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.Strict && h.Sessions != nil {
		// Reuse the /oauth/token parser: it reads a JSON or form body into
		// Params. A malformed body just means nothing to revoke.
		if req, err := parseTokenBody(r); err == nil {
			if token, _ := req.Params["token"].(string); token != "" {
				h.Sessions.RevokeRefreshChain(token)
			}
		}
	}
	w.WriteHeader(http.StatusOK)
}
