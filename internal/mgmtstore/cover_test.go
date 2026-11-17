package mgmtstore

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func schemaOf(s *openapi3.Schema) *openapi3.SchemaRef { return &openapi3.SchemaRef{Value: s} }

func objectSchema(props map[string]*openapi3.SchemaRef, required ...string) *openapi3.Schema {
	return &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: props, Required: required}
}

func TestServeMissingItems(t *testing.T) {
	h := newHarness(t)
	for _, m := range []string{"GET", "PATCH", "DELETE"} {
		body := ""
		if m == "PATCH" {
			body = `{"description":"d"}`
		}
		code, errBody, _ := h.do(m, "/api/v2/roles/nope", "", body)
		assert.Equal(t, 404, code, m)
		assert.Equal(t, "inexistent_resource", errBody["errorCode"], m)
	}
}

func TestServeSettingsResetAndEmptyRead(t *testing.T) {
	h := newHarness(t)
	code, _, raw := h.do("GET", "/api/v2/keys/custom-signing", "", "")
	require.Equal(t, 200, code, string(raw))
	code, _, raw = h.do("DELETE", "/api/v2/keys/custom-signing", "", "")
	assert.Less(t, code, 300, string(raw))
	code, _, raw = h.do("PUT", "/api/v2/keys/custom-signing", "", `{"keys":[]}`)
	assert.Equal(t, 200, code, string(raw))
	_, got, _ := h.do("GET", "/api/v2/keys/custom-signing", "", "")
	assert.Equal(t, []any{}, got["keys"])
}

