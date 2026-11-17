package mgmtstore

import (
	"fmt"
	"slices"
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

// selectBranch narrows a oneOf/anyOf schema to the branch that describes v,
// so projection and seed normalization follow the shape the value really has.
// The branch is chosen by the discriminator when the schema has one, else it
// is the first branch whose validation accepts v, else the one whose declared
// properties overlap v's keys the most. A schema with its own properties keeps
// them, with the chosen branch added as an allOf member. A schema without a
// union, or a branchless one, is returned as is.
func selectBranch(s *openapi3.SchemaRef, v any, depth int) *openapi3.SchemaRef {
	if s == nil || s.Value == nil || depth > maxSchemaDepth {
		return s
	}
	group := s.Value.OneOf
	if len(group) == 0 {
		group = s.Value.AnyOf
	}
	if len(group) == 0 {
		return s
	}
	chosen := pickBranch(s.Value, group, v)
	if chosen == nil {
		return s
	}
	chosen = selectBranch(chosen, v, depth+1)
	if len(s.Value.Properties) == 0 && len(s.Value.AllOf) == 0 && s.Value.AdditionalProperties.Schema == nil {
		return chosen
	}
	merged := *s.Value
	merged.OneOf, merged.AnyOf = nil, nil
	merged.AllOf = append(openapi3.SchemaRefs{chosen}, s.Value.AllOf...)
	return &openapi3.SchemaRef{Value: &merged}
}

// enumFit scores how well obj's values match the enum and const values of
// schema s, directly, through allOf or through nested unions. A value that contradicts them
// makes the result not ok, so the branch cannot be the one meant.
// Each matching value adds enumBoost.
func enumFit(s *openapi3.SchemaRef, obj map[string]any, depth int) (score int, ok bool) {
	if s == nil || s.Value == nil || depth > maxSchemaDepth {
		return 0, true
	}
	if score, ok = propsFit(s.Value.Properties, obj); !ok {
		return 0, false
	}
	for _, b := range s.Value.AllOf {
		n, ok := enumFit(b, obj, depth+1)
		if !ok {
			return 0, false
		}
		score += n
	}
	for _, group := range []openapi3.SchemaRefs{s.Value.OneOf, s.Value.AnyOf} {
		best, found := 0, false
		for _, b := range group {
			if n, ok := enumFit(b, obj, depth+1); ok && (!found || n > best) {
				best, found = n, true
			}
		}
		if len(group) > 0 {
			if !found {
				return 0, false
			}
			score += best
		}
	}
	return score, true
}

// propsFit checks obj's values against the enums and consts of props.
func propsFit(props map[string]*openapi3.SchemaRef, obj map[string]any) (score int, ok bool) {
	for k, p := range props {
		val, present := obj[k]
		if !present {
			continue
		}
		allowed, constrained := allowedValues(p, 0)
		if !constrained {
			continue
		}
		if !slices.ContainsFunc(allowed, func(a any) bool { return fmt.Sprint(a) == fmt.Sprint(val) }) {
			return 0, false
		}
		score += enumBoost
	}
	return score, true
}

const enumBoost = 1000

// allowedValues returns the values a property schema restricts itself to
// through enum or const, directly or through allOf, and whether it does.
func allowedValues(p *openapi3.SchemaRef, depth int) ([]any, bool) {
	if p == nil || p.Value == nil || depth > maxSchemaDepth {
		return nil, false
	}
	if len(p.Value.Enum) > 0 {
		return p.Value.Enum, true
	}
	if p.Value.Const != nil {
		return []any{p.Value.Const}, true
	}
	for _, b := range p.Value.AllOf {
		if vals, ok := allowedValues(b, depth+1); ok {
			return vals, true
		}
	}
	return nil, false
}

func pickBranch(s *openapi3.Schema, group openapi3.SchemaRefs, v any) *openapi3.SchemaRef {
	obj, _ := v.(map[string]any)
	if b := discriminated(s.Discriminator, group, obj); b != nil {
		return b
	}
	best, bestOverlap := (*openapi3.SchemaRef)(nil), -1
	for _, b := range group {
		if b == nil || b.Value == nil {
			continue
		}
		if acceptsValue(b, v) {
			return b
		}
		fit, ok := enumFit(b, obj, 0)
		if !ok {
			continue
		}
		n := fit
		for k := range schemaProps(b) {
			if _, ok := obj[k]; ok {
				n++
			}
		}
		if n > bestOverlap {
			best, bestOverlap = b, n
		}
	}
	return best
}

// acceptsValue reports whether v validates against b as a request or response.
func acceptsValue(b *openapi3.SchemaRef, v any) bool {
	for _, mode := range []openapi3.SchemaValidationOption{openapi3.VisitAsResponse(), openapi3.VisitAsRequest()} {
		if b.Value.VisitJSON(v, mode, openapi3.DisablePatternValidation(), openapi3.DisableSchemaErrorDetails()) == nil {
			return true
		}
	}
	return false
}

// discriminated finds the branch named by obj's discriminator property, by
// mapping entry or, without one, by the branch's own schema name.
func discriminated(d *openapi3.Discriminator, group openapi3.SchemaRefs, obj map[string]any) *openapi3.SchemaRef {
	if d == nil || obj == nil {
		return nil
	}
	val, ok := obj[d.PropertyName].(string)
	if !ok {
		return nil
	}
	name := val
	if m, ok := d.Mapping[val]; ok {
		name = m.Ref
	}
	name = name[strings.LastIndex(name, "/")+1:]
	for _, b := range group {
		if b != nil && b.Ref != "" && b.Ref[strings.LastIndex(b.Ref, "/")+1:] == name {
			return b
		}
	}
	return nil
}
