package mgmtstore

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
)

func ref(s *openapi3.Schema) *openapi3.SchemaRef { return &openapi3.SchemaRef{Value: s} }

func typed(t string) *openapi3.Schema { return &openapi3.Schema{Type: &openapi3.Types{t}} }

func TestSchemaProps(t *testing.T) {
	a := typed("object")
	a.Properties = openapi3.Schemas{"a": ref(typed("string"))}
	b := typed("object")
	b.Properties = openapi3.Schemas{"b": ref(typed("string"))}
	c := typed("object")
	c.Properties = openapi3.Schemas{"c": ref(typed("string"))}
	root := &openapi3.Schema{
		Properties: openapi3.Schemas{"r": ref(typed("string"))},
		AllOf:      openapi3.SchemaRefs{ref(a)},
		OneOf:      openapi3.SchemaRefs{ref(b), ref(c)},
	}
	got := schemaProps(ref(root))
	assert.ElementsMatch(t, []string{"r", "a", "b", "c"}, keys(got))
	assert.Empty(t, schemaProps(nil))
}

func keys(m map[string]*openapi3.SchemaRef) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestMinimalValue(t *testing.T) {
	str := func(mod func(*openapi3.Schema)) *openapi3.SchemaRef {
		s := typed("string")
		mod(s)
		return ref(s)
	}
	assert.Equal(t, "", minimalValue(ref(typed("string"))))
	assert.Equal(t, "user@example.com", minimalValue(str(func(s *openapi3.Schema) { s.Format = "email" })))
	assert.Equal(t, "https://example.com", minimalValue(str(func(s *openapi3.Schema) { s.Format = "uri" })))
	assert.Equal(t, "2026-01-01T00:00:00.000Z", minimalValue(str(func(s *openapi3.Schema) { s.Format = "date-time" })))
	assert.Equal(t, "x", minimalValue(str(func(s *openapi3.Schema) { s.Enum = []any{"x", "y"} })))
	assert.Equal(t, "aaa", minimalValue(str(func(s *openapi3.Schema) { s.MinLength = 3 })))

	assert.EqualValues(t, 0, minimalValue(ref(typed("integer"))))
	lo := 5.0
	assert.EqualValues(t, 5, minimalValue(ref(&openapi3.Schema{Type: &openapi3.Types{"integer"}, Min: &lo})))
	assert.Equal(t, false, minimalValue(ref(typed("boolean"))))

	arr := typed("array")
	arr.Items = ref(typed("boolean"))
	assert.Equal(t, []any{}, minimalValue(ref(arr)))
	arr2 := typed("array")
	arr2.Items = ref(typed("boolean"))
	arr2.MinItems = 2
	assert.Equal(t, []any{false, false}, minimalValue(ref(arr2)))

	obj := typed("object")
	obj.Required = []string{"n"}
	obj.Properties = openapi3.Schemas{"n": ref(typed("string")), "opt": ref(typed("string"))}
	assert.Equal(t, map[string]any{"n": ""}, minimalValue(ref(obj)))

	one := &openapi3.Schema{OneOf: openapi3.SchemaRefs{ref(typed("boolean")), ref(typed("string"))}}
	assert.Equal(t, false, minimalValue(ref(one)))
}
