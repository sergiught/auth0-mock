package mgmtstore

import (
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

const maxSchemaDepth = 8

// schemaProps returns the union of declared properties across the schema's
// own properties and its allOf/oneOf/anyOf branches.
func schemaProps(s *openapi3.SchemaRef) map[string]*openapi3.SchemaRef {
	out := map[string]*openapi3.SchemaRef{}
	collectProps(s, out, 0)
	return out
}

func collectProps(s *openapi3.SchemaRef, out map[string]*openapi3.SchemaRef, depth int) {
	if s == nil || s.Value == nil || depth > maxSchemaDepth {
		return
	}
	for name, p := range s.Value.Properties {
		if _, ok := out[name]; !ok {
			out[name] = p
		}
	}
	for _, group := range []openapi3.SchemaRefs{s.Value.AllOf, s.Value.OneOf, s.Value.AnyOf} {
		for _, b := range group {
			collectProps(b, out, depth+1)
		}
	}
}

// additionalProps reports whether schema, or one of its allOf/oneOf/anyOf
// branches, allows undeclared properties (additionalProperties true or a
// schema), and the schema they follow when it gives one.
func additionalProps(s *openapi3.SchemaRef, depth int) (bool, *openapi3.SchemaRef) {
	if s == nil || s.Value == nil || depth > maxSchemaDepth {
		return false, nil
	}
	ap := s.Value.AdditionalProperties
	if ap.Schema != nil {
		return true, ap.Schema
	}
	allowed := ap.Has != nil && *ap.Has
	for _, group := range []openapi3.SchemaRefs{s.Value.AllOf, s.Value.OneOf, s.Value.AnyOf} {
		for _, b := range group {
			if ok, sch := additionalProps(b, depth+1); sch != nil {
				return true, sch
			} else if ok {
				allowed = true
			}
		}
	}
	return allowed, nil
}

// collectRequired gathers required property names across the schema and its
// allOf branches.
func collectRequired(s *openapi3.SchemaRef, depth int) []string {
	if s == nil || s.Value == nil || depth > maxSchemaDepth {
		return nil
	}
	req := append([]string(nil), s.Value.Required...)
	for _, b := range s.Value.AllOf {
		req = append(req, collectRequired(b, depth+1)...)
	}
	return req
}

// MinimalValue returns the smallest JSON value that satisfies the schema's
// required/type/enum/format/minLength constraints.
func MinimalValue(s *openapi3.SchemaRef) any {
	return minimalAt(s, 0)
}

func minimalAt(s *openapi3.SchemaRef, depth int) any {
	if s == nil || s.Value == nil || depth > maxSchemaDepth {
		return nil
	}
	v := s.Value
	if len(v.Enum) > 0 {
		return v.Enum[0]
	}
	if b := firstBranch(v); b != nil {
		return minimalAt(b, depth+1)
	}
	switch schemaType(v) {
	case "string":
		return minimalString(v)
	case "integer", "number":
		return minimalNumber(v)
	case "boolean":
		return false
	case "array":
		out := make([]any, 0, v.MinItems)
		for range v.MinItems {
			out = append(out, minimalAt(v.Items, depth+1))
		}
		return out
	case "object":
		return minimalObject(s, depth)
	}
	if len(v.AllOf) > 0 {
		return minimalAt(&openapi3.SchemaRef{Value: &openapi3.Schema{
			Type: &openapi3.Types{"object"}, AllOf: v.AllOf,
		}}, depth+1)
	}
	return nil
}

func firstBranch(v *openapi3.Schema) *openapi3.SchemaRef {
	if len(v.OneOf) > 0 {
		return v.OneOf[0]
	}
	if len(v.AnyOf) > 0 {
		return v.AnyOf[0]
	}
	return nil
}

func minimalObject(s *openapi3.SchemaRef, depth int) map[string]any {
	props := schemaProps(s)
	req := collectRequired(s, 0)
	sort.Strings(req)
	out := map[string]any{}
	for _, name := range req {
		if p, ok := props[name]; ok {
			out[name] = minimalAt(p, depth+1)
		}
	}
	return out
}

func minimalString(v *openapi3.Schema) string {
	switch v.Format {
	case "email":
		return "user@example.com"
	case "uri":
		return "https://example.com"
	case "date-time":
		return "2026-01-01T00:00:00.000Z"
	}
	return strings.Repeat("a", int(min(v.MinLength, 4096)))
}

func minimalNumber(v *openapi3.Schema) any {
	if v.Min == nil {
		return int64(0)
	}
	if schemaType(v) == "integer" {
		return int64(*v.Min)
	}
	return *v.Min
}

// schemaType returns the first non-null declared type, or "".
func schemaType(v *openapi3.Schema) string {
	if v.Type == nil {
		return ""
	}
	for _, t := range v.Type.Slice() {
		if t != "null" {
			return t
		}
	}
	return ""
}

// successResponse returns the lowest 2xx status the operation declares and
// its application/json schema (nil if that response has no JSON content).
func successResponse(op *openapi3.Operation) (int, *openapi3.SchemaRef) {
	if op == nil || op.Responses == nil {
		return 0, nil
	}
	best := 0
	for code := range op.Responses.Map() {
		n := 0
		for _, c := range code {
			if c < '0' || c > '9' {
				n = 0
				break
			}
			n = n*10 + int(c-'0')
		}
		if n >= 200 && n < 300 && (best == 0 || n < best) {
			best = n
		}
	}
	if best == 0 {
		return 0, nil
	}
	resp := op.Responses.Status(best)
	if resp == nil || resp.Value == nil {
		return best, nil
	}
	if media := resp.Value.Content.Get("application/json"); media != nil {
		return best, media.Schema
	}
	return best, nil
}
