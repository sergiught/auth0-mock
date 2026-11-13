package matches_test

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/matches"
)

func decodeMatcher(t *testing.T, src string) *matches.RequestMatcher {
	t.Helper()
	var rm matches.RequestMatcher
	require.NoError(t, json.Unmarshal([]byte(src), &rm))
	return &rm
}

func TestQueryMatcherJSONRoundTrip(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`{"query":{"q":"a"}}`,
		`{"query":{"tag":["a","b"]}}`,
		`{"query":{"q":"a","tag":["a","a"]},"headers":{"X-A":"1"},"body":{"x":1}}`,
	} {
		rm := decodeMatcher(t, src)
		out, err := json.Marshal(rm)
		require.NoError(t, err)
		assert.JSONEq(t, src, string(out), src)
	}
}

func TestQueryMatcherOneElementListMarshalsAsString(t *testing.T) {
	t.Parallel()
	out, err := json.Marshal(decodeMatcher(t, `{"query":{"q":["a"]}}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"query":{"q":"a"}}`, string(out))
}

func TestQueryMatchesAnyRepeatedValue(t *testing.T) {
	t.Parallel()
	rm := decodeMatcher(t, `{"query":{"tag":"a"}}`)
	assert.True(t, rm.Matches(matches.MatchableRequest{Query: url.Values{"tag": {"b", "a"}}}))
	assert.False(t, rm.Matches(matches.MatchableRequest{Query: url.Values{"tag": {"b", "c"}}}))
}

func TestQueryListMatchesAllValues(t *testing.T) {
	t.Parallel()
	rm := decodeMatcher(t, `{"query":{"tag":["a","b"]}}`)
	assert.True(t, rm.Matches(matches.MatchableRequest{Query: url.Values{"tag": {"b", "x", "a"}}}))
	assert.False(t, rm.Matches(matches.MatchableRequest{Query: url.Values{"tag": {"a"}}}))

	dup := decodeMatcher(t, `{"query":{"tag":["a","a"]}}`)
	assert.False(t, dup.Matches(matches.MatchableRequest{Query: url.Values{"tag": {"a", "b"}}}))
	assert.True(t, dup.Matches(matches.MatchableRequest{Query: url.Values{"tag": {"a", "b", "a"}}}))
}

func TestQueryListNearMiss(t *testing.T) {
	t.Parallel()
	rm := decodeMatcher(t, `{"query":{"tag":["a","b"]}}`)
	assert.Equal(t,
		[]matches.Difference{{Field: "query.tag", Reason: "mismatch", Want: json.RawMessage(`["a","b"]`), Got: json.RawMessage(`"a"`)}},
		rm.Explain(matches.MatchableRequest{Query: url.Values{"tag": {"a"}}}))
	assert.Equal(t,
		[]matches.Difference{{Field: "query.tag", Reason: "mismatch", Want: json.RawMessage(`["a","b"]`), Got: json.RawMessage(`["a","c"]`)}},
		rm.Explain(matches.MatchableRequest{Query: url.Values{"tag": {"a", "c"}}}))
	assert.Equal(t,
		[]matches.Difference{{Field: "query.tag", Reason: "missing", Want: json.RawMessage(`["a","b"]`)}},
		rm.Explain(matches.MatchableRequest{Query: url.Values{}}))

	str := decodeMatcher(t, `{"query":{"tag":"a"}}`)
	assert.Equal(t,
		[]matches.Difference{{Field: "query.tag", Reason: "mismatch", Want: json.RawMessage(`"a"`), Got: json.RawMessage(`["b","c"]`)}},
		str.Explain(matches.MatchableRequest{Query: url.Values{"tag": {"b", "c"}}}))
}

func TestQueryListRegistrationRejections(t *testing.T) {
	t.Parallel()
	v := newValidator(t)
	for name, src := range map[string]string{
		"empty list":      `{"query":{"fields":[]}}`,
		"non-string item": `{"query":{"fields":["a",1]}}`,
		"number value":    `{"query":{"fields":5}}`,
		"object value":    `{"query":{"fields":{"a":"b"}}}`,
		"null value":      `{"query":{"fields":null}}`,
		"null item":       `{"query":{"fields":["a",null]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			var reg matches.Registration
			require.NoError(t, json.Unmarshal([]byte(`{"method":"GET","path":"/api/v2/users","response":{"status":429},"request":`+src+`}`), &reg))
			_, err := matches.Prepare(v, reg)
			var re *matches.RegistrationError
			require.ErrorAs(t, err, &re)
			assert.Equal(t, "invalid_request_match", re.Code)
		})
	}
}

func TestQueryListRegistrationAccepted(t *testing.T) {
	t.Parallel()
	v := newValidator(t)
	var reg matches.Registration
	require.NoError(t, json.Unmarshal([]byte(`{"method":"GET","path":"/api/v2/users","response":{"status":429},"request":{"query":{"fields":["a","b"]}}}`), &reg))
	e, err := matches.Prepare(v, reg)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, e.Request.Query["fields"])
}

func TestSameTargetTreatsQueryListsAsMultisets(t *testing.T) {
	t.Parallel()
	mk := func(src string) matches.Expectation {
		return matches.Expectation{Method: "GET", Path: "/api/v2/users", Kind: matches.KindExact, Request: decodeMatcher(t, src)}
	}
	assert.True(t, matches.SameTarget(mk(`{"query":{"t":["a","b"]}}`), mk(`{"query":{"t":["b","a"]}}`)))
	assert.True(t, matches.SameTarget(mk(`{"query":{"t":"a"}}`), mk(`{"query":{"t":["a"]}}`)))
	assert.False(t, matches.SameTarget(mk(`{"query":{"t":["a","a"]}}`), mk(`{"query":{"t":["a","b"]}}`)))
	assert.False(t, matches.SameTarget(mk(`{"query":{"t":["a"]}}`), mk(`{"query":{"t":["a","b"]}}`)))
}

func TestMistypedAbsentIsInvalidRequestMatch(t *testing.T) {
	t.Parallel()
	v := newValidator(t)
	for name, src := range map[string]string{
		"string":          `{"absent":"body.x"}`,
		"non-string item": `{"absent":["body.x",1]}`,
		"null":            `{"absent":null}`,
		"null item":       `{"absent":["body.x",null]}`,
	} {
		t.Run(name, func(t *testing.T) {
			var reg matches.Registration
			require.NoError(t, json.Unmarshal([]byte(`{"method":"GET","path":"/api/v2/users","response":{"status":429},"request":`+src+`}`), &reg))
			_, err := matches.Prepare(v, reg)
			var re *matches.RegistrationError
			require.ErrorAs(t, err, &re)
			assert.Equal(t, "invalid_request_match", re.Code)
		})
	}
}

func TestEmptyAbsentListIsAccepted(t *testing.T) {
	t.Parallel()
	v := newValidator(t)
	var reg matches.Registration
	require.NoError(t, json.Unmarshal([]byte(`{"method":"GET","path":"/api/v2/users","response":{"status":429},"request":{"absent":[]}}`), &reg))
	_, err := matches.Prepare(v, reg)
	require.NoError(t, err)
}

func TestEventsStubRejectsBodyAbsent(t *testing.T) {
	t.Parallel()
	v := newValidator(t)
	reg := func(absent string) matches.Registration {
		var r matches.Registration
		require.NoError(t, json.Unmarshal([]byte(`{"method":"GET","path":"/api/v2/events","response":{"status":410},"request":{"absent":["`+absent+`"]}}`), &r))
		return r
	}
	_, err := matches.Prepare(v, reg("body.x"))
	var re *matches.RegistrationError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, "invalid_request_match", re.Code)
	_, err = matches.Prepare(v, reg("headers.Last-Event-Id"))
	require.NoError(t, err)
}
