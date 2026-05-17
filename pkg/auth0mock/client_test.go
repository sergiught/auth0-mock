package auth0mock_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/pkg/auth0mock"
)

func TestNewClient_TrimsTrailingSlash(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"http://localhost:8080",
		"http://localhost:8080/",
		"http://localhost:8080///",
	} {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			c, err := auth0mock.NewClient(in)
			require.NoError(t, err)
			assert.Equal(t, "http://localhost:8080", c.BaseURL(),
				"trailing slashes must be trimmed so request paths concatenate cleanly")
		})
	}
}

func TestNewClient_DefaultHTTPClient(t *testing.T) {
	t.Parallel()
	c, err := auth0mock.NewClient("http://localhost:8080")
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, "http://localhost:8080", c.BaseURL())
}

// TestNewClient_RejectsInvalidURLs locks the URL-validation contract:
// constructors must fail fast on empty / unparsable / schemeless input
// so a typo can't surface as a baffling transport error on the first
// SDK call.
func TestNewClient_RejectsInvalidURLs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		url  string
	}{
		{"empty", ""},
		{"no scheme", "localhost:8080"},
		{"no host", "http://"},
		// Url.Parse is permissive; "::not a url" is rejected as parse error.
		{"unparsable", "http://[::not a url"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, err := auth0mock.NewClient(tc.url)
			require.Error(t, err, "want NewClient to reject %q", tc.url)
			assert.Nil(t, c)
			assert.Contains(t, err.Error(), "auth0mock: NewClient")
		})
	}
}

func TestWithHTTPClient_Override(t *testing.T) {
	t.Parallel()
	custom := &http.Client{Timeout: 1 * time.Millisecond}
	c, err := auth0mock.NewClient("http://localhost:8080", auth0mock.WithHTTPClient(custom))
	require.NoError(t, err)
	require.NotNil(t, c)
	// Don't assert on internal fields — assert on observable behavior
	// in the Reset tests, where a 1ms timeout produces a deadline-
	// exceeded transport error.
	_ = c
}

func TestWithHTTPClient_NilIsNoop(t *testing.T) {
	t.Parallel()
	// Passing nil must NOT zero out the default client — that would
	// silently break callers who pass `WithHTTPClient(someBuilder())`
	// when someBuilder returns nil under error.
	c, err := auth0mock.NewClient("http://localhost:8080", auth0mock.WithHTTPClient(nil))
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, "http://localhost:8080", c.BaseURL())
}
