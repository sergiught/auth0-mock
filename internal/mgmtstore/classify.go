// Package mgmtstore classifies Management API operations by whether an
// in-memory store can answer them.
package mgmtstore

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/sergiught/auth0-mock/internal/spec"
)

// Kind is how the store answers an operation.
type Kind int

// The Kind values name how the store answers an operation; KindNone means
// stub-only.
const (
	KindNone Kind = iota
	KindList
	KindCreate
	KindRead
	KindMerge
	KindReplace
	KindDelete
	KindSettingsRead
	KindSettingsMerge
	KindSettingsReplace
	KindSettingsReset
)

var kindNames = [...]string{
	"stub", "list", "create", "read", "merge", "replace", "delete",
	"settings-read", "settings-merge", "settings-replace", "settings-reset",
}

// String returns the kind's name as used in Describe.
func (k Kind) String() string {
	if k < 0 || int(k) >= len(kindNames) {
		return "stub"
	}
	return kindNames[k]
}

// Route is the store's answer for one operation. Templates have no "/api/v2".
type Route struct {
	Kind Kind
	// Collection is the collection (or settings) template, e.g. "/organizations/{id}/invitations".
	Collection string
	// Parent is the parent item template, e.g. "/organizations/{id}"; empty if top level or settings.
	Parent string
	// IDParam is the item path parameter, e.g. "invitation_id"; empty for list/create/settings.
	IDParam string
	// IDField is the body field holding the id, e.g. "user_id"; empty for settings.
	IDField string
}

// settingsDenyList holds settings-shaped paths that are really relations.
var settingsDenyList = map[string]bool{
	// Sessions and refresh tokens are records created by logins, so settings
	// semantics would invent one for any id where Auth0 returns 404.
	"/sessions/{id}":                         true,
	"/refresh-tokens/{id}":                   true,
	"/actions/triggers/{triggerId}/bindings": true,
}

// collectionDenyList holds collection templates the store must not handle.
var collectionDenyList = map[string]bool{
	// The spec's name pattern uses a lookahead the validator cannot compile, so
	// every create body is rejected with 400 before the store sees it.
	"/experimentation/feature-flags": true,
	"/experimentation/segments":      true,
	"/experimentation/experiments":   true, // Same lookahead pattern on name.
	// The theme URL patterns use lookaheads the validator cannot compile, so
	// every create body is rejected with 400.
	"/branding/themes": true,
	// The spec's response requires "credentials" but declares no such
	// property under additionalProperties:false, so no body can satisfy it.
	"/branding/phone/providers": true,
}

// Engine holds the classification of every operation in a spec.
type Engine struct {
	basePath string
	ops      []spec.Operation
	// Routes is keyed by "METHOD full-template".
	routes map[string]Route
	// Initial maps a settings template to its initial value.
	initial map[string]any
	// Readable holds the handled item templates whose GET is a read.
	readable map[string]bool
	// CandColls maps collection candidates to their child item template.
	candColls map[string]string
	// CandItems holds the child item templates of collection candidates.
	candItems map[string]itemInfo
}

func routeKey(method, template string) string { return method + " " + template }

// NewEngine classifies every operation in s.
func NewEngine(s *spec.Spec, v *spec.Validator) *Engine {
	e := &Engine{
		basePath: s.BasePath,
		routes:   map[string]Route{},
		initial:  map[string]any{},
	}
	paths := map[string]map[string]spec.Operation{}
	for op := range s.Operations() {
		e.ops = append(e.ops, op)
		p := strings.TrimPrefix(op.Template, s.BasePath)
		if paths[p] == nil {
			paths[p] = map[string]spec.Operation{}
		}
		paths[p][op.Method] = op
	}
	e.classifyCollections(paths)
	e.classifySettings(paths, v)
	return e
}

// Route returns the store route for op; the bool is false for stub-only.
func (e *Engine) Route(op spec.Operation) (Route, bool) {
	r, ok := e.routes[routeKey(op.Method, op.Template)]
	return r, ok
}

