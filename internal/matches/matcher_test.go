package matches

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSubsetMatch(t *testing.T) {
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
}

func TestRequestMatcher_IsEmpty(t *testing.T) {
	var nilM *RequestMatcher
	assert.True(t, nilM.IsEmpty())
	assert.True(t, (&RequestMatcher{}).IsEmpty())
	assert.True(t, (&RequestMatcher{Body: json.RawMessage(`null`)}).IsEmpty())
	assert.True(t, (&RequestMatcher{Body: json.RawMessage("  ")}).IsEmpty())
	assert.False(t, (&RequestMatcher{Query: map[string]string{"a": "b"}}).IsEmpty())
	assert.False(t, (&RequestMatcher{Body: json.RawMessage(`{"a":1}`)}).IsEmpty())
}

func TestRequestMatcherEqual(t *testing.T) {
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
}
