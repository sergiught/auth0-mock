package matches

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

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
	for _, src := range []string{`{"arrays":5}`, `{"arrays":null}`, `{"arrays":""}`, `{"body":{"t":["a"]},"arrays":null}`} {
		var wrong RequestMatcher
		require.NoError(t, json.Unmarshal([]byte(src), &wrong))
		require.Error(t, wrong.validateArrays(), src)
	}
	var omitted RequestMatcher
	require.NoError(t, json.Unmarshal([]byte(`{"body":{"t":["a"]}}`), &omitted))
	require.NoError(t, omitted.validateArrays())
	require.NoError(t, (&RequestMatcher{Arrays: "exact"}).validateArrays())
}

func TestArrayContains_FailsFastOnLargeArrays(t *testing.T) {
	t.Parallel()
	want := make([]any, 0, 11)
	for range 10 {
		want = append(want, "a")
	}
	want = append(want, "b")
	got := make([]any, 20)
	for i := range got {
		got[i] = "a"
	}
	done := make(chan bool, 1)
	go func() { done <- arrayContains(want, got, newContainsBudget()) }()
	select {
	case ok := <-done:
		assert.False(t, ok)
	case <-time.After(2 * time.Second):
		t.Fatal("arrayContains did not finish within 2s")
	}
}

func TestArrayContains_PairsAroundGreedyChoice(t *testing.T) {
	t.Parallel()
	want := []any{map[string]any{"a": 1.0}, map[string]any{"a": 1.0, "b": 2.0}}
	got := []any{map[string]any{"a": 1.0, "b": 2.0}, map[string]any{"a": 1.0}}
	assert.True(t, arrayContains(want, got, newContainsBudget()))
}

func TestArrayContains_DenseDuplicateScalarsAreCounted(t *testing.T) {
	t.Parallel()
	want := make([]any, 250_000)
	got := make([]any, 250_000)
	for i := range want {
		want[i], got[i] = "a", "a"
	}
	assert.True(t, arrayContains(want[:10_000], got[:20_000], newContainsBudget()))
	assert.False(t, arrayContains(append(want[:10_000:10_000], "b"), got, newContainsBudget()))
}

func TestArrayContains_DenseDuplicateObjectsStopAtBudget(t *testing.T) {
	t.Parallel()
	want := make([]any, 10_000)
	got := make([]any, 10_000)
	for i := range want {
		want[i], got[i] = map[string]any{"a": 1.0}, map[string]any{"a": 1.0}
	}
	done := make(chan bool, 1)
	go func() { done <- arrayContains(want, got, newContainsBudget()) }()
	select {
	case ok := <-done:
		assert.False(t, ok, "over-budget match gives up as no match")
	case <-time.After(2 * time.Second):
		t.Fatal("arrayContains did not stop within 2s")
	}
}

func TestArrayContains_MixesScalarsAndObjects(t *testing.T) {
	t.Parallel()
	want := []any{"a", map[string]any{"id": 1.0}, "a", nil}
	got := []any{map[string]any{"id": 1.0, "x": true}, "a", nil, "b", "a"}
	assert.True(t, arrayContains(want, got, newContainsBudget()))
	assert.False(t, arrayContains(append(want, "a"), got, newContainsBudget()))
	assert.False(t, arrayContains([]any{map[string]any{}}, []any{"a"}, newContainsBudget()))
}

func TestArrayContains_CandidatePairsBuildNoDiagnostics(t *testing.T) {
	// Not parallel: testing.AllocsPerRun panics in a parallel test.
	want := []any{map[string]any{"big": strings.Repeat("x", 500<<10)}}
	got := make([]any, 10_000)
	for i := range got {
		got[i] = map[string]any{}
	}
	allocs := testing.AllocsPerRun(1, func() { arrayContains(want, got, newContainsBudget()) })
	assert.Less(t, allocs, 100.0, "candidate pairs must not serialize mismatches")
}

func TestArrayContains_ObjectKeysSpendBudgetInOrder(t *testing.T) {
	t.Parallel()
	// Each decoy fails on "a" for 2 units, but costs about 800 units when
	// "z" is checked first. With 2,000 decoys only the sorted order stays
	// under the budget, so a random key order would sometimes miss.
	z := make([]any, 400)
	for i := range z {
		z[i] = float64(i)
	}
	want := []any{map[string]any{"a": 1.0, "z": z}}
	got := make([]any, 0, 2_001)
	for range 2_000 {
		got = append(got, map[string]any{"a": 2.0, "z": z})
	}
	got = append(got, map[string]any{"a": 1.0, "z": z})
	for range 20 {
		require.True(t, arrayContains(want, got, newContainsBudget()))
	}
}

func TestArrays_ExplainSpendsBudgetInKeyOrder(t *testing.T) {
	t.Parallel()
	// Each of the eight lists fits alone, but the budget covers only three,
	// so it runs out on whichever comes fourth. Sorted keys make the
	// failing set always d to h, whatever order the map yields.
	list := "[0" + strings.Repeat(",0", 14_000-1) + "]"
	var parts []string
	for _, k := range strings.Split("abcdefgh", "") {
		parts = append(parts, `"`+k+`":`+list)
	}
	body := []byte("{" + strings.Join(parts, ",") + "}")
	rm := &RequestMatcher{Arrays: "contains", Body: body}
	for range 5 {
		var fields []string
		for _, d := range rm.Explain(MatchableRequest{Body: body}) {
			fields = append(fields, d.Field)
		}
		require.Equal(t, []string{"body.d", "body.e", "body.f", "body.g", "body.h"}, fields)
	}
}

func TestArrayContains_WideObjectsSpendBudgetPerKey(t *testing.T) {
	t.Parallel()
	wide := make(map[string]any, 10_000)
	for i := range 10_000 {
		wide["k"+strconv.Itoa(i)] = 1.0
	}
	got := make([]any, 20_000)
	for i := range got {
		got[i] = map[string]any{}
	}
	done := make(chan bool, 1)
	go func() { done <- arrayContains([]any{wide}, got, newContainsBudget()) }()
	select {
	case ok := <-done:
		assert.False(t, ok)
	case <-time.After(2 * time.Second):
		t.Fatal("preparing the keys of a wide object was not charged to the budget")
	}
}
