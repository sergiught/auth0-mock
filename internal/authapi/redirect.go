package authapi

import (
	"net/url"
	"slices"
	"strings"
)

// isSafeRedirect is the shared opt-in allow-list check used by both
// LogoutHandler.isAllowed and AuthorizeHandler.isRedirectAllowed. Returns
// true when `raw` is either a truly-relative URL (which can't escape the
// mock's origin) or appears verbatim in `allowList`. Rejects every variant
// of the open-redirect bypass class that browsers + parsers disagree on:
//
//   - `javascript:`, `data:`, `mailto:`, `file:`, custom-app schemes
//     (any non-empty scheme on an empty-host URL).
//   - `\\evil.tld`, `/\evil.tld`, anything with a backslash — browsers
//     normalise `\` → `/` before following Location.
//   - `//evil.tld`, `///evil.tld`, `////evil.tld` — url.Parse may return
//     empty host on three-or-more leading slashes, but browsers collapse
//     them to a protocol-relative cross-origin redirect.
//   - ` //evil.tld`, `\t//evil.tld` — leading whitespace passes net/url
//     but browsers trim it before following.
//
// Empty allow-lists are handled by the callers; this helper assumes the
// caller has already decided to enforce.
func isSafeRedirect(raw string, allowList []string) bool {
	// Reject any backslash — browsers normalise `\` → `/` before following.
	if strings.ContainsAny(raw, "\\") {
		return false
	}
	// Reject leading whitespace — browsers trim WHATWG URL §C0 controls +
	// space before following Location, so " //evil.tld" effectively
	// becomes "//evil.tld" (protocol-relative). We trim the same set
	// (NUL, \t, \n, \v, \f, \r, space) — slightly wider than the four
	// obvious ones because `\v` and `\f` pass net/url without error.
	if trimmed := strings.TrimLeft(raw, "\x00 \t\n\v\f\r"); trimmed != raw {
		return false
	}
	// Reject protocol-relative ("//foo") and authority-confusing
	// ("///foo", "////foo") forms. Net/url returns Host=="" for three
	// or more leading slashes (parsed as a path), but browsers collapse
	// them to a cross-origin redirect.
	if strings.HasPrefix(raw, "//") {
		return false
	}
	u, err := url.Parse(raw)
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
	return slices.Contains(allowList, raw)
}
