package mgmtstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"

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

// fill replaces every {param} segment of template with its value, percent-
// escaped so an id holding a slash stays one segment of a storage key.
func fill(template string, params map[string]string) string {
	segs := strings.Split(template, "/")
	for i, seg := range segs {
		if isParam(seg) {
			segs[i] = url.PathEscape(params[paramName(seg)])
		}
	}
	return strings.Join(segs, "/")
}

func notFound(w http.ResponseWriter) {
	httperr.WriteMgmt(w, http.StatusNotFound, "Not Found", "The resource does not exist.", "inexistent_resource")
}

func badBody(w http.ResponseWriter, msg string) {
	httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", msg, "invalid_body")
}

const notObjectMsg = "The request body must be a JSON object."

// project returns obj as a response body. Nested objects and array items follow
// their own response sub-schemas. A key the response schema does not declare is
// kept only where that schema allows additional properties, and never when the
// request schema (req, may be nil) declares it at the same level, as it does
// for a password. Any required property the stored object lacks is filled with
// its minimal value, so the response satisfies the spec even when the create
// request did not carry the field. A schema with no properties keeps
// everything. Recursion follows the stored data, which is finite, so a
// recursive schema cannot loop.
func project(obj Object, schema, req *openapi3.SchemaRef) Object {
	if out, ok := projectValue(obj, schema, req).(Object); ok {
		return out
	}
	return obj
}

func projectValue(v any, schema, req *openapi3.SchemaRef) any {
	if schema == nil || schema.Value == nil {
		return v
	}
	schema, req = selectBranch(schema, v, 0), selectBranch(req, v, 0)
	switch t := v.(type) {
	case map[string]any:
		return projectMap(t, schema, req)
	case []any:
		items := schema.Value.Items
		if items == nil {
			return v
		}
		var reqItems *openapi3.SchemaRef
		if req != nil && req.Value != nil {
			reqItems = req.Value.Items
		}
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = projectValue(x, items, reqItems)
		}
		return out
	}
	return v
}

func projectMap(obj map[string]any, schema, req *openapi3.SchemaRef) map[string]any {
	props := schemaProps(schema)
	allowed, extra := additionalProps(schema, 0)
	if len(props) == 0 {
		return projectFreeForm(obj, extra, req)
	}
	reqProps := schemaProps(req)
	out := make(map[string]any, len(props))
	for k, x := range obj {
		if p, ok := props[k]; ok {
			out[k] = projectValue(x, p, reqProps[k])
		} else if _, requestOnly := reqProps[k]; !requestOnly && allowed {
			out[k] = redactWriteOnly(projectValue(x, extra, nil), nil)
		}
	}
	for _, k := range collectRequired(schema, 0) {
		if _, ok := out[k]; !ok {
			if p, ok := props[k]; ok {
				out[k] = MinimalValue(p)
			}
		}
	}
	return out
}

// projectFreeForm projects an object whose read schema declares no
// properties: values follow the extra schema when there is one, and keys the
// request schema marks writeOnly are dropped, at every depth.
func projectFreeForm(obj map[string]any, extra, req *openapi3.SchemaRef) map[string]any {
	out := make(map[string]any, len(obj))
	for k, x := range obj {
		if extra != nil {
			x = projectValue(x, extra, nil)
		}
		out[k] = x
	}
	return redactMap(out, req)
}

// redactWriteOnly returns v without the keys req explicitly marks writeOnly,
// recursing through objects and arrays. Other request-declared keys stay.
// Where req is a union that v does not single out, a key is dropped only when
// every branch declaring it marks it writeOnly.
func redactWriteOnly(v any, req *openapi3.SchemaRef) any {
	if req == nil || req.Value == nil {
		return v
	}
	req = selectBranch(req, v, 0)
	switch t := v.(type) {
	case []any:
		if req.Value.Items == nil {
			return v
		}
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = redactWriteOnly(x, req.Value.Items)
		}
		return out
	case map[string]any:
		return redactMap(t, req)
	}
	return v
}

func redactMap(obj map[string]any, req *openapi3.SchemaRef) map[string]any {
	if req == nil || req.Value == nil {
		return obj
	}
	leaves := unionLeaves(selectBranch(req, obj, 0), 0)
	out := make(map[string]any, len(obj))
	for k, x := range obj {
		if writeOnlyIn(leaves, k) {
			continue
		}
		out[k] = redactWriteOnly(x, declaredBy(leaves, k))
	}
	return out
}

// writeOnlyIn reports whether some leaf declares key and every leaf that
// does marks it writeOnly.
func writeOnlyIn(leaves []*openapi3.SchemaRef, key string) bool {
	found := false
	for _, l := range leaves {
		if p, ok := schemaProps(l)[key]; ok {
			if p.Value == nil || !p.Value.WriteOnly {
				return false
			}
			found = true
		}
	}
	return found
}

func projectAll(items []Object, schema, req *openapi3.SchemaRef) []Object {
	out := make([]Object, len(items))
	for i, o := range items {
		out[i] = project(o, schema, req)
	}
	return out
}

