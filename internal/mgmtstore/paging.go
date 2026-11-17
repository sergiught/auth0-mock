package mgmtstore

import (
	"net/http"
	"sort"
	"strconv"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/sergiught/auth0-mock/internal/spec"
)

const (
	defaultPageSize = 50
	maxPageSize     = 100
)

// wrapper is an object-shaped list variant: one array property plus
// optional paging fields.
type wrapper struct {
	key   string
	items *openapi3.SchemaRef
	props map[string]*openapi3.SchemaRef
}

func (w wrapper) has(name string) bool { _, ok := w.props[name]; return ok }

func (w wrapper) checkpoint() bool { return w.has("next") }

func (w wrapper) totals() bool { return w.has("total") || w.has("start") || w.has("limit") }

// listShape is every response form a list operation can take.
type listShape struct {
	plain      bool
	plainItems *openapi3.SchemaRef
	wrappers   []wrapper
}

func schemaVariants(s *openapi3.SchemaRef) []*openapi3.SchemaRef {
	if s == nil || s.Value == nil {
		return nil
	}
	if len(s.Value.OneOf) > 0 {
		return s.Value.OneOf
	}
	return []*openapi3.SchemaRef{s}
}

func listShapeOf(schema *openapi3.SchemaRef) listShape {
	var shape listShape
	for _, v := range schemaVariants(schema) {
		if v == nil || v.Value == nil {
			continue
		}
		if v.Value.Type.Is("array") {
			shape.plain = true
			shape.plainItems = v.Value.Items
			continue
		}
		props := schemaProps(v)
		keys := make([]string, 0, len(props))
		for k, p := range props {
			if p != nil && p.Value != nil && p.Value.Type.Is("array") {
				keys = append(keys, k)
			}
		}
		if len(keys) == 0 {
			continue
		}
		sort.Strings(keys)
		shape.wrappers = append(shape.wrappers, wrapper{
			key: keys[0], items: props[keys[0]].Value.Items, props: props,
		})
	}
	return shape
}

func (s listShape) firstWrapper(match func(wrapper) bool) (wrapper, bool) {
	for _, w := range s.wrappers {
		if match(w) {
			return w, true
		}
	}
	return wrapper{}, false
}

func declares(op spec.Operation, names ...string) bool {
	for _, p := range op.Op.Parameters {
		if p == nil || p.Value == nil || p.Value.In != "query" {
			continue
		}
		for _, n := range names {
			if p.Value.Name == n {
				return true
			}
		}
	}
	return false
}

func intParam(q map[string][]string, name string, def int) (int, bool) {
	v, ok := q[name]
	if !ok || len(v) == 0 || v[0] == "" {
		return def, true
	}
	n, err := strconv.Atoi(v[0])
	if err != nil {
		return def, false
	}
	return n, true
}

func clampSize(n int) int {
	if n <= 0 {
		return defaultPageSize
	}
	return min(n, maxPageSize)
}

// pageList shapes items for a list response; badFrom reports an invalid
// checkpoint cursor in the request.
func pageList(op spec.Operation, r *http.Request, shape listShape, items []Object) (body any, itemsSchema *openapi3.SchemaRef, badFrom bool) {
	q := r.URL.Query()
	_, hasFrom := q["from"]
	_, hasTake := q["take"]
	ckpt, hasCkpt := shape.firstWrapper(wrapper.checkpoint)
	totals, hasTotals := shape.firstWrapper(wrapper.totals)

	switch {
	case (hasFrom || hasTake) && hasCkpt:
		return checkpointPage(ckpt, q, items)
	case q.Get("include_totals") == "true" && hasTotals:
		return offsetPage(op, totals, q, items), totals.items, false
	case shape.plain:
		// Ops that declare neither page nor per_page return the whole list.
		if !declares(op, "page", "per_page") {
			return items, shape.plainItems, false
		}
		start, end, _, _ := offsetWindow(q, len(items))
		return items[start:end], shape.plainItems, false
	}
	if len(shape.wrappers) == 0 {
		return items, nil, false
	}
	w := shape.wrappers[0]
	if w.checkpoint() {
		return checkpointPage(w, q, items)
	}
	return offsetPage(op, w, q, items), w.items, false
}

func offsetWindow(q map[string][]string, n int) (start, end, page, perPage int) {
	pp, _ := intParam(q, "per_page", defaultPageSize)
	perPage = clampSize(pp)
	page, _ = intParam(q, "page", 0)
	if page < 0 {
		page = 0
	}
	// Compare before multiplying so a huge page cannot overflow int.
	start = n
	if page <= n/perPage {
		start = min(page*perPage, n)
	}
	end = min(start+perPage, n)
	return start, end, page, perPage
}

func offsetPage(op spec.Operation, w wrapper, q map[string][]string, items []Object) map[string]any {
	out := map[string]any{}
	start, end, page, perPage := 0, len(items), 0, len(items)
	// Ops that declare neither page nor per_page return the whole list.
	if declares(op, "page", "per_page") {
		start, end, page, perPage = offsetWindow(q, len(items))
	}
	out[w.key] = items[start:end]
	fields := map[string]int{
		"start": start, "limit": perPage, "length": end - start,
		"total": len(items), "page": page, "per_page": perPage,
	}
	for name, v := range fields {
		if w.has(name) {
			out[name] = v
		}
	}
	return out
}

func checkpointPage(w wrapper, q map[string][]string, items []Object) (map[string]any, *openapi3.SchemaRef, bool) {
	take, _ := intParam(q, "take", defaultPageSize)
	take = clampSize(take)
	start := 0
	if v := q["from"]; len(v) > 0 {
		n, err := strconv.Atoi(v[0])
		if err != nil || n < 0 || strconv.Itoa(n) != v[0] {
			return nil, nil, true
		}
		start = n
	}
	start = min(start, len(items))
	end := min(start+take, len(items))
	out := map[string]any{w.key: items[start:end]}
	if end < len(items) {
		out["next"] = strconv.Itoa(end)
	}
	return out, w.items, false
}
