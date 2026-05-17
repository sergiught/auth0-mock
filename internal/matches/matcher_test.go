package matches

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSubsetMatch(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		want, got  string
		shouldPass bool
	}{
		{"object extra keys allowed", `{"a":1}`, `{"a":1,"b":2}`, true},
		{"object missing key fails", `{"a":1,"b":2}`, `{"a":1}`, false},
		{"object value mismatch fails", `{"a":1}`, `{"a":2}`, false},
		{"nested object subset", `{"a":{"b":1}}`, `{"a":{"b":1,"c":2}}`, true},
		{"nested object mismatch", `{"a":{"b":1}}`, `{"a":{"b":2}}`, false},
		{"array exact equality passes", `[1,2]`, `[1,2]`, true},
		{"array subset fails (exact only)", `[1]`, `[1,2]`, false},
		{"array order matters", `[1,2]`, `[2,1]`, false},
		{"scalar equality", `"x"`, `"x"`, true},
		{"scalar mismatch", `"x"`, `"y"`, false},
		{"object vs non-object fails", `{"a":1}`, `5`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var w, g any
			assert.NoError(t, json.Unmarshal([]byte(c.want), &w))
			assert.NoError(t, json.Unmarshal([]byte(c.got), &g))
			assert.Equal(t, c.shouldPass, subsetMatch(w, g))
		})
	}
}

func TestRequestMatcher_Matches(t *testing.T) {
	t.Parallel()
	var catchAll *RequestMatcher
	assert.True(t, catchAll.Matches(MatchableRequest{Body: []byte(`{"anything":true}`)}))

	body := &RequestMatcher{Body: json.RawMessage(`{"email":"a@x"}`)}
	assert.True(t, body.Matches(MatchableRequest{Body: []byte(`{"email":"a@x","connection":"db"}`)}))
	assert.False(t, body.Matches(MatchableRequest{Body: []byte(`{"email":"b@x"}`)}))
	assert.False(t, body.Matches(MatchableRequest{Body: nil}))

	query := &RequestMatcher{Query: map[string]string{"q": "email:a@x"}}
	assert.True(t, query.Matches(MatchableRequest{Query: url.Values{"q": {"email:a@x"}, "page": {"1"}}}))
	assert.False(t, query.Matches(MatchableRequest{Query: url.Values{"q": {"email:b@x"}}}))
	assert.False(t, query.Matches(MatchableRequest{Query: url.Values{}}))

	// Header matcher — single key.
	headers := &RequestMatcher{Headers: map[string]string{"X-Tenant": "acme"}}
	assert.True(t, headers.Matches(MatchableRequest{Headers: http.Header{"X-Tenant": {"acme"}}}))
	assert.True(t, headers.Matches(MatchableRequest{Headers: http.Header{"X-Tenant": {"acme"}, "X-Other": {"v"}}}),
		"extra headers must not disqualify a match (subset semantics)")
	assert.False(t, headers.Matches(MatchableRequest{Headers: http.Header{"X-Tenant": {"globex"}}}))
	assert.False(t, headers.Matches(MatchableRequest{}))

	// Header matcher — case-insensitive (HTTP canonical form via http.Header.Get).
	caseInsensitive := &RequestMatcher{Headers: map[string]string{"x-tenant": "acme"}}
	assert.True(t, caseInsensitive.Matches(MatchableRequest{Headers: http.Header{"X-Tenant": {"acme"}}}),
		"matcher header key must be canonicalised case-insensitively")

	// Header matcher — combined with body subset.
	combined := &RequestMatcher{
		Headers: map[string]string{"Authorization": "Bearer foo"},
		Body:    json.RawMessage(`{"k":"v"}`),
	}
	assert.True(t, combined.Matches(MatchableRequest{
		Headers: http.Header{"Authorization": {"Bearer foo"}},
		Body:    []byte(`{"k":"v","extra":1}`),
	}))
	assert.False(t, combined.Matches(MatchableRequest{
		Headers: http.Header{"Authorization": {"Bearer bar"}},
		Body:    []byte(`{"k":"v"}`),
	}), "header mismatch alone is enough to fail the match")
}

func TestRequestMatcher_IsEmpty(t *testing.T) {
	t.Parallel()
	var nilM *RequestMatcher
	assert.True(t, nilM.IsEmpty())
	assert.True(t, (&RequestMatcher{}).IsEmpty())
	assert.True(t, (&RequestMatcher{Body: json.RawMessage(`null`)}).IsEmpty())
	assert.True(t, (&RequestMatcher{Body: json.RawMessage("  ")}).IsEmpty())
	assert.False(t, (&RequestMatcher{Query: map[string]string{"a": "b"}}).IsEmpty())
	assert.False(t, (&RequestMatcher{Body: json.RawMessage(`{"a":1}`)}).IsEmpty())
	assert.False(t, (&RequestMatcher{Headers: map[string]string{"X-Tenant": "acme"}}).IsEmpty(),
		"a header-only matcher must not collapse to catch-all")
}

func TestRequestMatcherEqual(t *testing.T) {
	t.Parallel()
	assert.True(t, requestMatcherEqual(nil, nil))
	assert.False(t, requestMatcherEqual(nil, &RequestMatcher{}))
	assert.True(t, requestMatcherEqual(
		&RequestMatcher{Body: json.RawMessage(`{"a":1}`)},
		&RequestMatcher{Body: json.RawMessage(`{ "a": 1 }`)},
	))
	assert.False(t, requestMatcherEqual(
		&RequestMatcher{Body: json.RawMessage(`{"a":1}`)},
		&RequestMatcher{Body: json.RawMessage(`{"a":2}`)},
	))
	assert.True(t, requestMatcherEqual(
		&RequestMatcher{Query: map[string]string{"a": "b"}},
		&RequestMatcher{Query: map[string]string{"a": "b"}},
	))
	assert.False(t, requestMatcherEqual(
		&RequestMatcher{Query: map[string]string{"a": "b"}},
		&RequestMatcher{Query: map[string]string{"a": "c"}},
	))

	// Two matchers with equal Headers maps are equal.
	assert.True(t, requestMatcherEqual(
		&RequestMatcher{Headers: map[string]string{"X-Tenant": "acme"}},
		&RequestMatcher{Headers: map[string]string{"X-Tenant": "acme"}},
	))
	assert.False(t, requestMatcherEqual(
		&RequestMatcher{Headers: map[string]string{"X-Tenant": "acme"}},
		&RequestMatcher{Headers: map[string]string{"X-Tenant": "globex"}},
	))
}
