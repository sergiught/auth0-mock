package version

import (
	"strings"
	"testing"
)

func TestDefaults(t *testing.T) {
	t.Parallel()

	// A bare `go test` (no -ldflags) must leave the defaults in place; the
	// release/Makefile injection paths overwrite them but tests run without
	// either, so guard the contract here.
	for _, c := range []struct{ name, got, want string }{
		{"Version", Version, "dev"},
		{"Commit", Commit, "none"},
		{"Date", Date, "unknown"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestString(t *testing.T) {
	t.Parallel()

	s := String()
	for _, want := range []string{"auth0-mock", Version, Commit, Date} {
		if !strings.Contains(s, want) {
			t.Errorf("String() = %q, missing %q", s, want)
		}
	}
}
