package auth0mock_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/pkg/auth0mock"
)

func TestFluent_WithArraysContainingSendsArraysAndRoundTrips(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	_, err := c.ExpectPost("/api/v2/users").
		WithBody(json.RawMessage(`{"app_metadata":{"roles":["a"]}}`)).
		WithArraysContaining().
		Respond(200).
		Apply(t.Context())
	require.NoError(t, err)

	var wire struct {
		Request struct {
			Arrays string `json:"arrays"`
		} `json:"request"`
	}
	require.NoError(t, json.Unmarshal(rec.last(t).Body, &wire))
	assert.Equal(t, "contains", wire.Request.Arrays)

	var sent auth0mock.Expectation
	require.NoError(t, json.Unmarshal(rec.last(t).Body, &sent))
	assert.Equal(t, auth0mock.ArraysContains, sent.Request.Arrays)
}

func TestRequestMatcher_ArraysOmittedWhenEmpty(t *testing.T) {
	t.Parallel()
	out, err := json.Marshal(auth0mock.RequestMatcher{Headers: map[string]string{"A": "b"}})
	require.NoError(t, err)
	assert.JSONEq(t, `{"headers":{"A":"b"}}`, string(out))
	assert.Equal(t, "exact", auth0mock.ArraysExact)
}
