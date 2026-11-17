package mgmtstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
)

// target is what a seed key (a path under /api/v2) maps to.
type target struct {
	route    Route
	params   map[string]string
	settings bool
	// Request validates a seed object, response is what a write returns, and
	// read is what a GET of one item returns (collections only).
	request, response, read *openapi3.SchemaRef
}

func bodySchema(op *openapi3.Operation) *openapi3.SchemaRef {
	if op == nil || op.RequestBody == nil || op.RequestBody.Value == nil {
		return nil
	}
	if media := op.RequestBody.Value.Content.Get("application/json"); media != nil {
		return media.Schema
	}
	return nil
}

// match finds the collection-create or settings route whose template fits
// path, preferring the template with the fewest parameters.
func (e *Engine) match(path string) (Route, map[string]string, bool) {
	var best Route
	var params map[string]string
	found := false
	for _, op := range e.ops {
		r, ok := e.routes[routeKey(op.Method, op.Template)]
		if !ok || (r.Kind != KindCreate && r.Kind != KindSettingsMerge && r.Kind != KindSettingsReplace) {
			continue
		}
		p, ok := pathParams(r.Collection, path)
		if !ok {
			continue
		}
		if !found || strings.Count(r.Collection, "{") < strings.Count(best.Collection, "{") {
			best, params, found = r, p, true
		}
	}
	return best, params, found
}

// lookup maps a path such as "/organizations/org_1/invitations" to the
// collection or settings route that stores it.
func (e *Engine) lookup(path string) (target, bool) {
	best, params, ok := e.match(path)
	if !ok {
		return target{}, false
	}
	t := target{route: best, params: params, settings: best.Kind != KindCreate}
	t.request, t.response, t.read = e.schemas(best.Collection)
	return t, true
}

// checkPathParams validates the decoded path parameters of a seed key against
// the parameter schemas of the operations that serve its route, so a key GET
// and PUT would reject (an unknown prompt language) is rejected up front.
func (e *Engine) checkPathParams(t target) error {
	for _, op := range e.ops {
		if r, ok := e.routes[routeKey(op.Method, op.Template)]; !ok || r.Collection != t.route.Collection {
			continue
		}
		for _, p := range op.Op.Parameters {
			val, has := t.params[p.Value.Name]
			if p.Value.In != "path" || !has || p.Value.Schema == nil || p.Value.Schema.Value == nil {
				continue
			}
			if err := p.Value.Schema.Value.VisitJSON(val, openapi3.DisablePatternValidation(), openapi3.DisableSchemaErrorDetails()); err != nil {
				return fmt.Errorf("path parameter %q: %q is not valid", p.Value.Name, val)
			}
		}
	}
	return nil
}

// schemas returns the request schema that writes the collection or settings
// path, the response schema of its create or settings read, and the response
// schema of reading one item (collections only).
func (e *Engine) schemas(collection string) (request, response, read *openapi3.SchemaRef) {
	set := e.schemaCache[collection]
	return set.request, set.response, set.read
}

// schemaSet is the cached result of scanSchemas for one collection.
type schemaSet struct{ request, response, read *openapi3.SchemaRef }

// cacheSchemas scans the operations once per collection, so schemas never
// rescans them. The cache is immutable once NewEngine returns.
func (e *Engine) cacheSchemas() {
	e.schemaCache = map[string]schemaSet{}
	for _, r := range e.routes {
		if _, done := e.schemaCache[r.Collection]; !done {
			req, resp, read := e.scanSchemas(r.Collection)
			e.schemaCache[r.Collection] = schemaSet{req, resp, read}
		}
	}
}

// literalAt reports whether the stored key matches a route whose segment at
// index idx (counting the empty segment before the leading slash) is a
// literal, so it is a sibling route rather than a child of an item there.
func (e *Engine) literalAt(key string, idx int) bool {
	r, _, ok := e.match(key)
	if !ok {
		return false
	}
	segs := strings.Split(r.Collection, "/")
	return idx < len(segs) && !isParam(segs[idx])
}