// Describe returns one line per spec operation, sorted by template then method.
func (e *Engine) Describe() []string {
	ops := append([]spec.Operation(nil), e.ops...)
	sort.Slice(ops, func(i, j int) bool {
		if ops[i].Template != ops[j].Template {
			return ops[i].Template < ops[j].Template
		}
		return ops[i].Method < ops[j].Method
	})
	lines := make([]string, 0, len(ops))
	for _, op := range ops {
		r := e.routes[routeKey(op.Method, op.Template)]
		line := op.Method + "\t" + op.Template + "\t" + r.Kind.String()
		if r.IDField != "" {
			line += "\tid=" + r.IDField
		}
		if r.Parent != "" {
			line += "\tparent=" + r.Parent
		}
		lines = append(lines, line)
	}
	return lines
}

// RouteInfo is a store route with the method and full template it serves.
type RouteInfo struct {
	Method   string
	Template string // Includes the spec's base path, like spec.Operation.Template.
	Route
}

// Routes returns every handled operation's route, sorted by template then method.
func (e *Engine) Routes() []RouteInfo {
	out := make([]RouteInfo, 0, len(e.routes))
	for _, op := range e.ops {
		if r, ok := e.routes[routeKey(op.Method, op.Template)]; ok {
			out = append(out, RouteInfo{Method: op.Method, Template: op.Template, Route: r})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Template != out[j].Template {
			return out[i].Template < out[j].Template
		}
		return out[i].Method < out[j].Method
	})
	return out
}

func lastSegment(p string) string { return p[strings.LastIndex(p, "/")+1:] }