func stampFor(schema *openapi3.SchemaRef) stamp {
	props := schemaProps(schema)
	_, c := props["created_at"]
	_, u := props["updated_at"]
	return stamp{created: c, updated: u}
}

// updateStamp is the stamp policy for updates: what a GET of the item
// returns, else what create returns, as an update's own response may omit
// the timestamps the item carries.
func updateStamp(read, create *openapi3.SchemaRef) stamp {
	if read != nil {
		return stampFor(read)
	}
	return stampFor(create)
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

// writesBody reports whether a route of kind k stores the request body.
func writesBody(k Kind) bool {
	switch k {
	case KindCreate, KindMerge, KindReplace, KindSettingsMerge, KindSettingsReplace:
		return true
	}
	return false
}

const formType = "application/x-www-form-urlencoded"

// decodeBody decodes a write body by its Content-Type: a form body through
// kin-openapi's form decoder with the operation's form schema, so values are
// typed, and anything else as JSON.
func decodeBody(r *http.Request, op spec.Operation, body []byte) (Object, bool) {
	if rb := op.Op.RequestBody; rb == nil || rb.Value == nil || (!rb.Value.Required && len(bytes.TrimSpace(body)) == 0) {
		return Object{}, true
	}
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != formType {
		return decodeObject(body)
	}
	media := op.Op.RequestBody.Value.Content.Get(formType)
	decode := openapi3filter.RegisteredBodyDecoder(formType)
	if media == nil || decode == nil {
		return nil, false
	}
	v, err := decode(bytes.NewReader(body), r.Header, media.Schema,
		func(name string) *openapi3.Encoding { return media.Encoding[name] })
	if err != nil {
		return nil, false
	}
	o, ok := v.(map[string]any)
	return o, ok && o != nil
}

func decodeObject(body []byte) (Object, bool) {
	var o Object
	if err := decodeJSON(body, &o); err != nil || o == nil {
		return nil, false
	}
	return o, true
}

// decodeJSON decodes exactly one JSON value into v, keeping numbers as
// json.Number so large integers survive a round trip, and rejects trailing
// data like json.Unmarshal does.
func decodeJSON(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("unexpected data after the JSON value")
	}
	return nil
}

// prospective returns the state a write would store: the patch applied to the
// current item or settings for a merge, the body itself otherwise.
func (s *Store) prospective(route Route, coll, id string, obj Object, ws writeSchemas) Object {
	var cur Object
	switch route.Kind {
	case KindMerge:
		o, found := s.get(coll, id)
		if !found {
			return obj
		}
		cur = o
	case KindSettingsMerge:
		initial, _ := s.engine.initial[route.Collection].(map[string]any)
		cur = s.getSettings(coll, initial)
	default:
		return obj
	}
	if cur == nil {
		cur = Object{}
	}
	for k, v := range obj {
		if route.Kind == KindMerge && (k == route.IDField || k == "created_at") {
			continue
		}
		if v == nil {
			delete(cur, k)
			continue
		}
		cur[k] = ws.carry(k, cur[k], v)
	}
	return cur
}

// decodeWrite decodes a write body and checks the state it would store.
// A failure returns the message for the 400 response.
func (s *Store) decodeWrite(r *http.Request, op spec.Operation, body []byte, route Route, coll, id string) (Object, string) {
	obj, ok := decodeBody(r, op, body)
	if !ok {
		return nil, notObjectMsg
	}
	req, resp, read := s.engine.schemas(route.Collection)
	if read == nil {
		read = resp
	}
	if err := s.checkWrite(route, coll, id, obj, req, read); err != nil {
		return nil, err.Error()
	}
	return obj, ""
}

// checkWrite validates the state a write would store against the read
// response schema, so the store never saves a value its own GET would then
// return invalid. Keys the request also declares are checked too.
// The state is projected first, as GET would, so values the response
// legitimately redacts or drops (a secret's value) are not held against it.
func (s *Store) checkWrite(route Route, coll, id string, obj Object, req, read *openapi3.SchemaRef) error {
	return checkPresent(project(s.prospective(route, coll, id, obj, writeSchemas{req, read}), read, req), read)
}

// parentOf names the parent item a route's path points at, or the zero
// parentRef when the route has none.
func parentOf(route Route, params map[string]string) parentRef {
	if route.Parent == "" {
		return parentRef{}
	}
	last := lastSegment(route.Parent)
	return parentRef{fill(strings.TrimSuffix(route.Parent, "/"+last), params), params[paramName(last)]}
}