func (e *Engine) scanSchemas(collection string) (request, response, read *openapi3.SchemaRef) {
	var putBody *openapi3.SchemaRef
	for _, op := range e.ops {
		r, ok := e.routes[routeKey(op.Method, op.Template)]
		if !ok || r.Collection != collection {
			continue
		}
		_, resp := successResponse(op.Op)
		switch r.Kind {
		case KindCreate:
			request, response = bodySchema(op.Op), resp
		case KindRead:
			read = resp
		case KindSettingsRead:
			response = resp
		case KindSettingsMerge:
			request = bodySchema(op.Op)
		case KindSettingsReplace:
			putBody = bodySchema(op.Op)
		}
	}
	if request == nil {
		request = putBody
	}
	return request, response, read
}

// checkObject validates one seed object against the request schema. Keys the
// request schema lacks but the response schema declares (such as logins_count
// in an export) are checked against that response property and dropped from
// the validated copy, at every nesting level. A key neither schema declares is
// accepted when the response schema allows additionalProperties (checked
// against that schema when it gives one), otherwise it is an error. The
// original object is stored by the caller, untouched.
func (t target) checkObject(obj Object) error {
	if err := t.checkRequest(obj); err != nil {
		return err
	}
	// The object is stored as given, so it must also read back valid: check it
	// as GET would project it, as the write path does.
	read := t.read
	if read == nil {
		read = t.response
	}
	return checkPresent(project(obj, read, t.request), read)
}

// checkRequest validates obj against the request schema (see checkObject).
func (t target) checkRequest(obj Object) error {
	if t.request == nil || t.request.Value == nil {
		// No request schema to validate against: check the supplied keys
		// against the response schemas instead. Generated and required response
		// fields may be missing, so only the keys present are checked.
		return checkPresent(obj, t.response, t.read)
	}
	filtered := any(obj)
	if len(schemaProps(t.request)) > 0 {
		var err error
		if filtered, err = normalizeSeed(t.request, []*openapi3.SchemaRef{t.response, t.read}, obj, 0); err != nil {
			return err
		}
	}
	return t.request.Value.VisitJSON(filtered,
		openapi3.VisitAsRequest(), openapi3.DisablePatternValidation(), openapi3.DisableSchemaErrorDetails())
}

// normalizeSeed returns a copy of v shaped for request validation: response
// only keys are validated against resps and dropped, response-allowed extras
// are dropped, and required request fields GET never returns (a user's
// connection, an action secret's value) get a minimal value. A value with no
// response counterpart passes through unchanged.
func normalizeSeed(req *openapi3.SchemaRef, resps []*openapi3.SchemaRef, v any, depth int) (any, error) {
	if req == nil || req.Value == nil || depth > maxSchemaDepth {
		return v, nil
	}
	req = selectBranch(req, withoutResponseOnly(req, resps, v), 0)
	resps = slices.Clone(resps)
	for i, r := range resps {
		resps[i] = selectBranch(r, v, 0)
	}
	switch val := v.(type) {
	case []any:
		if req.Value.Items == nil {
			return v, nil
		}
		var itemResps []*openapi3.SchemaRef
		for _, r := range resps {
			if r != nil && r.Value != nil {
				itemResps = append(itemResps, r.Value.Items)
			}
		}
		out := make([]any, len(val))
		for i, item := range val {
			n, err := normalizeSeed(req.Value.Items, itemResps, item, depth+1)
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
			out[i] = n
		}
		return out, nil
	case map[string]any:
		return normalizeObject(req, resps, val, depth)
	}
	return v, nil
}

// withoutResponseOnly returns v without the keys that only the response
// schemas declare, so picking a request branch is not thrown off by them.
func withoutResponseOnly(req *openapi3.SchemaRef, resps []*openapi3.SchemaRef, v any) any {
	obj, ok := v.(map[string]any)
	if !ok {
		return v
	}
	reqProps := schemaProps(req)
	respProps, _, _ := responseShape(resps)
	out := make(map[string]any, len(obj))
	for k, val := range obj {
		_, inReq := reqProps[k]
		if _, inResp := respProps[k]; inResp && !inReq {
			continue
		}
		out[k] = val
	}
	return out
}