func isParam(seg string) bool { return strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") }

func paramName(seg string) string { return strings.Trim(seg, "{}") }

func segCount(p string) int { return strings.Count(p, "/") }

type itemInfo struct{ collection string }

// collectionCandidates finds each collection C (literal last segment, POST,
// and a child C/{p} with a GET/PATCH/PUT/DELETE) and its child item template.
// The second return value maps every such child template to its collection,
// whether or not the collection is later accepted.
func collectionCandidates(paths map[string]map[string]spec.Operation) (map[string]string, map[string]itemInfo) {
	cands := map[string]string{} // Collection to child item template.
	items := map[string]itemInfo{}
	for p := range paths {
		seg := lastSegment(p)
		if !isParam(seg) {
			continue
		}
		c := strings.TrimSuffix(p, "/"+seg)
		if c == "" || isParam(lastSegment(c)) {
			continue
		}
		if _, ok := paths[c]["POST"]; !ok {
			continue
		}
		m := paths[p]
		if m["GET"].Op == nil && m["PATCH"].Op == nil && m["PUT"].Op == nil && m["DELETE"].Op == nil {
			continue
		}
		if prev, ok := cands[c]; !ok || p < prev {
			cands[c] = p
		}
		items[p] = itemInfo{collection: c}
	}
	return cands, items
}

func (e *Engine) set(method, collection string, r Route) {
	e.routes[routeKey(method, e.basePath+collection)] = r
}

func (e *Engine) classifyCollections(paths map[string]map[string]spec.Operation) {
	cands, items := collectionCandidates(paths)
	e.candColls, e.candItems = cands, items
	order := make([]string, 0, len(cands))
	for c := range cands {
		order = append(order, c)
	}
	sort.Slice(order, func(i, j int) bool {
		if segCount(order[i]) != segCount(order[j]) {
			return segCount(order[i]) < segCount(order[j])
		}
		return order[i] < order[j]
	})

	readable := map[string]bool{}
	e.readable = readable
	for _, c := range order {
		if collectionDenyList[c] {
			continue
		}
		item := cands[c]
		parent := ""
		parentOf := strings.TrimSuffix(c, "/"+lastSegment(c))
		if parentOf != "" {
			switch {
			case isParam(lastSegment(parentOf)):
				if !readable[parentOf] {
					continue
				}
				parent = parentOf
			case strings.Contains(parentOf, "{"):
				continue
			}
		}
		itemSeg := lastSegment(item)
		idParam := paramName(itemSeg)
		idField := idFieldFor(paths[c]["POST"].Op, c, idParam)
		if idField == "" {
			continue
		}
		coll := Route{Collection: c, Parent: parent}
		list, create := coll, coll
		list.Kind, create.Kind = KindList, KindCreate
		create.IDField = idField
		if paths[c]["GET"].Op != nil {
			list.IDField = idField
			e.set("GET", c, list)
		}
		create.IDField = idField
		e.set("POST", c, create)

		for method, kind := range map[string]Kind{
			"GET": KindRead, "PATCH": KindMerge, "PUT": KindReplace, "DELETE": KindDelete,
		} {
			if paths[item][method].Op == nil {
				continue
			}
			e.set(method, item, Route{Kind: kind, Collection: c, Parent: parent, IDParam: idParam, IDField: idField})
			if kind == KindRead {
				readable[item] = true
			}
		}
	}
}

// idFieldFor picks the body field holding the id from the create response.
func idFieldFor(create *openapi3.Operation, collection, idParam string) string {
	_, schema := successResponse(create)
	props := schemaProps(schema)
	singular := strings.ReplaceAll(strings.TrimSuffix(lastSegment(collection), "s"), "-", "_") + "_id"
	var candidates []string
	if idParam != "id" {
		candidates = append(candidates, idParam)
	}
	candidates = append(candidates, "id", singular)
	for _, f := range candidates {
		if _, ok := props[f]; ok {
			return f
		}
	}
	return ""
}

func (e *Engine) classifySettings(paths map[string]map[string]spec.Operation, v *spec.Validator) {
	for s, m := range paths {
		if m["GET"].Op == nil || (m["PATCH"].Op == nil && m["PUT"].Op == nil) || m["POST"].Op != nil {
			continue
		}
		if !e.settingsShaped(s, m) {
			continue
		}
		initial, ok := initialValue(v, m["GET"])
		if !ok {
			continue
		}
		e.initial[s] = initial
		for method, kind := range map[string]Kind{
			"GET": KindSettingsRead, "PATCH": KindSettingsMerge, "PUT": KindSettingsReplace, "DELETE": KindSettingsReset,
		} {
			if m[method].Op != nil {
				e.set(method, s, Route{Kind: kind, Collection: s, Parent: e.settingsParent(s)})
			}
		}
	}
}

// settingsParent returns the longest handled item template that s lives
// under, or "" if there is none.
func (e *Engine) settingsParent(s string) string {
	best := ""
	for item := range e.readable {
		if strings.HasPrefix(s, item+"/") && len(item) > len(best) {
			best = item
		}
	}
	return best
}

// settingsShaped reports whether path s, already known to have GET and
// PATCH/PUT and no POST, is object-shaped and not a collection, item or
// deny-listed relation.
func (e *Engine) settingsShaped(s string, m map[string]spec.Operation) bool {
	if settingsDenyList[s] {
		return false
	}
	if _, isColl := e.candColls[s]; isColl {
		return false
	}
	if _, isItem := e.candItems[s]; isItem {
		return false
	}
	if !objectResponse(m["GET"].Op) {
		return false
	}
	for _, method := range []string{"PATCH", "PUT"} {
		if op := m[method].Op; op != nil && !objectBody(op) {
			return false
		}
	}
	return true
}

func isPlainObject(s *openapi3.SchemaRef) bool {
	return s != nil && s.Value != nil && s.Value.Type.Is("object") &&
		len(s.Value.OneOf) == 0 && len(s.Value.AnyOf) == 0
}

func objectResponse(op *openapi3.Operation) bool {
	resp := op.Responses.Status(http.StatusOK)
	if resp == nil || resp.Value == nil {
		return false
	}
	media := resp.Value.Content.Get("application/json")
	return media != nil && isPlainObject(media.Schema)
}

func objectBody(op *openapi3.Operation) bool {
	if op.RequestBody == nil || op.RequestBody.Value == nil {
		return false
	}
	media := op.RequestBody.Value.Content.Get("application/json")
	return media != nil && isPlainObject(media.Schema)
}

func initialValue(v *spec.Validator, get spec.Operation) (any, bool) {
	hdr := map[string]string{"Content-Type": "application/json"}
	if v.ValidateResponse(get, http.StatusOK, hdr, []byte("{}")) == nil {
		return map[string]any{}, true
	}
	media := get.Op.Responses.Status(http.StatusOK).Value.Content.Get("application/json")
	val := MinimalValue(media.Schema)
	body, err := json.Marshal(val)
	if err != nil || v.ValidateResponse(get, http.StatusOK, hdr, body) != nil {
		return nil, false
	}
	return val, true
}
