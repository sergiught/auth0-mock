package matches

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func TestExplain_NilMatcherHasNoDifferences(t *testing.T) {
	t.Parallel()
	assert.Empty(t, (*RequestMatcher)(nil).Explain(MatchableRequest{Body: []byte(`x`)}))
}

func TestExplain_Query(t *testing.T) {
	t.Parallel()
	rm := &RequestMatcher{Query: map[string]string{"page": "2"}}
	assert.Equal(t, []Difference{{Field: "query.page", Reason: "mismatch", Want: raw(`"2"`), Got: raw(`"1"`)}},
		rm.Explain(MatchableRequest{Query: url.Values{"page": {"1"}}}))
	assert.Equal(t, []Difference{{Field: "query.page", Reason: "missing", Want: raw(`"2"`)}},
		rm.Explain(MatchableRequest{Query: url.Values{"other": {"1"}}}))
}

func TestExplain_HeaderCanonicalised(t *testing.T) {
	t.Parallel()
	rm := &RequestMatcher{Headers: map[string]string{"x-tenant": "acme"}}
	assert.Equal(t, []Difference{{Field: "headers.X-Tenant", Reason: "missing", Want: raw(`"acme"`)}},
		rm.Explain(MatchableRequest{}))
}

func TestExplain_RedactedHeader(t *testing.T) {
	t.Parallel()
	rm := &RequestMatcher{Headers: map[string]string{"Authorization": "Bearer secret"}}
	diffs := rm.Explain(MatchableRequest{Headers: http.Header{"Authorization": {"Bearer other"}}})
	require.Len(t, diffs, 1)
	assert.Equal(t, raw(`"[REDACTED]"`), diffs[0].Want)
	assert.Equal(t, raw(`"[REDACTED]"`), diffs[0].Got)
	b, err := json.Marshal(diffs)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "secret")
	assert.NotContains(t, string(b), "other")
}

func TestExplain_BodyNested(t *testing.T) {
	t.Parallel()
	rm := &RequestMatcher{Body: raw(`{"user":{"blocked":true,"name":"a"}}`)}
	assert.Equal(t, []Difference{
		{Field: "body.user.blocked", Reason: "mismatch", Want: raw(`true`), Got: raw(`false`)},
		{Field: "body.user.name", Reason: "missing", Want: raw(`"a"`)},
	}, rm.Explain(MatchableRequest{Body: []byte(`{"user":{"blocked":false}}`)}))
}

func TestExplain_BodyArrayDeepEqual(t *testing.T) {
	t.Parallel()
	rm := &RequestMatcher{Body: raw(`{"roles":["a"]}`)}
	assert.Equal(t, []Difference{{Field: "body.roles", Reason: "mismatch", Want: raw(`["a"]`), Got: raw(`["a","b"]`)}},
		rm.Explain(MatchableRequest{Body: []byte(`{"roles":["a","b"]}`)}))
}

func TestExplain_BodyRootTypeMismatch(t *testing.T) {
	t.Parallel()
	rm := &RequestMatcher{Body: raw(`{"a":1}`)}
	assert.Equal(t, []Difference{{Field: "body", Reason: "mismatch", Want: raw(`{"a":1}`), Got: raw(`[1]`)}},
		rm.Explain(MatchableRequest{Body: []byte(`[1]`)}))
}

func TestExplain_BodyMissing(t *testing.T) {
	t.Parallel()
	rm := &RequestMatcher{Body: raw(`{"a":1}`)}
	assert.Equal(t, []Difference{{Field: "body", Reason: "missing", Want: raw(`{"a":1}`)}},
		rm.Explain(MatchableRequest{}))
}

func TestExplain_CapsLargeValues(t *testing.T) {
	t.Parallel()
	rm := &RequestMatcher{Body: raw(`{"a":true}`)}
	big := `{"a":{"k":"` + strings.Repeat("x", 5000) + `"}}`
	diffs := rm.Explain(MatchableRequest{Body: []byte(big)})
	require.Len(t, diffs, 1)
	var s string
	require.NoError(t, json.Unmarshal(diffs[0].Got, &s))
	assert.LessOrEqual(t, len(diffs[0].Got), maxDiffValue+10)
	assert.True(t, strings.HasSuffix(s, "…"))
	assert.Equal(t, maxDiffValue+len("…"), len(s))
}

func TestExplain_CapsLargeMissingBody(t *testing.T) {
	t.Parallel()
	rm := &RequestMatcher{Body: raw(`{"k":"` + strings.Repeat("x", 5000) + `"}`)}
	diffs := rm.Explain(MatchableRequest{})
	require.Len(t, diffs, 1)
	assert.Equal(t, "missing", diffs[0].Reason)
	var s string
	require.NoError(t, json.Unmarshal(diffs[0].Want, &s))
	assert.True(t, strings.HasSuffix(s, "…"))
	assert.Equal(t, maxDiffValue+len("…"), len(s))
}