// responseShape merges what the response schemas declare: their properties
// and whether (and how) they allow undeclared keys.
func responseShape(resps []*openapi3.SchemaRef) (props map[string]*openapi3.SchemaRef, extras bool, extraSchema *openapi3.SchemaRef) {
	props = map[string]*openapi3.SchemaRef{}
	for _, r := range resps {
		maps.Copy(props, schemaProps(r))
		if ok, sch := additionalProps(r, 0); ok {
			extras = true
			if extraSchema == nil {
				extraSchema = sch
			}
		}
	}
	return props, extras, extraSchema
}

func normalizeObject(req *openapi3.SchemaRef, resps []*openapi3.SchemaRef, obj Object, depth int) (any, error) {
	reqProps := schemaProps(req)
	respProps, extras, extraSchema := responseShape(resps)
	if len(reqProps) == 0 || (len(respProps) == 0 && !extras) {
		return obj, nil
	}
	filtered := Object{}
	for _, k := range slices.Sorted(maps.Keys(obj)) {
		if rq, ok := reqProps[k]; ok {
			var childResps []*openapi3.SchemaRef
			if rp, ok := respProps[k]; ok {
				childResps = []*openapi3.SchemaRef{rp}
			}
			n, err := normalizeSeed(rq, childResps, obj[k], depth+1)
			if err != nil {
				return nil, fmt.Errorf("key %q: %w", k, err)
			}
			filtered[k] = n
			continue
		}
		rp, declared := respProps[k]
		if !declared && !extras {
			return nil, fmt.Errorf("unknown key %q", k)
		}
		if !declared {
			rp = extraSchema
		}
		if err := checkResponseValue(rp, obj[k]); err != nil {
			return nil, fmt.Errorf("key %q: %w", k, err)
		}
	}
	for _, k := range collectRequired(req, 0) {
		if _, has := filtered[k]; has {
			continue
		}
		if _, inResp := respProps[k]; !inResp {
			filtered[k] = MinimalValue(reqProps[k])
		}
	}
	return filtered, nil
}

// checkPresent validates each non-null top-level key of state that a response
// schema declares against that property, with full nested validation. Keys no
// schema declares and absent required keys are not errors. The first schema
// declaring a key wins.
func checkPresent(state Object, resps ...*openapi3.SchemaRef) error {
	var propSets []map[string]*openapi3.SchemaRef
	for _, r := range resps {
		propSets = append(propSets, schemaProps(selectBranch(r, state, 0)))
	}
	for _, k := range slices.Sorted(maps.Keys(state)) {
		for _, props := range propSets {
			if p, ok := props[k]; ok {
				if err := checkResponseValue(p, state[k]); err != nil {
					return fmt.Errorf("key %q: %w", k, err)
				}
				break
			}
		}
	}
	return nil
}

func checkResponseValue(prop *openapi3.SchemaRef, v any) error {
	if prop == nil || prop.Value == nil {
		return nil
	}
	return prop.Value.VisitJSON(v,
		openapi3.VisitAsResponse(), openapi3.DisablePatternValidation(), openapi3.DisableSchemaErrorDetails())
}

func (t target) parent() (coll, id string, ok bool) {
	if t.route.Parent == "" {
		return "", "", false
	}
	last := lastSegment(t.route.Parent)
	return fill(strings.TrimSuffix(t.route.Parent, "/"+last), t.params), t.params[paramName(last)], true
}

// Load fills the store from seed data: keys are paths under /api/v2 without
// the leading slash, a collection takes an array of objects and a settings
// path takes an object. Objects go through the create path; ids and
// timestamps the seed carries are kept.
//
// Load replaces the store's contents with the seed and is all-or-nothing:
// the seed is built in a scratch store and swapped in only when every key
// loaded, so on error the store is unchanged. Callers wipe first anyway.
func (s *Store) Load(data map[string]json.RawMessage) error {
	scratch := s.engine.NewStore(s.now)
	scratch.max = s.max
	if err := scratch.loadAll(data); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.colls, s.settings = scratch.colls, scratch.settings
	return nil
}

