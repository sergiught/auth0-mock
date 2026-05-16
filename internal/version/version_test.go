package version

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDefaults pins the no-ldflags contract: a bare `go test` (which
// tests run under) must observe the package-level defaults, because the
// release pipeline and `make build` are the only paths that inject real
// version metadata.
func TestDefaults(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, got, want string
	}{
		{"Version", Version, "dev"},
		{"Commit", Commit, "none"},
		{"Date", Date, "unknown"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, c.got, "ldflags injection must not be in effect under `go test`")
		})
	}
}

// TestString checks that the human-readable descriptor mentions every
// piece of metadata the package exposes, so a future field can't silently
// drop out of the output.
func TestString(t *testing.T) {
	t.Parallel()

	s := String()
	for _, want := range []string{"auth0-mock", Version, Commit, Date} {
		assert.Truef(t, strings.Contains(s, want),
			"String() = %q must mention %q", s, want)
	}
}
