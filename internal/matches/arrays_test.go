package matches

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArrays_Matches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		arrays string
		want   string
		got    string
		match  bool
	}{
		{"exact extra item", "", `{"t":["a"]}`, `{"t":["a","b"]}`, false},
		{"exact explicit extra item", "exact", `{"t":["a"]}`, `{"t":["a","b"]}`, false},
		{"contains extra item", "contains", `{"t":["a"]}`, `{"t":["a","b"]}`, true},
		{"contains other order", "contains", `{"t":["a","b"]}`, `{"t":["b","a"]}`, true},
		{"contains missing item", "contains", `{"t":["a","c"]}`, `{"t":["a","b"]}`, false},
		{"contains duplicates need two", "contains", `{"t":["a","a"]}`, `{"t":["a"]}`, false},
		{"contains duplicates ok", "contains", `{"t":["a","a"]}`, `{"t":["a","b","a"]}`, true},
		{"contains empty want", "contains", `{"t":[]}`, `{"t":["a"]}`, true},
		{"contains objects subset", "contains", `{"t":[{"id":1}]}`, `{"t":[{"id":2},{"id":1,"x":true}]}`, true},
		{"contains backtracking", "contains", `{"t":[{"a":1},{"a":1,"b":2}]}`, `{"t":[{"a":1,"b":2},{"a":1}]}`, true},
		{"contains backtracking needed", "contains", `{"t":[{"a":1},{"b":2}]}`, `{"t":[{"a":1,"b":2},{"a":1}]}`, true},
		{"contains backtracking fails", "contains", `{"t":[{"a":1},{"b":2}]}`, `{"t":[{"a":1,"b":2}]}`, false},
		{"contains nested arrays", "contains", `{"t":[["x"]]}`, `{"t":[["y","x"],["z"]]}`, true},
		{"contains top level", "contains", `["a"]`, `["b","a"]`, true},
		{"contains top level miss", "contains", `["a"]`, `["b"]`, false},
		{"contains non-array got", "contains", `{"t":["a"]}`, `{"t":"a"}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rm := &RequestMatcher{Arrays: c.arrays, Body: raw(c.want)}
			req := MatchableRequest{Body: []byte(c.got)}
			assert.Equal(t, c.match, rm.Matches(req))
			assert.Equal(t, c.match, oracleMatches(rm, req))
		})
	}
}

func TestArrays_Explain(t *testing.T) {
	t.Parallel()
	rm := &RequestMatcher{Arrays: "contains", Body: raw(`{"t":["a","c"]}`)}
	assert.Equal(t, []Difference{{Field: "body.t", Reason: "mismatch", Want: raw(`["a","c"]`), Got: raw(`["a","b"]`)}},
		rm.Explain(MatchableRequest{Body: []byte(`{"t":["a","b"]}`)}))
	top := &RequestMatcher{Arrays: "contains", Body: raw(`["a"]`)}
	assert.Equal(t, []Difference{{Field: "body", Reason: "mismatch", Want: raw(`["a"]`), Got: raw(`["b"]`)}},
		top.Explain(MatchableRequest{Body: []byte(`["b"]`)}))
}

func TestArrays_IsEmptyAndEqual(t *testing.T) {
	t.Parallel()
	assert.True(t, (&RequestMatcher{Arrays: "contains"}).IsEmpty())
	a := &RequestMatcher{Arrays: "contains", Body: raw(`{"t":["a"]}`)}
	b := &RequestMatcher{Body: raw(`{"t":["a"]}`)}
	assert.False(t, requestMatcherEqual(a, b))
	assert.True(t, requestMatcherEqual(a, &RequestMatcher{Arrays: "contains", Body: raw(`{"t":["a"]}`)}))
}

func TestArrays_JSON(t *testing.T) {
	t.Parallel()
	var rm RequestMatcher
	require.NoError(t, json.Unmarshal([]byte(`{"body":{"t":["a"]},"arrays":"contains"}`), &rm))
	assert.Equal(t, "contains", rm.Arrays)
	out, err := json.Marshal(rm)
	require.NoError(t, err)
	assert.JSONEq(t, `{"body":{"t":["a"]},"arrays":"contains"}`, string(out))

	var bad RequestMatcher
	require.NoError(t, json.Unmarshal([]byte(`{"arrays":"all"}`), &bad))
	require.Error(t, bad.validateArrays())
	var wrongType RequestMatcher
	require.NoError(t, json.Unmarshal([]byte(`{"arrays":5}`), &wrongType))
	require.Error(t, wrongType.validateArrays())
	require.NoError(t, (&RequestMatcher{Arrays: "exact"}).validateArrays())
}