func (s *Store) loadAll(data map[string]json.RawMessage) error {
	keys := slices.Sorted(maps.Keys(data))
	sort.SliceStable(keys, func(i, j int) bool {
		return strings.Count(keys[i], "/") < strings.Count(keys[j], "/")
	})
	for _, key := range keys {
		if err := s.loadKey(key, data[key]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) loadKey(key string, rawVal json.RawMessage) error {
	t, ok := s.engine.lookup("/" + key)
	if !ok {
		return fmt.Errorf("management.%q: not a stored collection or settings path", key)
	}
	if err := s.engine.checkPathParams(t); err != nil {
		return fmt.Errorf("management.%q: %w", key, err)
	}
	path := fill(t.route.Collection, t.params) // Escaped, as Serve looks it up.
	if pc, pid, has := t.parent(); has {
		if _, found := s.get(pc, pid); !found {
			return fmt.Errorf("management.%q: parent %q is not in the seed", key, strings.TrimPrefix(pc+"/"+pid, "/"))
		}
	}
	if t.settings {
		var obj Object
		if err := decodeJSON(rawVal, &obj); err != nil || obj == nil {
			return fmt.Errorf("management.%q: must be an object", key)
		}
		// An empty snapshot is what a null patch leaves behind, and a PATCH
		// schema may require a property, so only a non-empty one is checked.
		if len(obj) > 0 {
			if err := t.checkObject(obj); err != nil {
				return fmt.Errorf("management.%s: %w", key, err)
			}
		}
		_, _ = s.replaceSettings(path, obj, parentRef{}) // Parent checked above.
		return nil
	}
	var items []Object
	if err := decodeJSON(rawVal, &items); err != nil || items == nil {
		return fmt.Errorf("management.%q: must be an array of objects", key)
	}
	for i, obj := range items {
		if err := s.loadItem(path, t, obj); err != nil {
			return fmt.Errorf("management.%s[%d]: %w", key, i, err)
		}
	}
	return nil
}

// loadItem checks one seed object and adds it through the create path.
func (s *Store) loadItem(path string, t target, obj Object) error {
	if obj == nil {
		return errors.New("must be an object")
	}
	if err := t.checkObject(obj); err != nil {
		return err
	}
	if id, _ := obj[t.route.IDField].(string); id == "" {
		obj[t.route.IDField] = newIDFor(schemaProps(t.response)[t.route.IDField])
	}
	_, err := s.insert(path, t.route.IDField, obj, stampFor(t.response), true, parentRef{})
	switch {
	case errors.Is(err, errConflict):
		return fmt.Errorf("duplicate id %v", obj[t.route.IDField])
	case errors.Is(err, errLimit):
		return fmt.Errorf("%d objects is the most %s holds", s.max, path)
	}
	return err
}

// CheckSeed reports the error Load would return for data, without touching
// any store.
func (e *Engine) CheckSeed(data map[string]json.RawMessage) error {
	return e.NewStore(time.Now).loadAll(data)
}

// CheckSeed reports the error Load would return for data, without changing
// this store.
func (s *Store) CheckSeed(data map[string]json.RawMessage) error {
	return s.engine.CheckSeed(data)
}

// Export returns the stored data in seed shape: every non-empty collection as
// an array of objects projected like a GET response, and every written
// settings path as stored.
func (s *Store) Export() map[string]any {
	s.mu.Lock()
	colls := make([]string, 0, len(s.colls))
	for k, c := range s.colls {
		if len(c.order) > 0 {
			colls = append(colls, k)
		}
	}
	settings := make(map[string]Object, len(s.settings))
	for k, o := range s.settings {
		settings[k] = copyObject(o)
	}
	s.mu.Unlock()

	out := map[string]any{}
	for _, k := range colls {
		t, ok := s.engine.lookup(k) // Stored keys are already escaped.
		if !ok {
			continue
		}
		schema := t.read
		if schema == nil {
			schema = t.response
		}
		out[strings.TrimPrefix(k, "/")] = projectAll(s.list(k), schema, t.request)
	}
	for k, o := range settings {
		if t, ok := s.engine.lookup(k); ok {
			o = project(o, t.response, t.request)
		}
		out[strings.TrimPrefix(k, "/")] = o
	}
	return out
}
