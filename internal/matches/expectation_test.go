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

func TestServed_PicksByHits(t *testing.T) {
	t.Parallel()
	e := Expectation{Response: ResponseDef{Status: 429}, Then: []ResponseDef{{Status: 503}, {Status: 200}}}
	for hits, want := range map[int64]int{0: 429, 1: 429, 2: 503, 3: 200, 4: 200, 10: 200} {
		e.Hits = hits
		assert.Equal(t, want, e.Served().Status, "hits=%d", hits)
	}
}

func TestServed_EmptyThenIsSingle(t *testing.T) {
	t.Parallel()
	e := Expectation{Response: ResponseDef{Status: 429}, Then: []ResponseDef{}, Hits: 5}
	assert.Equal(t, 429, e.Served().Status)
}
