package mgmtstore

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func threeRoles(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	for _, n := range []string{"a", "b", "c"} {
		code, _, _ := h.do("POST", "/api/v2/roles", "", `{"name":"`+n+`"}`)
		require.Equal(t, 200, code)
	}
	return h
}

func names(t *testing.T, v any) []string {
	t.Helper()
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(map[string]any)["name"].(string))
	}
	return out
}

func TestPagingPlain(t *testing.T) {
	h := threeRoles(t)
	_, _, raw := h.do("GET", "/api/v2/roles", "per_page=2&page=1", "")
	var arr []any
	require.NoError(t, json.Unmarshal(raw, &arr))
	assert.Equal(t, []string{"c"}, names(t, arr))
}

func TestPagingTotals(t *testing.T) {
	h := threeRoles(t)
	_, body, _ := h.do("GET", "/api/v2/roles", "include_totals=true&per_page=2", "")
	assert.EqualValues(t, 0, body["start"])
	assert.EqualValues(t, 2, body["limit"])
	assert.EqualValues(t, 3, body["total"])
	assert.Equal(t, []string{"a", "b"}, names(t, body["roles"]))
}

// users declares length in its totals wrapper.
func TestPagingTotalsLength(t *testing.T) {
	h := newHarness(t)
	for _, n := range []string{"u1", "u2", "u3"} {
		code, _, _ := h.do("POST", "/api/v2/users", "", `{"user_id":"`+n+`","connection":"x"}`)
		require.Equal(t, 201, code)
	}
	_, body, _ := h.do("GET", "/api/v2/users", "include_totals=true&per_page=2", "")
	assert.EqualValues(t, 2, body["length"])
	assert.EqualValues(t, 3, body["total"])
}

// actions/actions declares page and per_page in its object-only wrapper.
func TestPagingPageFields(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 3; i++ {
		_, err := h.s.create("/actions/actions", "id", Object{"name": "n"}, stamp{})
		require.NoError(t, err)
	}
	_, body, _ := h.do("GET", "/api/v2/actions/actions", "per_page=2&page=1", "")
	assert.EqualValues(t, 1, body["page"])
	assert.EqualValues(t, 2, body["per_page"])
	assert.EqualValues(t, 3, body["total"])
	assert.Len(t, body["actions"], 1)
}

func TestPagingHugePage(t *testing.T) {
	h := threeRoles(t)
	code, _, raw := h.do("GET", "/api/v2/roles", "page=9223372036854775807&per_page=100", "")
	assert.Equal(t, 200, code)
	assert.JSONEq(t, `[]`, string(raw))

	code, body, _ := h.do("GET", "/api/v2/roles", "include_totals=true&page=9223372036854775807&per_page=100", "")
	assert.Equal(t, 200, code)
	assert.Empty(t, body["roles"])
	assert.GreaterOrEqual(t, body["start"], float64(0))
}

func TestPagingCheckpoint(t *testing.T) {
	h := threeRoles(t)
	_, body, _ := h.do("GET", "/api/v2/roles", "take=2", "")
	assert.Equal(t, []string{"a", "b"}, names(t, body["roles"]))
	assert.Equal(t, "2", body["next"])

	_, body, _ = h.do("GET", "/api/v2/roles", "from=2&take=2", "")
	assert.Equal(t, []string{"c"}, names(t, body["roles"]))
	assert.NotContains(t, body, "next")
}

func TestPagingBadFrom(t *testing.T) {
	h := threeRoles(t)
	code, body, _ := h.do("GET", "/api/v2/roles", "from=zzz", "")
	assert.Equal(t, 400, code)
	assert.Equal(t, "invalid_query_string", body["errorCode"])
	assert.Equal(t, "from: not a cursor issued by this mock", body["message"])
}

func TestPagingClamp(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 101; i++ {
		_, err := h.s.create("/roles", "id", Object{"name": "n"}, stamp{})
		require.NoError(t, err)
	}
	_, _, raw := h.do("GET", "/api/v2/roles", "per_page=500", "")
	var arr []any
	require.NoError(t, json.Unmarshal(raw, &arr))
	assert.Len(t, arr, 100)
}

func TestPagingObjectOnly(t *testing.T) {
	h := newHarness(t)
	_, body, _ := h.do("GET", "/api/v2/actions/actions", "", "")
	assert.Contains(t, body, "actions")
	assert.EqualValues(t, 0, body["total"])
}
