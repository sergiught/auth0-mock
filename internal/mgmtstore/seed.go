package mgmtstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
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

// schemas returns the request schema that writes the collection or settings
// path, the response schema of its create or settings read, and the response
// schema of reading one item (collections only).
func (e *Engine) schemas(collection string) (request, response, read *openapi3.SchemaRef) {
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
// in an export) are checked against that response property; any other
// undeclared key is an error. The
// original object is stored by the caller.
func (t target) checkObject(obj Object) error {
	if t.request == nil || t.request.Value == nil {
		return nil
	}
	reqProps := schemaProps(t.request)
	filtered := obj
	if len(reqProps) > 0 {
		respProps := schemaProps(t.response)
		maps.Copy(respProps, schemaProps(t.read))
		filtered = Object{}
		for _, k := range slices.Sorted(maps.Keys(obj)) {
			if _, ok := reqProps[k]; ok {
				filtered[k] = obj[k]
			} else if rp, ok := respProps[k]; !ok {
				return fmt.Errorf("unknown key %q", k)
			} else if err := checkResponseValue(rp, obj[k]); err != nil {
				return fmt.Errorf("key %q: %w", k, err)
			}
		}
		// A required field no GET returns, such as a user's connection, is
		// missing from an export. Fill it for the check only, so an export
		// loads again.
		for _, k := range collectRequired(t.request, 0) {
			if _, has := filtered[k]; has {
				continue
			}
			if _, inResp := respProps[k]; !inResp {
				filtered[k] = MinimalValue(reqProps[k])
			}
		}
	}
	return t.request.Value.VisitJSON(filtered,
		openapi3.VisitAsRequest(), openapi3.DisablePatternValidation(), openapi3.DisableSchemaErrorDetails())
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
	path := fill(t.route.Collection, t.params) // Decoded, as Serve looks it up.
	if pc, pid, has := t.parent(); has {
		if _, found := s.get(pc, pid); !found {
			return fmt.Errorf("management.%q: parent %q is not in the seed", key, strings.TrimPrefix(pc+"/"+pid, "/"))
		}
	}
	if t.settings {
		var obj Object
		if err := json.Unmarshal(rawVal, &obj); err != nil || obj == nil {
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
	if err := json.Unmarshal(rawVal, &items); err != nil {
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

// escapePath percent-escapes each segment of a stored (decoded) path, so
// that Load, which decodes a key once, gets the stored path back.
func escapePath(p string) string {
	segs := strings.Split(p, "/")
	for i, seg := range segs {
		segs[i] = url.PathEscape(seg)
	}
	return strings.Join(segs, "/")
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
		key := escapePath(k)
		t, ok := s.engine.lookup(key)
		if !ok {
			continue
		}
		schema := t.read
		if schema == nil {
			schema = t.response
		}
		out[strings.TrimPrefix(key, "/")] = projectAll(s.list(k), schema, t.request)
	}
	for k, o := range settings {
		out[strings.TrimPrefix(escapePath(k), "/")] = o
	}
	return out
}