func TestExplain_BodyInvalidJSON(t *testing.T) {
	t.Parallel()
	rm := &RequestMatcher{Body: raw(`{"a":1}`)}
	assert.Equal(t, []Difference{{Field: "body", Reason: "invalid_json"}},
		rm.Explain(MatchableRequest{Body: []byte(`not json`)}))
}

func TestExplain_AgreesWithMatches(t *testing.T) {
	t.Parallel()
	hdr := func(k, v string) http.Header { return http.Header{k: {v}} }
	cases := []struct {
		name string
		rm   *RequestMatcher
		req  MatchableRequest
	}{
		{"nil", nil, MatchableRequest{Body: []byte(`{"x":1}`)}},
		{"body subset", &RequestMatcher{Body: raw(`{"email":"a@x"}`)}, MatchableRequest{Body: []byte(`{"email":"a@x","c":"db"}`)}},
		{"body value mismatch", &RequestMatcher{Body: raw(`{"email":"a@x"}`)}, MatchableRequest{Body: []byte(`{"email":"b@x"}`)}},
		{"body nil request", &RequestMatcher{Body: raw(`{"email":"a@x"}`)}, MatchableRequest{}},
		{"query ok", &RequestMatcher{Query: map[string]string{"q": "e"}}, MatchableRequest{Query: url.Values{"q": {"e"}, "p": {"1"}}}},
		{"query mismatch", &RequestMatcher{Query: map[string]string{"q": "e"}}, MatchableRequest{Query: url.Values{"q": {"f"}}}},
		{"query missing", &RequestMatcher{Query: map[string]string{"q": "e"}}, MatchableRequest{Query: url.Values{}}},
		{"query empty want absent", &RequestMatcher{Query: map[string]string{"q": ""}}, MatchableRequest{}},
		{"header ok", &RequestMatcher{Headers: map[string]string{"X-Tenant": "acme"}}, MatchableRequest{Headers: hdr("X-Tenant", "acme")}},
		{"header extra ok", &RequestMatcher{Headers: map[string]string{"X-Tenant": "acme"}}, MatchableRequest{Headers: http.Header{"X-Tenant": {"acme"}, "X-Other": {"v"}}}},
		{"header mismatch", &RequestMatcher{Headers: map[string]string{"X-Tenant": "acme"}}, MatchableRequest{Headers: hdr("X-Tenant", "globex")}},
		{"header missing", &RequestMatcher{Headers: map[string]string{"X-Tenant": "acme"}}, MatchableRequest{}},
		{"header lowercase key", &RequestMatcher{Headers: map[string]string{"x-tenant": "acme"}}, MatchableRequest{Headers: hdr("X-Tenant", "acme")}},
		{"combined ok", &RequestMatcher{Headers: map[string]string{"Authorization": "Bearer foo"}, Body: raw(`{"k":"v"}`)}, MatchableRequest{Headers: hdr("Authorization", "Bearer foo"), Body: []byte(`{"k":"v","e":1}`)}},
		{"combined header fails", &RequestMatcher{Headers: map[string]string{"Authorization": "Bearer foo"}, Body: raw(`{"k":"v"}`)}, MatchableRequest{Headers: hdr("Authorization", "Bearer bar"), Body: []byte(`{"k":"v"}`)}},
		{"nested body", &RequestMatcher{Body: raw(`{"user":{"blocked":true}}`)}, MatchableRequest{Body: []byte(`{"user":{"blocked":false}}`)}},
		{"array equal", &RequestMatcher{Body: raw(`{"roles":["a"]}`)}, MatchableRequest{Body: []byte(`{"roles":["a"]}`)}},
		{"array superset", &RequestMatcher{Body: raw(`{"roles":["a"]}`)}, MatchableRequest{Body: []byte(`{"roles":["a","b"]}`)}},
		{"root type mismatch", &RequestMatcher{Body: raw(`{"a":1}`)}, MatchableRequest{Body: []byte(`[1]`)}},
		{"invalid request json", &RequestMatcher{Body: raw(`{"a":1}`)}, MatchableRequest{Body: []byte(`not json`)}},
		{"scalar body", &RequestMatcher{Body: raw(`"x"`)}, MatchableRequest{Body: []byte(`"x"`)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want := oracleMatches(c.rm, c.req)
			assert.Equal(t, want, c.rm.Matches(c.req))
			assert.Equal(t, want, len(c.rm.Explain(c.req)) == 0)
		})
	}
	assert.GreaterOrEqual(t, len(cases), 15)
}

