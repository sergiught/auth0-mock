package matches

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAbsent_Matches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		absent []string
		req    MatchableRequest
		want   bool
	}{
		{"query absent", []string{"query.x"}, MatchableRequest{Query: url.Values{"y": {"1"}}}, true},
		{"query present", []string{"query.x"}, MatchableRequest{Query: url.Values{"x": {"1"}}}, false},
		{"query present but empty", []string{"query.x"}, MatchableRequest{Query: url.Values{"x": {""}}}, false},
		{"header absent", []string{"headers.X-Foo"}, MatchableRequest{Headers: http.Header{}}, true},
		{"header present", []string{"headers.X-Foo"}, MatchableRequest{Headers: http.Header{"X-Foo": {"v"}}}, false},
		{"header path any case", []string{"headers.x-foo"}, MatchableRequest{Headers: http.Header{"X-Foo": {"v"}}}, false},
		{"header present but empty", []string{"headers.X-Foo"}, MatchableRequest{Headers: http.Header{"X-Foo": {""}}}, false},
		{"body no body", []string{"body.a"}, MatchableRequest{}, true},
		{"body not json", []string{"body.a"}, MatchableRequest{Body: []byte(`nope`)}, true},
		{"body path missing", []string{"body.a"}, MatchableRequest{Body: []byte(`{"b":1}`)}, true},
		{"body path present", []string{"body.a"}, MatchableRequest{Body: []byte(`{"a":1}`)}, false},
		{"body path null is present", []string{"body.a"}, MatchableRequest{Body: []byte(`{"a":null}`)}, false},
		{"body parent missing", []string{"body.a.b"}, MatchableRequest{Body: []byte(`{"c":1}`)}, true},
		{"body parent not object", []string{"body.a.b"}, MatchableRequest{Body: []byte(`{"a":5}`)}, true},
		{"body nested present", []string{"body.a.b"}, MatchableRequest{Body: []byte(`{"a":{"b":false}}`)}, false},
		{"one of several present", []string{"query.x", "query.y"}, MatchableRequest{Query: url.Values{"y": {"1"}}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rm := &RequestMatcher{Absent: c.absent}
			assert.Equal(t, c.want, rm.Matches(c.req))
			assert.Equal(t, c.want, len(rm.Explain(c.req)) == 0, "Explain agrees with Matches")
		})
	}
}

func TestAbsent_IsEmpty(t *testing.T) {
	t.Parallel()
	assert.False(t, (&RequestMatcher{Absent: []string{"query.x"}}).IsEmpty())
	assert.True(t, (&RequestMatcher{Absent: []string{}}).IsEmpty())
}

func TestAbsent_Equal(t *testing.T) {
	t.Parallel()
	a := &RequestMatcher{Absent: []string{"query.x", "headers.x-foo"}}
	b := &RequestMatcher{Absent: []string{"headers.X-Foo", "query.x"}}
	assert.True(t, requestMatcherEqual(a, b))
	assert.False(t, requestMatcherEqual(a, &RequestMatcher{Absent: []string{"query.x"}}))
	assert.False(t, requestMatcherEqual(a, &RequestMatcher{}))
}

func TestAbsent_Explain(t *testing.T) {
	t.Parallel()
	rm := &RequestMatcher{Absent: []string{"query.x", "headers.authorization", "headers.x-foo", "body.a.b"}}
	diffs := rm.Explain(MatchableRequest{
		Query:   url.Values{"x": {"1"}},
		Headers: http.Header{"Authorization": {"Bearer secret"}, "X-Foo": {"v"}},
		Body:    []byte(`{"a":{"b":null}}`),
	})
	assert.Equal(t, []Difference{
		{Field: "body.a.b", Reason: "present", Got: raw(`null`)},
		{Field: "headers.Authorization", Reason: "present", Got: raw(`"[REDACTED]"`)},
		{Field: "headers.X-Foo", Reason: "present", Got: raw(`"v"`)},
		{Field: "query.x", Reason: "present", Got: raw(`"1"`)},
	}, diffs)
}

func TestAbsent_ExplainReportsDuplicatePathOnce(t *testing.T) {
	t.Parallel()
	rm := &RequestMatcher{Absent: []string{"headers.x-foo", "headers.X-Foo"}}
	diffs := rm.Explain(MatchableRequest{Headers: http.Header{"X-Foo": {"v"}}})
	assert.Equal(t, []Difference{{Field: "headers.X-Foo", Reason: "present", Got: raw(`"v"`)}}, diffs)
}

func TestAbsent_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	var rm RequestMatcher
	require.NoError(t, json.Unmarshal([]byte(`{"query":{"a":"1"},"absent":["query.x","headers.Y"]}`), &rm))
	assert.Equal(t, []string{"query.x", "headers.Y"}, rm.Absent)
	out, err := json.Marshal(rm)
	require.NoError(t, err)
	assert.JSONEq(t, `{"query":{"a":"1"},"absent":["query.x","headers.Y"]}`, string(out))
}

func TestAbsent_Validate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		rm   RequestMatcher
		ok   bool
	}{
		{"valid", RequestMatcher{Absent: []string{"query.x", "headers.X", "body.a.b"}}, true},
		{"no prefix", RequestMatcher{Absent: []string{"x"}}, false},
		{"unknown prefix", RequestMatcher{Absent: []string{"cookies.x"}}, false},
		{"empty query key", RequestMatcher{Absent: []string{"query."}}, false},
		{"empty header", RequestMatcher{Absent: []string{"headers."}}, false},
		{"empty body segment", RequestMatcher{Absent: []string{"body.a..b"}}, false},
		{"bare body", RequestMatcher{Absent: []string{"body."}}, false},
		{"query also required", RequestMatcher{Query: map[string][]string{"x": {"1"}}, Absent: []string{"query.x"}}, false},
		{"header also required", RequestMatcher{Headers: map[string]string{"x-foo": "1"}, Absent: []string{"headers.X-Foo"}}, false},
		{"body exact", RequestMatcher{Body: raw(`{"a":{"b":1}}`), Absent: []string{"body.a.b"}}, false},
		{"body ancestor set", RequestMatcher{Body: raw(`{"a":{"b":1}}`), Absent: []string{"body.a"}}, false},
		{"body below scalar", RequestMatcher{Body: raw(`{"a":{"b":1}}`), Absent: []string{"body.a.b.c"}}, true},
		{"body below array", RequestMatcher{Body: raw(`{"a":[1]}`), Absent: []string{"body.a.b"}}, true},
		{"host header", RequestMatcher{Absent: []string{"headers.host"}}, false},
		{"body sibling ok", RequestMatcher{Body: raw(`{"a":{"b":1}}`), Absent: []string{"body.a.c"}}, true},
		{"body other key ok", RequestMatcher{Body: raw(`{"a":1}`), Absent: []string{"body.password"}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.rm.validateAbsent()
			if c.ok {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}
