package mgmtstore

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/spec"
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

func TestOffsetPageExplicitZeroPerPage(t *testing.T) {
	h := newHarness(t)
	for _, n := range []string{"u1", "u2", "u3"} {
		code, _, _ := h.do("POST", "/api/v2/users", "", `{"user_id":"`+n+`","connection":"x"}`)
		require.Equal(t, 201, code)
	}
	_, body, _ := h.do("GET", "/api/v2/users", "include_totals=true&per_page=0", "")
	assert.Empty(t, body["users"])
	assert.EqualValues(t, 3, body["total"])
	assert.EqualValues(t, 0, body["limit"])
	assert.EqualValues(t, 0, body["length"])
	// An omitted or empty per_page still takes the default.
	_, body, _ = h.do("GET", "/api/v2/users", "include_totals=true&per_page=", "")
	assert.EqualValues(t, 50, body["limit"])
	// A zero page size never divides, whatever the page.
	start, end, _, perPage := offsetWindow(map[string][]string{"per_page": {"0"}, "page": {"7"}}, 3)
	assert.Equal(t, []int{0, 0, 0}, []int{start, end, perPage})
}

// An operation that declares no paging parameters, and a shape with no
// array to page, return the whole collection.
func TestPageListWholeWhenNotPaged(t *testing.T) {
	h := threeRoles(t)
	fetch := func(win func(n int) (int, int)) (int, []Object) { return h.s.listWindow("/roles", win) }
	op := spec.Operation{Op: &openapi3.Operation{}}
	r := httptest.NewRequest("GET", "/?per_page=1", nil)

	body, _, bad := pageList(op, r, listShape{plain: true}, fetch)
	assert.False(t, bad)
	assert.Len(t, body, 3)

	body, schema, _ := pageList(op, r, listShape{}, fetch)
	assert.Len(t, body, 3)
	assert.Nil(t, schema)

	w := wrapper{key: "roles", props: map[string]*openapi3.SchemaRef{"total": nil, "start": nil, "limit": nil}}
	page := offsetPage(op, w, r.URL.Query(), fetch)
	assert.Len(t, page["roles"], 3)
	assert.Equal(t, 3, page["total"])
	assert.Equal(t, 3, page["limit"])

	total, items := h.s.listWindow("/missing", wholeList)
	assert.Zero(t, total)
	assert.Empty(t, items)
}

func TestUpdateStampFallsBackToCreateResponse(t *testing.T) {
	withBoth := schemaOf(&openapi3.Schema{Properties: openapi3.Schemas{
		"created_at": schemaOf(&openapi3.Schema{}), "updated_at": schemaOf(&openapi3.Schema{}),
	}})
	none := schemaOf(&openapi3.Schema{})
	assert.Equal(t, both, updateStamp(nil, withBoth))
	assert.Equal(t, stamp{}, updateStamp(none, withBoth))
}