// oracleMatches decides a match independently of Explain: subsetMatch for the
// body, a direct first-value query check and a case-insensitive header check.
func oracleMatches(rm *RequestMatcher, req MatchableRequest) bool {
	if rm == nil {
		return true
	}
	for k, want := range rm.Query {
		if vs := req.Query[k]; (len(vs) == 0 && want != "") || (len(vs) > 0 && vs[0] != want) {
			return false
		}
	}
	for k, want := range rm.Headers {
		if oracleHeader(req.Headers, k) != want {
			return false
		}
	}
	body := bytes.TrimSpace(rm.Body)
	if len(body) == 0 || string(body) == "null" {
		return true
	}
	var w, g any
	if json.Unmarshal(body, &w) != nil || json.Unmarshal(req.Body, &g) != nil {
		return false
	}
	return subsetMatch(w, g)
}

func oracleHeader(h http.Header, key string) string {
	for name, vs := range h {
		if strings.EqualFold(name, key) && len(vs) > 0 {
			return vs[0]
		}
	}
	return ""
}

func tmplOf(_, p string) string {
	switch p {
	case "/api/v2/users/123", "/api/v2/users/124", "/api/v2/users/{id}":
		return "/api/v2/users/{id}"
	}
	return p
}

// queryStub returns a stub requiring every named query key, so a request
// with no query differs on exactly len(fields) keys.
func queryStub(path string, kind Kind, method string, fields ...string) Expectation {
	rm := &RequestMatcher{Query: map[string]string{}}
	for _, f := range fields {
		rm.Query[f] = "want"
	}
	return Expectation{Method: method, Path: path, Kind: kind, Request: rm}
}

func TestNearMisses_RankingAndCap(t *testing.T) {
	t.Parallel()
	s := NewStore()
	const tpl = "/api/v2/users/{id}"
	s.Put(queryStub(tpl, KindTemplate, "GET", "a", "b", "c"))
	oneOld := s.Put(queryStub(tpl, KindTemplate, "GET", "a"))
	two := s.Put(queryStub(tpl, KindTemplate, "GET", "a", "b"))
	oneNew := s.Put(queryStub(tpl, KindTemplate, "GET", "b"))

	got := s.NearMisses("GET", "/api/v2/users/123", tpl, MatchableRequest{}, tmplOf)
	require.Len(t, got, 3)
	assert.Equal(t, []string{oneNew.ID, oneOld.ID, two.ID},
		[]string{got[0].ExpectationID, got[1].ExpectationID, got[2].ExpectationID})
}

func TestNearMisses_TiesAreNewestFirstAcrossBuckets(t *testing.T) {
	t.Parallel()
	s := NewStore()
	const tpl, path = "/api/v2/users/{id}", "/api/v2/users/123"
	s.Put(queryStub(path, KindExact, "GET", "a"))
	exactB := s.Put(queryStub(path, KindExact, "GET", "b"))
	exactC := s.Put(queryStub(path, KindExact, "GET", "c"))
	newest := s.Put(queryStub(tpl, KindTemplate, "GET", "d"))

	got := s.NearMisses("GET", path, tpl, MatchableRequest{}, tmplOf)
	require.Len(t, got, 3)
	assert.Equal(t, []string{newest.ID, exactC.ID, exactB.ID},
		[]string{got[0].ExpectationID, got[1].ExpectationID, got[2].ExpectationID})
}

func TestNearMisses_ExactStubOnOtherConcretePath(t *testing.T) {
	t.Parallel()
	s := NewStore()
	e := s.Put(queryStub("/api/v2/users/123", KindExact, "GET", "a"))
	got := s.NearMisses("GET", "/api/v2/users/124", "/api/v2/users/{id}", MatchableRequest{}, tmplOf)
	require.Len(t, got, 1)
	assert.Equal(t, e.ID, got[0].ExpectationID)
	assert.Equal(t, "/api/v2/users/123", got[0].Path)
	assert.Equal(t, []Difference{
		{Field: "path", Reason: "mismatch", Want: raw(`"/api/v2/users/123"`), Got: raw(`"/api/v2/users/124"`)},
		{Field: "query.a", Reason: "missing", Want: raw(`"want"`)},
	}, got[0].Differences)
}

func TestNearMisses_OtherMethodAndOtherOperationIgnored(t *testing.T) {
	t.Parallel()
	s := NewStore()
	s.Put(queryStub("/api/v2/users/{id}", KindTemplate, "GET", "a"))
	s.Put(queryStub("/api/v2/roles/{id}", KindTemplate, "PATCH", "a"))
	got := s.NearMisses("PATCH", "/api/v2/users/123", "/api/v2/users/{id}", MatchableRequest{}, tmplOf)
	assert.Empty(t, got)
}

func TestNearMisses_NoStubs(t *testing.T) {
	t.Parallel()
	got := NewStore().NearMisses("GET", "/api/v2/users/1", "/api/v2/users/{id}", MatchableRequest{}, tmplOf)
	assert.Len(t, got, 0)
}
