package auth0mock_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/pkg/auth0mock"
)

func TestFluent_WithQueryAllSendsListAndRoundTrips(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	_, err := c.ExpectGet("/api/v2/users").
		WithQuery("q", "a").
		WithQueryAll("fields", "name", "email").
		WithQueryAll("one", "x").
		Respond(200).
		Apply(t.Context())
	require.NoError(t, err)

	var wire struct {
		Request struct {
			Query map[string]any `json:"query"`
		} `json:"request"`
	}
	require.NoError(t, json.Unmarshal(rec.last(t).Body, &wire))
	assert.Equal(t, map[string]any{"q": "a", "fields": []any{"name", "email"}, "one": "x"}, wire.Request.Query)

	var sent auth0mock.Expectation
	require.NoError(t, json.Unmarshal(rec.last(t).Body, &sent))
	assert.Equal(t, map[string]string{"q": "a", "one": "x"}, sent.Request.Query)
	assert.Equal(t, map[string][]string{"fields": {"name", "email"}}, sent.Request.QueryAll)
}

func TestRequestMatcher_QueryInBothMarshalsUnion(t *testing.T) {
	t.Parallel()
	out, err := json.Marshal(auth0mock.RequestMatcher{
		Query:    map[string]string{"k": "a"},
		QueryAll: map[string][]string{"k": {"b", "c"}},
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{"query":{"k":["a","b","c"]}}`, string(out))
}

func TestRequestMatcher_KeepsHeadersAndBody(t *testing.T) {
	t.Parallel()
	var rm auth0mock.RequestMatcher
	require.NoError(t, json.Unmarshal([]byte(`{"headers":{"X-A":"1"},"body":{"a":1}}`), &rm))
	assert.Equal(t, map[string]string{"X-A": "1"}, rm.Headers)
	assert.JSONEq(t, `{"a":1}`, string(rm.Body))
}

func TestRequestMatcher_UnmarshalRejectsBadQueryValue(t *testing.T) {
	t.Parallel()
	var rm auth0mock.RequestMatcher
	require.Error(t, json.Unmarshal([]byte(`{"query":{"k":5}}`), &rm))
}
