package authapi

import (
	"net/http"
	"net/url"
	"slices"

	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/session"
)

const loggedOutPage = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>You have been logged out.</title></head>
<body><p>You have been logged out.</p></body>
</html>
`

// OIDCLogoutHandler mirrors Auth0's /oidc/logout (OIDC RP-Initiated Logout).
// It reads id_token_hint, client_id, post_logout_redirect_uri and state from
// the GET query or POST form body. With no post_logout_redirect_uri it renders
// a static logged-out page; otherwise it 302-redirects there, appending state.
// When AllowedReturnURLs is empty (the default) every post_logout_redirect_uri
// is permitted — the same permissive default as LogoutHandler, so SDK tests
// work out of the box. When set (via LOGOUT_ALLOWED_URLS) the allow-list is
// enforced with the guards described in isSafeRedirect.
//
// An id_token_hint must be an id_token minted by this mock; it may be
// expired, as the spec allows.
//
// Never expose the mock to untrusted networks — see the README disclaimer.
type OIDCLogoutHandler struct {
	Keys              *jwks.KeySet
	AllowedReturnURLs []string
	// Sessions may be nil; the login cookie is then cleared without ending a session.
	Sessions *session.Store
}

func (h *OIDCLogoutHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		badLogout(w, "invalid form body: "+err.Error())
		return
	}
	hint := r.FormValue("id_token_hint")
	clientID := r.FormValue("client_id")
	target := r.FormValue("post_logout_redirect_uri")
	state := r.FormValue("state")

	if hint != "" {
		claims, err := h.Keys.Verify(hint, jwks.VerifyOpts{AllowExpired: true})
		if err != nil {
			badLogout(w, "id_token_hint is not a valid id_token issued by this mock: "+err.Error())
			return
		}
		if _, isAccessToken := claims.Extra["gty"]; isAccessToken {
			badLogout(w, "id_token_hint is an access token; send the id_token instead")
			return
		}
		if clientID != "" && !slices.Contains(claims.Audience, clientID) {
			badLogout(w, "id_token_hint was issued to a different client than client_id")
			return
		}
	}

	if target == "" {
		endLogin(w, r, h.Sessions)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(loggedOutPage))
		return
	}
	if clientID == "" && hint == "" {
		badLogout(w, "post_logout_redirect_uri requires client_id or id_token_hint")
		return
	}
	if len(h.AllowedReturnURLs) > 0 && !isSafeRedirect(target, h.AllowedReturnURLs) {
		badLogout(w, "post_logout_redirect_uri is not in the configured allow-list (LOGOUT_ALLOWED_URLS); add it there to permit this redirect")
		return
	}
	u, err := url.Parse(target)
	if err != nil {
		badLogout(w, "post_logout_redirect_uri is not a valid URL")
		return
	}
	if state != "" {
		// Append rather than re-encode, so the caller's query survives verbatim.
		if u.RawQuery == "" {
			u.RawQuery = "state=" + url.QueryEscape(state)
		} else {
			u.RawQuery += "&state=" + url.QueryEscape(state)
		}
	}
	endLogin(w, r, h.Sessions)
	w.Header().Set("Location", u.String())
	w.WriteHeader(http.StatusFound)
}

func badLogout(w http.ResponseWriter, description string) {
	httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", description)
}
