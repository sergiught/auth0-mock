package authapi

import (
	"net/http"
	"net/url"
	"slices"

	"github.com/sergiught/auth0-mock/internal/httperr"
)

// LogoutHandler 302-redirects to the returnTo query parameter (or "/" when
// missing). Absolute (scheme+host) returnTo values must appear verbatim in
// AllowedReturnURLs; relative URLs are always permitted because they can't
// jump origins.
//
// This mirrors Auth0's "Allowed Logout URLs" tenant setting. Skipping
// validation would make /v2/logout an open redirect — an unauthenticated
// attacker could send victims a `…/v2/logout?returnTo=https://evil.tld`
// link that lands them on attacker-controlled content under the mock's
// origin.
type LogoutHandler struct {
	AllowedReturnURLs []string
}

func (h *LogoutHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ret := r.URL.Query().Get("returnTo")
	if ret == "" {
		ret = "/"
	}
	if !h.isAllowed(ret) {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request",
			"returnTo is not in the configured allow-list (LOGOUT_ALLOWED_URLS); add it there to permit this redirect")
		return
	}
	w.Header().Set("Location", ret)
	w.WriteHeader(http.StatusFound)
}

func (h *LogoutHandler) isAllowed(returnTo string) bool {
	u, err := url.Parse(returnTo)
	if err != nil {
		return false
	}
	// Relative URLs (no host component) can't redirect cross-origin and so
	// are always safe.
	if u.Host == "" {
		return true
	}
	return slices.Contains(h.AllowedReturnURLs, returnTo)
}
