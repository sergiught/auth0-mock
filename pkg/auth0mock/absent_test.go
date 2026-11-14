package auth0mock_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/pkg/auth0mock"
)

func TestFluent_WithoutFieldSendsAbsentAndRoundTrips(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	_, err := c.ExpectPost("/api/v2/users").
		WithQuery("q", "a").
		WithoutField("body.password").
		WithoutField("headers.X-Debug").
		Respond(200).
		Apply(t.Context())
	require.NoError(t, err)

	var wire struct {
		Request struct {
			Query  map[string]any `json:"query"`
			Absent []string       `json:"absent"`
		} `json:"request"`
	}
	require.NoError(t, json.Unmarshal(rec.last(t).Body, &wire))
	assert.Equal(t, []string{"body.password", "headers.X-Debug"}, wire.Request.Absent)
	assert.Equal(t, map[string]any{"q": "a"}, wire.Request.Query)

	var sent auth0mock.Expectation
	require.NoError(t, json.Unmarshal(rec.last(t).Body, &sent))
	assert.Equal(t, []string{"body.password", "headers.X-Debug"}, sent.Request.Absent)
}

func TestRequestMatcher_AbsentOmittedWhenEmpty(t *testing.T) {
	t.Parallel()
	out, err := json.Marshal(auth0mock.RequestMatcher{Headers: map[string]string{"A": "b"}})
	require.NoError(t, err)
	assert.JSONEq(t, `{"headers":{"A":"b"}}`, string(out))
}