func TestServeSettingsPutAndBadForm(t *testing.T) {
	h := newHarness(t)
	code, _, raw := h.do("PUT", "/api/v2/prompts/login/custom-text/en", "", `{}`)
	assert.Equal(t, 200, code, string(raw))

	// A form body with a bad percent escape does not decode.
	op, err := h.v.Resolve("POST", "/api/v2/roles")
	require.NoError(t, err)
	r := httptest.NewRequest("POST", "/api/v2/roles", strings.NewReader("name=%zz"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.s.Serve(w, r, op, []byte("name=%zz"))
	assert.Equal(t, 400, w.Code)
}

func TestPathParamsMismatch(t *testing.T) {
	_, ok := pathParams("/a/{id}", "/a/%zz")
	assert.False(t, ok)
	_, ok = pathParams("/a/{id}", "/b/x")
	assert.False(t, ok)
	_, ok = pathParams("/a/{id}", "/a/x/y")
	assert.False(t, ok)
	p, ok := pathParams("/a/{id}", "/a/x%7Cy")
	assert.True(t, ok)
	assert.Equal(t, map[string]string{"id": "x|y"}, p)
}

func TestWriteJSONEncodeFailure(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, 200, schemaOf(&openapi3.Schema{}), make(chan int))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	w = httptest.NewRecorder()
	writeJSON(w, 204, nil, nil)
	assert.Equal(t, 204, w.Code)
	assert.Empty(t, w.Body.String())
}

func TestNewIDForHonoursMinLength(t *testing.T) {
	assert.Len(t, newIDFor(schemaOf(&openapi3.Schema{MinLength: 40})), 40)
	assert.Len(t, newIDFor(nil), 24)
}

func TestProjectNestedRules(t *testing.T) {
	str := schemaOf(&openapi3.Schema{Type: &openapi3.Types{"string"}})
	inner := schemaOf(objectSchema(map[string]*openapi3.SchemaRef{"name": str}, "name"))
	list := schemaOf(&openapi3.Schema{Type: &openapi3.Types{"array"}, Items: inner})
	yes := true
	free := objectSchema(nil)
	free.AdditionalProperties = openapi3.AdditionalProperties{Has: &yes}
	typed := objectSchema(nil)
	typed.AdditionalProperties = openapi3.AdditionalProperties{Schema: inner}
	resp := schemaOf(objectSchema(map[string]*openapi3.SchemaRef{
		"list": list, "free": schemaOf(free), "typed": schemaOf(typed), "plain": inner,
	}))
	resp.Value.AdditionalProperties = openapi3.AdditionalProperties{Has: &yes}
	req := schemaOf(objectSchema(map[string]*openapi3.SchemaRef{"secret": str, "list": list}))

	got := project(Object{
		"list":   []any{map[string]any{"name": "a", "value": "hidden"}, "scalar"},
		"free":   map[string]any{"any": 1},
		"typed":  map[string]any{"k": map[string]any{"name": "n", "value": "hidden"}},
		"plain":  map[string]any{},
		"secret": "x",
		"extra":  1,
	}, resp, req)
	assert.Equal(t, Object{
		"list":  []any{map[string]any{"name": "a"}, "scalar"},
		"free":  map[string]any{"any": 1},
		"typed": map[string]any{"k": map[string]any{"name": "n"}},
		"plain": map[string]any{"name": ""}, // Required nested field is filled.
		"extra": 1,
	}, got)

	// A schema without properties keeps the object; one with only an
	// additionalProperties schema projects the values; a non-object value
	// passes through.
	assert.Equal(t, Object{"a": 1}, project(Object{"a": 1}, schemaOf(objectSchema(nil)), nil))
	assert.Equal(t, Object{"k": map[string]any{"name": "n"}},
		project(Object{"k": map[string]any{"name": "n", "x": 1}}, schemaOf(typed), nil))
	assert.Equal(t, "s", projectValue("s", inner, nil))
	assert.Equal(t, []any{1}, projectValue([]any{1}, schemaOf(&openapi3.Schema{}), nil))
	assert.Equal(t, Object{"a": 1}, project(Object{"a": 1}, nil, nil))
}

func TestAdditionalPropsAcrossBranches(t *testing.T) {
	yes, no := true, false
	ok, sch := additionalProps(nil, 0)
	assert.False(t, ok)
	assert.Nil(t, sch)

	branchTrue := objectSchema(nil)
	branchTrue.AdditionalProperties = openapi3.AdditionalProperties{Has: &yes}
	ok, sch = additionalProps(schemaOf(&openapi3.Schema{OneOf: openapi3.SchemaRefs{schemaOf(branchTrue)}}), 0)
	assert.True(t, ok)
	assert.Nil(t, sch)

	typedBranch := objectSchema(nil)
	typedBranch.AdditionalProperties = openapi3.AdditionalProperties{Schema: schemaOf(objectSchema(nil))}
	ok, sch = additionalProps(schemaOf(&openapi3.Schema{AllOf: openapi3.SchemaRefs{schemaOf(branchTrue), schemaOf(typedBranch)}}), 0)
	assert.True(t, ok)
	assert.NotNil(t, sch)

	closed := objectSchema(nil)
	closed.AdditionalProperties = openapi3.AdditionalProperties{Has: &no}
	ok, _ = additionalProps(schemaOf(closed), 0)
	assert.False(t, ok)
}

func TestMinimalValueShapes(t *testing.T) {
	str := schemaOf(&openapi3.Schema{Type: &openapi3.Types{"string"}})
	one := 1.0
	assert.Equal(t, "", MinimalValue(str))
	assert.Equal(t, int64(1), MinimalValue(schemaOf(&openapi3.Schema{Type: &openapi3.Types{"integer"}, Min: &one})))
	assert.InDelta(t, 1.0, MinimalValue(schemaOf(&openapi3.Schema{Type: &openapi3.Types{"number"}, Min: &one})), 0)
	assert.Equal(t, map[string]any{"a": ""},
		MinimalValue(schemaOf(&openapi3.Schema{AllOf: openapi3.SchemaRefs{schemaOf(objectSchema(map[string]*openapi3.SchemaRef{"a": str}, "a"))}})))
	assert.Equal(t, "", MinimalValue(schemaOf(&openapi3.Schema{AnyOf: openapi3.SchemaRefs{str}})))
	assert.Nil(t, MinimalValue(schemaOf(&openapi3.Schema{})))
	assert.Equal(t, []string{"b"},
		collectRequired(schemaOf(&openapi3.Schema{AllOf: openapi3.SchemaRefs{schemaOf(objectSchema(nil, "b"))}}), 0))
	assert.Nil(t, collectRequired(nil, 0))
	assert.Equal(t, "", schemaType(&openapi3.Schema{}))
	assert.Equal(t, "", schemaType(&openapi3.Schema{Type: &openapi3.Types{"null"}}))
}

func TestSuccessResponseEdges(t *testing.T) {
	code, sch := successResponse(nil)
	assert.Zero(t, code)
	assert.Nil(t, sch)
	op := openapi3.NewOperation()
	op.Responses = openapi3.NewResponses()
	code, sch = successResponse(op)
	assert.Zero(t, code)
	assert.Nil(t, sch)
	op.Responses.Set("default", &openapi3.ResponseRef{Value: openapi3.NewResponse()})
	op.Responses.Set("204", &openapi3.ResponseRef{Value: openapi3.NewResponse()})
	code, sch = successResponse(op)
	assert.Equal(t, 204, code)
	assert.Nil(t, sch)
}

func TestPagingHelpers(t *testing.T) {
	n, ok := intParam(map[string][]string{"page": {"x"}}, "page", 7)
	assert.Equal(t, 7, n)
	assert.False(t, ok)
	n, ok = intParam(map[string][]string{"page": {""}}, "page", 7)
	assert.Equal(t, 7, n)
	assert.True(t, ok)
	assert.Equal(t, defaultPageSize, clampSize(0))
	assert.Equal(t, maxPageSize, clampSize(maxPageSize+1))

	start, end, page, _ := offsetWindow(map[string][]string{"page": {"-3"}}, 5)
	assert.Equal(t, []int{0, 5, 0}, []int{start, end, page})
	start, end, _, _ = offsetWindow(map[string][]string{"page": {"99999999999"}, "per_page": {"2"}}, 5)
	assert.Equal(t, []int{5, 5}, []int{start, end})

	assert.Nil(t, schemaVariants(nil))
	assert.Empty(t, listShapeOf(nil).wrappers)
	assert.Empty(t, listShapeOf(schemaOf(&openapi3.Schema{OneOf: openapi3.SchemaRefs{nil, schemaOf(objectSchema(nil))}})).wrappers)
}

func TestPagingNoPageParamsReturnsWholeList(t *testing.T) {
	h := newHarness(t)
	for i := range 3 {
		code, _, raw := h.do("POST", "/api/v2/roles", "", `{"name":"r`+string(rune('a'+i))+`"}`)
		require.Equal(t, 200, code, string(raw))
	}
	_, _, raw := h.do("GET", "/api/v2/roles", "page=-1&per_page=-1", "")
	assert.Equal(t, 3, strings.Count(string(raw), `"name"`))
}

func TestKindStringAndRoutes(t *testing.T) {
	assert.Equal(t, "stub", Kind(-1).String())
	assert.Equal(t, "stub", Kind(999).String())
	e, _ := loadEngine(t)
	routes := e.Routes()
	require.NotEmpty(t, routes)
	for i := 1; i < len(routes); i++ {
		a, b := routes[i-1], routes[i]
		assert.True(t, a.Template < b.Template || (a.Template == b.Template && a.Method <= b.Method))
	}
}

func TestStoreEdges(t *testing.T) {
	s := newTestStore(t)
	assert.False(t, s.remove("/none", "x"))
	_, err := s.create("/c", "id", Object{"id": "1"}, both)
	require.NoError(t, err)
	assert.False(t, s.remove("/c", "x"))
	_, err = s.replace("/c", "nope", "id", Object{}, both)
	assert.ErrorIs(t, err, errNotFound)
	got, err := s.replace("/c", "1", "id", nil, both)
	require.NoError(t, err)
	assert.Equal(t, "1", got["id"])
	assert.Nil(t, copyObject(nil))
	assert.Equal(t, Object{}, s.replaceSettings0("/s", nil))
	assert.Equal(t, Object{"a": 1}, mergeInto(s, Object{"a": 1}))
}

func (s *Store) replaceSettings0(path string, o Object) Object {
	out, _ := s.replaceSettings(path, o, parentRef{})
	return out
}

func mergeInto(s *Store, patch Object) Object {
	out, _ := s.mergeSettings("/m", nil, patch, parentRef{})
	return out
}

func TestNormalizeSeedBranches(t *testing.T) {
	str := schemaOf(&openapi3.Schema{Type: &openapi3.Types{"string"}})
	yes := true
	// Response allows extras that must be strings.
	resp := objectSchema(map[string]*openapi3.SchemaRef{"id": str})
	resp.AdditionalProperties = openapi3.AdditionalProperties{Schema: str}
	req := objectSchema(map[string]*openapi3.SchemaRef{"id": str})
	_, err := normalizeSeed(schemaOf(req), []*openapi3.SchemaRef{schemaOf(resp)}, Object{"id": "a", "x": "ok"}, 0)
	require.NoError(t, err)
	_, err = normalizeSeed(schemaOf(req), []*openapi3.SchemaRef{schemaOf(resp)}, Object{"id": "a", "x": 5}, 0)
	require.ErrorContains(t, err, `key "x"`)

	// Nested object and array item errors carry their path.
	nestedReq := schemaOf(objectSchema(map[string]*openapi3.SchemaRef{
		"o": schemaOf(req),
		"a": schemaOf(&openapi3.Schema{Type: &openapi3.Types{"array"}, Items: schemaOf(req)}),
		"n": schemaOf(&openapi3.Schema{Type: &openapi3.Types{"array"}}),
	}))
	nestedResp := schemaOf(objectSchema(map[string]*openapi3.SchemaRef{
		"o": schemaOf(objectSchema(map[string]*openapi3.SchemaRef{"id": str})),
		"a": schemaOf(&openapi3.Schema{Type: &openapi3.Types{"array"}, Items: schemaOf(objectSchema(map[string]*openapi3.SchemaRef{"id": str}))}),
	}))
	resps := []*openapi3.SchemaRef{nestedResp}
	_, err = normalizeSeed(nestedReq, resps, Object{"o": Object{"id": "a", "bad": 1}}, 0)
	require.ErrorContains(t, err, `key "o"`)
	_, err = normalizeSeed(nestedReq, resps, Object{"a": []any{Object{"id": "a", "bad": 1}}}, 0)
	require.ErrorContains(t, err, "item 0")
	// An array without items schema passes through.
	out, err := normalizeSeed(nestedReq, resps, Object{"n": []any{1}}, 0)
	require.NoError(t, err)
	assert.Equal(t, Object{"n": []any{1}}, out)

	// AdditionalProperties true allows undeclared keys.
	resp.AdditionalProperties = openapi3.AdditionalProperties{Has: &yes}
	_, err = normalizeSeed(schemaOf(req), []*openapi3.SchemaRef{schemaOf(resp)}, Object{"id": "a", "x": 5}, 0)
	require.NoError(t, err)

	// CheckObject surfaces normalization errors.
	err = target{request: schemaOf(req), response: schemaOf(objectSchema(map[string]*openapi3.SchemaRef{"id": str}))}.checkObject(Object{"zzz": 1})
	require.ErrorContains(t, err, "unknown key")
}

func TestProspectiveAndCheckPresentBranches(t *testing.T) {
	h := newHarness(t)
	_, _, raw := h.do("POST", "/api/v2/roles", "", `{"name":"r"}`)
	var role map[string]any
	require.NoError(t, json.Unmarshal(raw, &role))
	id := role["id"].(string)
	route := Route{Kind: KindMerge, IDField: "id"}
	got := h.s.prospective(route, "/roles", id, Object{"id": "other", "created_at": "x", "name": "n", "description": nil})
	assert.Equal(t, id, got["id"])
	assert.Equal(t, "n", got["name"])
	assert.NotContains(t, got, "description")

	// A settings path with no stored value or initial value starts empty.
	got = h.s.prospective(Route{Kind: KindSettingsMerge, Collection: "/nowhere"}, "/nowhere", "", Object{"a": 1})
	assert.Equal(t, Object{"a": 1}, got)

	// A present null is validated like any value; a write drops it first.
	assert.Error(t, checkPresent(Object{"name": nil}, schemaOf(objectSchema(map[string]*openapi3.SchemaRef{"name": schemaOf(&openapi3.Schema{Type: &openapi3.Types{"string"}})}))))
}
