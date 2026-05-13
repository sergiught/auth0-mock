package authapi

import (
	"maps"
	"net/http"
	"strings"

	"github.com/go-chi/render"

	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/jwks"
)

// UserInfoHandler returns claims for the authenticated user.
type UserInfoHandler struct {
	Keys *jwks.KeySet
}

func (h *UserInfoHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hdr := r.Header.Get("Authorization")
	if !strings.HasPrefix(hdr, "Bearer ") {
		httperr.WriteAuth(w, http.StatusUnauthorized, "invalid_token", "missing bearer token")
		return
	}
	claims, err := h.Keys.Verify(strings.TrimPrefix(hdr, "Bearer "))
	if err != nil {
		httperr.WriteAuth(w, http.StatusUnauthorized, "invalid_token", err.Error())
		return
	}
	out := map[string]any{"sub": claims.Subject}
	maps.Copy(out, claims.Extra)
	render.JSON(w, r, out)
}
