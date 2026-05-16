package authapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsSafeRedirect_BypassClass is the adversarial table of returnTo /
// redirect_uri values the shared opt-in allow-list helper must reject.
// Each variant has bitten an open-redirect fix at some point in this
// project's history (or in some widely-deployed OAuth library) and the
// table exists to lock the door against the next surprise.
func TestIsSafeRedirect_BypassClass(t *testing.T) {
	t.Parallel()
	allow := []string{"https://app/cb"}

	// Each row should be REJECTED by isSafeRedirect (returns false) even
	// though some of them parse with empty host through net/url and would
	// trick a naive "host==''" check.
	rejected := []string{
		// URL-scheme bypass — non-empty scheme, empty host, all parse fine.
		"javascript:alert(1)",
		"data:text/html,<script>x</script>",
		"mailto:a@b.test",
		"file:///etc/passwd",
		"intent://x/#Intent;scheme=https;package=evil;end",

		// Backslash bypass — browsers normalise `\` → `/` before following.
		`\\evil.tld`,
		`/\evil.tld`,
		`https:\\evil.tld`,

		// Protocol-relative + authority-confusing leading-slash variants.
		"//evil.tld",
		"///evil.tld",
		"////evil.tld",

		// Leading WHATWG URL §C0 controls + space — browsers trim this
		// set before following Location, so " //evil.tld" becomes
		// "//evil.tld" (protocol-relative cross-origin). Today net/url
		// rejects every C0 control as "invalid control character" and
		// only U+0020 actually slips its parse, but isSafeRedirect
		// trims (and rejects) the full set anyway: defense-in-depth
		// plus future-proofing if net/url ever loosens.
		" //evil.tld",
		"\t//evil.tld",
		"\n//evil.tld",
		"\v//evil.tld",
		"\f//evil.tld",
		"\r//evil.tld",
		"\x00//evil.tld",

		// Absolute, non-allow-listed.
		"https://evil.tld/cb",
		"https://app/cb/extra",
		"https://app/cb?attacker=1",
	}
	for _, raw := range rejected {
		t.Run("reject:"+raw, func(t *testing.T) {
			assert.Falsef(t, isSafeRedirect(raw, allow),
				"isSafeRedirect(%q) returned true, want false", raw)
		})
	}

	// Each row should be ALLOWED by isSafeRedirect — either truly relative
	// or an exact match against the allow-list.
	allowed := []string{
		"/post-logout",
		"/post-logout?x=1",
		"/post-logout#section",
		"",               // Empty (callers default to "/" anyway).
		"https://app/cb", // Exact allow-list match.

		// Percent-encoded slashes / backslashes — browsers do NOT
		// re-decode `%2F` / `%5C` before re-parsing Location, so these
		// stay as literal path bytes on the mock origin, not //evil.tld.
		// Locking the analysis in here so a future "tighten the strip"
		// PR can't silently break it.
		"/%2F%2Fevil.tld",
		"/%5C%5Cevil.tld",
		"/%09//evil.tld",
		"/%20//evil.tld",

		// Unicode whitespace NOT in WHATWG URL §C0 + space. NBSP,
		// LSEP, PSEP, ZWSP, RLO are not stripped by browsers before
		// navigating Location, so they stay in the path. Pass them
		// in URL-encoded form (raw bytes would fail net/url's
		// "invalid control character" check on the multibyte
		// sequence).
		"/%C2%A0//evil.tld", // U+00A0 NBSP.
		"/%E2%80%A8/evil",   // U+2028 LSEP.
		"/%E2%80%8B/evil",   // U+200B ZWSP.
	}
	for _, raw := range allowed {
		t.Run("allow:"+raw, func(t *testing.T) {
			assert.Truef(t, isSafeRedirect(raw, allow),
				"isSafeRedirect(%q) returned false, want true", raw)
		})
	}
}
