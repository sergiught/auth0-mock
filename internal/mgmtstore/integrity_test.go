package mgmtstore

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const eventBridgeStream = `{"name":"eb","destination":{"type":"eventbridge","configuration":{"aws_account_id":"123456789012","aws_region":"us-east-1"}}}`

func TestEventBridgeStreamKeepsItsDestination(t *testing.T) {
	h := newHarness(t)
	code, created, _ := h.do("POST", "/api/v2/event-streams", "", eventBridgeStream)
	require.Equal(t, 201, code)
	id, _ := created["id"].(string)

	check := func(dest any) {
		t.Helper()
		d, _ := dest.(map[string]any)
		assert.Equal(t, "eventbridge", d["type"])
		cfg, _ := d["configuration"].(map[string]any)
		assert.Equal(t, "123456789012", cfg["aws_account_id"])
		assert.Equal(t, "us-east-1", cfg["aws_region"])
		assert.NotContains(t, cfg, "webhook_endpoint")
		assert.NotContains(t, cfg, "webhook_authorization")
	}
	check(created["destination"])
	_, got, _ := h.do("GET", "/api/v2/event-streams/"+id, "", "")
	check(got["destination"])
	_, list, _ := h.do("GET", "/api/v2/event-streams", "", "")
	items, _ := list["eventStreams"].([]any)
	require.Len(t, items, 1)
	check(items[0].(map[string]any)["destination"])
	exported, ok := h.s.Export()["event-streams"].([]Object)
	require.True(t, ok)
	require.Len(t, exported, 1)
	check(exported[0]["destination"])
}

func TestSelectBranch(t *testing.T) {
	str := schemaOf(&openapi3.Schema{Type: &openapi3.Types{"string"}})
	a := &openapi3.SchemaRef{Ref: "#/components/schemas/A", Value: objectSchema(map[string]*openapi3.SchemaRef{"a": str, "shared": str}, "a")}
	b := &openapi3.SchemaRef{Ref: "#/components/schemas/B", Value: objectSchema(map[string]*openapi3.SchemaRef{"b": str, "shared": str}, "b")}
	a.Value.AdditionalProperties = openapi3.AdditionalProperties{Has: new(false)}
	b.Value.AdditionalProperties = openapi3.AdditionalProperties{Has: new(false)}
	union := schemaOf(&openapi3.Schema{OneOf: openapi3.SchemaRefs{nil, a, b}})

	// First accepting branch.
	assert.Same(t, b, selectBranch(union, map[string]any{"b": "x"}, 0))
	// None accepts: the most overlapping keys win.
	assert.Same(t, b, selectBranch(union, map[string]any{"b": 1, "shared": "s", "zz": 1}, 0))
	// A non-object value with no accepting branch falls back to a declared branch.
	assert.NotNil(t, selectBranch(union, 5, 0))
	// A schema with no union, or no usable branch, is returned as is.
	assert.Same(t, a, selectBranch(a, map[string]any{}, 0))
	empty := schemaOf(&openapi3.Schema{AnyOf: openapi3.SchemaRefs{nil}})
	assert.Same(t, empty, selectBranch(empty, map[string]any{}, 0))
	assert.Nil(t, selectBranch(nil, nil, 0))
	assert.Same(t, union, selectBranch(union, nil, maxSchemaDepth+1))

	// Discriminator by implicit name and by mapping, ahead of validation.
	union.Value.Discriminator = &openapi3.Discriminator{PropertyName: "kind"}
	assert.Same(t, b, selectBranch(union, map[string]any{"kind": "B", "a": "x"}, 0))
	union.Value.Discriminator.Mapping = map[string]openapi3.MappingRef{"first": {Ref: "#/components/schemas/A"}}
	assert.Same(t, a, selectBranch(union, map[string]any{"kind": "first", "b": "x"}, 0))
	// An unknown discriminator value falls back to validation.
	assert.Same(t, b, selectBranch(union, map[string]any{"kind": "nope", "b": "x"}, 0))

	// A schema with its own properties keeps them and gains the branch.
	own := schemaOf(&openapi3.Schema{Properties: map[string]*openapi3.SchemaRef{"id": str}, AnyOf: openapi3.SchemaRefs{a, b}})
	props := schemaProps(selectBranch(own, map[string]any{"b": "x"}, 0))
	assert.Contains(t, props, "id")
	assert.Contains(t, props, "b")
	assert.NotContains(t, props, "a")
}

func TestLargeNumbersSurviveRoundTrip(t *testing.T) {
	const big = "9007199254740993"
	h := newHarness(t)
	code, _, body := h.do("POST", "/api/v2/users", "", `{"connection":"c","user_id":"auth0|big","user_metadata":{"n":`+big+`}}`)
	require.Equal(t, 201, code)
	assert.Contains(t, string(body), `"n":`+big)
	_, _, body = h.do("GET", "/api/v2/users/"+url.PathEscape("auth0|big"), "", "")
	assert.Contains(t, string(body), `"n":`+big)

	exported, err := json.Marshal(h.s.Export())
	require.NoError(t, err)
	assert.Contains(t, string(exported), `"n":`+big)

	var data map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(exported, &data))
	e, _ := loadEngine(t)
	s2 := e.NewStore(fixedNow)
	require.NoError(t, s2.Load(data))
	again, err := json.Marshal(s2.Export())
	require.NoError(t, err)
	assert.Contains(t, string(again), `"n":`+big)
}

func TestDecodeJSONRejectsTrailingData(t *testing.T) {
	var o Object
	require.NoError(t, decodeJSON([]byte(`{"a":1} `), &o))
	require.Error(t, decodeJSON([]byte(`{"a":1} {"b":2}`), &o))
	require.Error(t, decodeJSON([]byte(`{"a":`), &o))
	_, ok := decodeObject([]byte(`{"a":1}x`))
	assert.False(t, ok)
}

func TestWritesRejectBadResponseTypedFields(t *testing.T) {
	h := newHarness(t)
	code, created, _ := h.do("POST", "/api/v2/users", "", `{"connection":"c","user_id":"auth0|p","logins_count":2}`)
	require.Equal(t, 201, code)
	path := "/api/v2/users/" + url.PathEscape(created["user_id"].(string))

	code, errBody, _ := h.do("PATCH", path, "", `{"logins_count":"many"}`)
	assert.Equal(t, 400, code)
	assert.Equal(t, "invalid_body", errBody["errorCode"])
	_, got, _ := h.do("GET", path, "", "")
	assert.EqualValues(t, 2, got["logins_count"])

	code, errBody, _ = h.do("POST", "/api/v2/users", "", `{"connection":"c","logins_count":"many"}`)
	assert.Equal(t, 400, code)
	assert.Equal(t, "invalid_body", errBody["errorCode"])

	// A valid value and a null (delete) still pass.
	code, patched, _ := h.do("PATCH", path, "", `{"logins_count":7}`)
	assert.Equal(t, 200, code)
	assert.EqualValues(t, 7, patched["logins_count"])
	code, _, _ = h.do("PATCH", path, "", `{"logins_count":null}`)
	assert.Equal(t, 200, code)
}

func TestSeedValidatesPathParams(t *testing.T) {
	e, _ := loadEngine(t)
	body := `{"login":{"pageTitle":"x"}}`
	err := e.NewStore(fixedNow).Load(raw(t, `{"prompts/login/custom-text/not-a-language":`+body+`}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompts/login/custom-text/not-a-language")
	assert.Contains(t, err.Error(), `"language"`)
	require.NoError(t, e.NewStore(fixedNow).Load(raw(t, `{"prompts/login/custom-text/en":`+body+`}`)))
}
