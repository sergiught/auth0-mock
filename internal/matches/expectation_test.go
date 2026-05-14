package matches

import "testing"

func TestKindOf(t *testing.T) {
	cases := map[string]Kind{
		"/api/v2/users/{id}":      KindTemplate,
		"/api/v2/users/auth0|123": KindExact,
		"/api/v2/clients":         KindExact,
		"/api/v2/a/{b}/c":         KindTemplate,
	}
	for path, want := range cases {
		if got := KindOf(path); got != want {
			t.Errorf("KindOf(%q) = %v, want %v", path, got, want)
		}
	}
}
