package mgmtstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/spec"
)

// Handles reports whether the store answers op.
func (s *Store) Handles(op spec.Operation) bool {
	r, ok := s.engine.Route(op)
	return ok && r.Kind != KindNone
}

// pathParams matches escapedPath against template and returns the path
// parameters, each percent-decoded. The bool is false on a mismatch.
func pathParams(template, escapedPath string) (map[string]string, bool) {
	ts := strings.Split(template, "/")
	ps := strings.Split(escapedPath, "/")
	if len(ts) != len(ps) {
		return nil, false
	}
	out := map[string]string{}
	for i, t := range ts {
		if isParam(t) {
			v, err := url.PathUnescape(ps[i])
			if err != nil {
				return nil, false
			}
			out[paramName(t)] = v
			continue
		}
		if t != ps[i] {
			return nil, false
		}
	}
	return out, true
}

// fill replaces every {param} segment of template with its value.
func fill(template string, params map[string]string) string {
	segs := strings.Split(template, "/")
	for i, seg := range segs {
		if isParam(seg) {
			segs[i] = params[paramName(seg)]
		}
	}
	return strings.Join(segs, "/")
}

func notFound(w http.ResponseWriter) {
	httperr.WriteMgmt(w, http.StatusNotFound, "Not Found", "The resource does not exist.", "inexistent_resource")
}

func badBody(w http.ResponseWriter) {
	httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", "The request body must be a JSON object.", "invalid_body")
}

// project keeps only the properties schema declares; a schema with no
// properties keeps everything.
func project(obj Object, schema *openapi3.SchemaRef) Object {
	props := schemaProps(schema)
	if len(props) == 0 {
		return obj
	}
	out := make(Object, len(props))
	for k, v := range obj {
		if _, ok := props[k]; ok {
			out[k] = v
		}
	}
	return out
}

func projectAll(items []Object, schema *openapi3.SchemaRef) []Object {
	out := make([]Object, len(items))
	for i, o := range items {
		out[i] = project(o, schema)
	}
	return out
}

func stampFor(schema *openapi3.SchemaRef) stamp {
	props := schemaProps(schema)
	_, c := props["created_at"]
	_, u := props["updated_at"]
	return stamp{created: c, updated: u}
}

func writeJSON(w http.ResponseWriter, status int, schema *openapi3.SchemaRef, v any) {
	if schema == nil {
		w.WriteHeader(status)
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		httperr.WriteMgmt(w, http.StatusInternalServerError, "Internal Server Error", "could not encode response", "internal")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

func decodeObject(body []byte) (Object, bool) {
	var o Object
	if err := json.Unmarshal(body, &o); err != nil || o == nil {
		return nil, false
	}
	return o, true
}

// Serve answers r, matched to op, from the store. The body argument is
// the already-read, already-validated request body.
func (s *Store) Serve(w http.ResponseWriter, r *http.Request, op spec.Operation, body []byte) {
	route, ok := s.engine.Route(op)
	if !ok || route.Kind == KindNone {
		notFound(w)
		return
	}
	params, ok := pathParams(op.Template, r.URL.EscapedPath())
	if !ok {
		notFound(w)
		return
	}
	if route.Parent != "" {
		parentColl := strings.TrimSuffix(route.Parent, "/"+lastSegment(route.Parent))
		parentID := params[paramName(lastSegment(route.Parent))]
		if _, found := s.get(fill(parentColl, params), parentID); !found {
			notFound(w)
			return
		}
	}
	status, schema := successResponse(op.Op)
	coll := fill(route.Collection, params)

	switch route.Kind {
	case KindList:
		s.serveList(w, r, op, coll, status, schema)
	case KindCreate:
		s.serveCreate(w, route, coll, status, schema, body)
	case KindRead:
		o, found := s.get(coll, params[route.IDParam])
		if !found {
			notFound(w)
			return
		}
		writeJSON(w, status, schema, project(o, schema))
	case KindMerge, KindReplace:
		s.serveWrite(w, route, coll, params[route.IDParam], status, schema, body)
	case KindDelete:
		id := params[route.IDParam]
		o, found := s.get(coll, id)
		if !found || !s.remove(coll, id) {
			notFound(w)
			return
		}
		writeJSON(w, status, schema, project(o, schema))
	default:
		s.serveSettings(w, route, coll, status, schema, body)
	}
}

func (s *Store) serveList(w http.ResponseWriter, r *http.Request, op spec.Operation, coll string, status int, schema *openapi3.SchemaRef) {
	items := s.list(coll)
	shape := listShapeOf(schema)
	page, itemSchema, badFrom := pageList(op, r, shape, items)
	if badFrom {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", "from: not a cursor issued by this mock", "invalid_query_string")
		return
	}
	writeJSON(w, status, schema, projectPage(page, itemSchema))
}

// projectPage projects every item inside a paged list body.
func projectPage(page any, itemSchema *openapi3.SchemaRef) any {
	switch p := page.(type) {
	case []Object:
		return projectAll(p, itemSchema)
	case map[string]any:
		for k, v := range p {
			if items, ok := v.([]Object); ok {
				p[k] = projectAll(items, itemSchema)
			}
		}
	}
	return page
}

func (s *Store) serveCreate(w http.ResponseWriter, route Route, coll string, status int, schema *openapi3.SchemaRef, body []byte) {
	obj, ok := decodeObject(body)
	if !ok {
		badBody(w)
		return
	}
	created, err := s.create(coll, route.IDField, obj, stampFor(schema))
	switch {
	case errors.Is(err, errConflict):
		id, _ := obj[route.IDField].(string)
		httperr.WriteMgmt(w, http.StatusConflict, "Conflict", fmt.Sprintf("An object with id %s already exists.", id), "already_exists")
	case errors.Is(err, errLimit):
		httperr.WriteMgmt(w, http.StatusTooManyRequests, "Too Many Requests",
			fmt.Sprintf("mock store limit reached: %d objects in %s", s.max, coll), "too_many_entities")
	case err != nil:
		httperr.WriteMgmt(w, http.StatusInternalServerError, "Internal Server Error", err.Error(), "internal")
	default:
		writeJSON(w, status, schema, project(created, schema))
	}
}

func (s *Store) serveWrite(w http.ResponseWriter, route Route, coll, id string, status int, schema *openapi3.SchemaRef, body []byte) {
	obj, ok := decodeObject(body)
	if !ok {
		badBody(w)
		return
	}
	var (
		out Object
		err error
	)
	if route.Kind == KindMerge {
		out, err = s.merge(coll, id, route.IDField, obj, stampFor(schema))
	} else {
		out, err = s.replace(coll, id, route.IDField, obj, stampFor(schema))
	}
	if err != nil {
		notFound(w)
		return
	}
	writeJSON(w, status, schema, project(out, schema))
}

func (s *Store) serveSettings(w http.ResponseWriter, route Route, path string, status int, schema *openapi3.SchemaRef, body []byte) {
	initial, _ := s.engine.initial[route.Collection].(map[string]any)
	var out Object
	switch route.Kind {
	case KindSettingsRead:
		out = s.getSettings(path, initial)
	case KindSettingsReset:
		s.resetSettings(path)
		out = s.getSettings(path, initial)
	default:
		obj, ok := decodeObject(body)
		if !ok {
			badBody(w)
			return
		}
		if route.Kind == KindSettingsMerge {
			out = s.mergeSettings(path, initial, obj)
		} else {
			out = s.replaceSettings(path, obj)
		}
	}
	if out == nil {
		out = Object{}
	}
	writeJSON(w, status, schema, project(out, schema))
}
