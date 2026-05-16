package authapi

import "testing"

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

		// Leading whitespace — net/url keeps it in Path; browsers trim it.
		" //evil.tld",
		"\t//evil.tld",
		"\n//evil.tld",
		"\r//evil.tld",

		// Absolute, non-allow-listed.
		"https://evil.tld/cb",
		"https://app/cb/extra",
		"https://app/cb?attacker=1",
	}
	for _, raw := range rejected {
		t.Run("reject:"+raw, func(t *testing.T) {
			if isSafeRedirect(raw, allow) {
				t.Errorf("isSafeRedirect(%q) returned true, want false", raw)
			}
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
	}
	for _, raw := range allowed {
		t.Run("allow:"+raw, func(t *testing.T) {
			if !isSafeRedirect(raw, allow) {
				t.Errorf("isSafeRedirect(%q) returned false, want true", raw)
			}
		})
	}
}