// hasParent reports whether parent is stored; a zero parent always is.
func (s *Store) hasParent(parent parentRef) bool {
	if parent == (parentRef{}) {
		return true
	}
	_, found := s.get(parent.coll, parent.id)
	return found
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
	parent := parentOf(route, params)
	if !s.hasParent(parent) {
		notFound(w)
		return
	}
	status, schema := successResponse(op.Op)
	coll := fill(route.Collection, params)
	req, resp, read := s.engine.schemas(route.Collection)
	var obj Object
	if writesBody(route.Kind) {
		var msg string
		if obj, msg = s.decodeWrite(r, op, body, route, coll, params[route.IDParam]); msg != "" {
			badBody(w, msg)
			return
		}
	}

	switch route.Kind {
	case KindList:
		s.serveList(w, r, op, coll, status, schema, req)
	case KindCreate:
		s.serveCreate(w, route, coll, parent, status, schema, req, obj)
	case KindRead:
		o, found := s.get(coll, params[route.IDParam])
		if !found {
			notFound(w)
			return
		}
		writeJSON(w, status, schema, project(o, schema, req))
	case KindMerge, KindReplace:
		s.serveWrite(w, route, coll, params[route.IDParam], status, schema, req, obj, updateStamp(read, resp))
	case KindDelete:
		id := params[route.IDParam]
		o, found := s.get(coll, id)
		if !found || !s.remove(coll, id) {
			notFound(w)
			return
		}
		writeJSON(w, status, schema, project(o, schema, req))
	default:
		s.serveSettings(w, route, coll, parent, status, schema, req, obj)
	}
}

func (s *Store) serveList(w http.ResponseWriter, r *http.Request, op spec.Operation, coll string, status int, schema, req *openapi3.SchemaRef) {
	shape := listShapeOf(schema)
	page, itemSchema, badFrom := pageList(op, r, shape, func(win func(n int) (int, int)) (int, []Object) {
		return s.listWindow(coll, win)
	})
	if badFrom {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", "from: not a cursor issued by this mock", "invalid_query_string")
		return
	}
	writeJSON(w, status, schema, projectPage(page, itemSchema, req))
}

// projectPage projects every item inside a paged list body.
func projectPage(page any, itemSchema, req *openapi3.SchemaRef) any {
	switch p := page.(type) {
	case []Object:
		return projectAll(p, itemSchema, req)
	case map[string]any:
		for k, v := range p {
			if items, ok := v.([]Object); ok {
				p[k] = projectAll(items, itemSchema, req)
			}
		}
	}
	return page
}

// newIDFor generates an id long enough for the id property's minLength.
func newIDFor(prop *openapi3.SchemaRef) string {
	id := newID()
	if prop != nil && prop.Value != nil && prop.Value.MinLength < 1024 {
		if n := int(prop.Value.MinLength); n > len(id) {
			id += strings.Repeat("0", n-len(id))
		}
	}
	return id
}

func (s *Store) serveCreate(w http.ResponseWriter, route Route, coll string, parent parentRef, status int, schema, req *openapi3.SchemaRef, obj Object) {
	if id, _ := obj[route.IDField].(string); id == "" {
		obj[route.IDField] = newIDFor(schemaProps(schema)[route.IDField])
	}
	created, err := s.insert(coll, route.IDField, obj, stampFor(schema), false, parent)
	switch {
	case errors.Is(err, errConflict):
		id, _ := obj[route.IDField].(string)
		httperr.WriteMgmt(w, http.StatusConflict, "Conflict", fmt.Sprintf("An object with id %s already exists.", id), "already_exists")
	case errors.Is(err, errNotFound):
		notFound(w)
	case errors.Is(err, errLimit):
		httperr.WriteMgmt(w, http.StatusTooManyRequests, "Too Many Requests",
			fmt.Sprintf("mock store limit reached: %d objects in %s", s.max, coll), "too_many_entities")
	case err != nil:
		httperr.WriteMgmt(w, http.StatusInternalServerError, "Internal Server Error", err.Error(), "internal")
	default:
		writeJSON(w, status, schema, project(created, schema, req))
	}
}

func (s *Store) serveWrite(w http.ResponseWriter, route Route, coll, id string, status int, schema, req *openapi3.SchemaRef, obj Object, st stamp) {
	var (
		out Object
		err error
	)
	if route.Kind == KindMerge {
		out, err = s.merge(coll, id, route.IDField, obj, st, s.engine.writeSchemas(route.Collection))
	} else {
		out, err = s.replace(coll, id, route.IDField, obj, st)
	}
	if err != nil {
		notFound(w)
		return
	}
	writeJSON(w, status, schema, project(out, schema, req))
}

func (s *Store) serveSettings(w http.ResponseWriter, route Route, path string, parent parentRef, status int, schema, req *openapi3.SchemaRef, obj Object) {
	initial, _ := s.engine.initial[route.Collection].(map[string]any)
	var out Object
	switch route.Kind {
	case KindSettingsRead:
		out = s.getSettings(path, initial)
	case KindSettingsReset:
		s.resetSettings(path)
		out = s.getSettings(path, initial)
	default:
		var err error
		if route.Kind == KindSettingsMerge {
			out, err = s.mergeSettings(path, initial, obj, parent, s.engine.writeSchemas(route.Collection))
		} else {
			out, err = s.replaceSettings(path, obj, parent)
		}
		if err != nil {
			notFound(w)
			return
		}
	}
	if out == nil {
		out = Object{}
	}
	writeJSON(w, status, schema, project(out, schema, req))
}
