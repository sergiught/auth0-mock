package authapi

import (
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/sergiught/auth0-mock/internal/httperr"
)

// LogoutHandler 302-redirects to the returnTo query parameter (or "/" when
// missing). When AllowedReturnURLs is empty (the default) every returnTo is
// permitted — this is a CI/local-testing mock, so the permissive default
// matches the same opt-in pattern as AuthorizeHandler and means SDK tests
// calling `/v2/logout?returnTo=https://app/…` work out of the box. When
// AllowedReturnURLs is set (via LOGOUT_ALLOWED_URLS) the handler enforces
// the allow-list like real Auth0 does, with the URL-scheme / backslash
// bypass guards described in isAllowed.
//
// Never expose the mock to untrusted networks — see the README disclaimer.
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

// isAllowed returns true when no allow-list is configured (permissive
// default for the CI/local-testing use case) or when the returnTo passes
// every check on the opted-in path: backslash-free, no dangerous scheme,
// and either truly relative or an exact match against AllowedReturnURLs.
func (h *LogoutHandler) isAllowed(returnTo string) bool {
	// Empty list = permissive default. Adopters opt into the allow-list
	// by setting LOGOUT_ALLOWED_URLS.
	if len(h.AllowedReturnURLs) == 0 {
		return true
	}
	// Backslash defence: browsers normalise '\' → '/' before following
	// Location, so "/\\evil.tld" resolves to "//evil.tld" — same open-
	// redirect class as a bare //evil.tld would be — but url.Parse keeps
	// the backslashes in u.Path so the host check below can't catch
	// them; reject up-front instead.
	if strings.ContainsAny(returnTo, "\\") {
		return false
	}
	u, err := url.Parse(returnTo)
	if err != nil {
		return false
	}
	// Truly relative URLs (no scheme AND no host) can't escape this
	// origin. Requiring "no scheme" closes the URL-scheme bypass class:
	// `javascript:`, `data:`, `mailto:` and custom-app schemes all
	// parse with an empty host but a non-empty scheme and would
	// otherwise sneak past the allow-list.
	if u.Scheme == "" && u.Host == "" {
		return true
	}
	return slices.Contains(h.AllowedReturnURLs, returnTo)
}
