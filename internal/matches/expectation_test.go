package matches

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestKindOf locks the path-shape classifier that the registration store
// uses to decide whether a stored expectation is an exact-path match or
// a route-template match (the `{param}` form).
func TestKindOf(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path string
		want Kind
	}{
		{"/api/v2/users/{id}", KindTemplate},
		{"/api/v2/users/auth0|123", KindExact},
		{"/api/v2/clients", KindExact},
		{"/api/v2/a/{b}/c", KindTemplate},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, KindOf(c.path))
		})
	}
}
