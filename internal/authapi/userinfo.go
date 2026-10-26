package authapi

import (
	"maps"
	"net/http"

	"github.com/go-chi/render"

	"github.com/sergiught/auth0-mock/internal/bearer"
	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/session"
)

// UserInfoHandler returns claims for the authenticated user.
type UserInfoHandler struct {
	Keys *jwks.KeySet
	// Sessions may be nil. When set, the profile remembered for the token's
	// subject is added beneath the token's own claims.
	Sessions *session.Store
}

func (h *UserInfoHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hdr := r.Header.Get("Authorization")
	token, ok := bearer.Token(hdr)
	if !ok {
		httperr.WriteAuth(w, http.StatusUnauthorized, "invalid_token", "missing bearer token")
		return
	}
	// /userinfo accepts any audience (it's about who the user is, not
	// what API they're authorised to call), so no RequireAudience here.
	// Matches real Auth0's /userinfo behaviour.
	claims, err := h.Keys.Verify(token, jwks.VerifyOpts{})
	if err != nil {
		httperr.WriteAuth(w, http.StatusUnauthorized, "invalid_token", err.Error())
		return
	}
	out := make(map[string]any, len(claims.Extra)+1)
	out["sub"] = claims.Subject
	// Machine tokens have no user, even when "<client_id>@clients" happens
	// to match a remembered subject.
	if h.Sessions != nil && claims.Extra["gty"] != "client-credentials" {
		if profile, ok := h.Sessions.Profile(claims.Subject); ok {
			maps.Copy(out, profile)
		}
	}
	maps.Copy(out, claims.Extra)
	render.JSON(w, r, out)
}
