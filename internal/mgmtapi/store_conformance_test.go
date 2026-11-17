package mgmtapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/api"
	"github.com/sergiught/auth0-mock/internal/journal"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/mgmtstore"
	"github.com/sergiught/auth0-mock/internal/spec"
)

// conformanceFixtures holds create bodies for collections whose minimal body
// fails request validation, keyed by collection template.
var conformanceFixtures = map[string]string{
	"/clients":                  `{"name":"a"}`,
	"/connections":              `{"name":"a","strategy":"auth0"}`,
	"/device-credentials":       `{"device_id":"ab","device_name":"a","type":"public_key","value":"a"}`,
	"/hooks":                    `{"name":"a","script":"a","triggerId":"credentials-exchange"}`,
	"/branding/phone/templates": `{"type":"otp_verify","content":{"syntax":"liquid"}}`,
	"/event-streams":            `{"destination":{"type":"webhook","configuration":{"webhook_endpoint":"https://example.com","webhook_authorization":{"method":"basic","username":"a"}}}}`,
	"/log-streams":              `{"type":"http","name":"a","sink":{"httpEndpoint":"https://example.com","httpContentFormat":"JSONLINES","httpContentType":"application/json"}}`,
	"/verifiable-credentials/verification/templates": `{"dialect":"simplified/1.0","name":"a","presentation":{"org.iso.18013.5.1.mDL":{"org.iso.18013.5.1":{"given_name":true}}},"type":"a","well_known_trusted_issuers":"aamva"}`,
	"/rules":                   `{"name":"a","script":"a"}`,
	"/user-attribute-profiles": `{"name":"a","user_attributes":{"email":{"label":"Email","profile_required":true,"auth0_mapping":"email","description":"a"}}}`,
}

// patchFixtures holds PATCH bodies for settings paths whose spec rejects {},
// keyed by template.
var patchFixtures = map[string]string{
	"/branding":                     `{"colors":{"primary":"#112233"}}`,
	"/prompts":                      `{"identifier_first":true}`,
	"/client-grants/{id}":           `{"allow_all_scopes":true}`,
	"/connections/{id}":             `{"display_name":"a"}`,
	"/token-exchange-profiles/{id}": `{"name":"abc"}`,
}

var sharedSpec struct {
	once sync.Once
	s    *spec.Spec
	v    *spec.Validator
	err  error
}

func loadSpec(t *testing.T) (*spec.Spec, *spec.Validator) {
	t.Helper()
	sharedSpec.once.Do(func() {
		sharedSpec.s, sharedSpec.err = spec.Load(api.ManagementOpenAPIJSON)
		if sharedSpec.err != nil {
			return
		}
		sharedSpec.v, sharedSpec.err = spec.NewValidator(sharedSpec.s)
	})
	require.NoError(t, sharedSpec.err)
	return sharedSpec.s, sharedSpec.v
}

type conformance struct {
	t     *testing.T
	r     chi.Router
	v     *spec.Validator
	token string
	// Ops is keyed by "METHOD template".
	Ops    map[string]spec.Operation
	routes []mgmtstore.RouteInfo
	// Created is the body the last create succeeded with.
	Created string
	base    string
}

func (c *conformance) op(method, template string) (spec.Operation, bool) {
	op, ok := c.Ops[method+" "+template]
	return op, ok
}

func doRequest(r http.Handler, token, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// minimalBody builds a body from the operation's request schema.
func (c *conformance) minimalBody(op spec.Operation) string {
	if op.Op.RequestBody == nil || op.Op.RequestBody.Value == nil {
		return "{}"
	}
	media := op.Op.RequestBody.Value.Content.Get("application/json")
	if media == nil {
		return "{}"
	}
	b, err := json.Marshal(mgmtstore.MinimalValue(media.Schema))
	require.NoError(c.t, err)
	return string(b)
}

// call sends the request and, for 2xx, checks the response against the spec.
func (c *conformance) call(method, path, body string) (int, []byte) {
	c.t.Helper()
	w := doRequest(c.r, c.token, method, path, body)
	if w.Code >= 200 && w.Code < 300 {
		base, _, _ := strings.Cut(path, "?")
		op, err := c.v.Resolve(method, base)
		require.NoError(c.t, err, "%s %s", method, path)
		hdr := map[string]string{"Content-Type": w.Header().Get("Content-Type")}
		err = c.v.ValidateResponse(op, w.Code, hdr, w.Body.Bytes())
		// A last checkpoint page has no next cursor, so a spec whose offset and
		// checkpoint variants have no required fields matches both oneOf
		// branches; Auth0's own responses are ambiguous the same way.
		if err != nil && strings.Contains(path, "take=") && strings.Contains(err.Error(), "more than one oneOf") {
			err = checkpointShape(w.Body.Bytes())
		}
		assert.NoError(c.t, err, "%s %s -> %d %s", method, path, w.Code, w.Body.String())
	}
	return w.Code, w.Body.Bytes()
}

// checkpointShape accepts a last checkpoint page: an object with no next
// cursor and at least one array property.
func checkpointShape(body []byte) error {
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return fmt.Errorf("not an object: %w", err)
	}
	if next, ok := obj["next"]; ok && next != nil {
		return fmt.Errorf("ambiguous page still has next %v", next)
	}
	for _, v := range obj {
		if _, ok := v.([]any); ok {
			return nil
		}
	}
	return fmt.Errorf("no array item key in %s", body)
}

// itemRoute returns the first item route of the collection with the given method.
func (c *conformance) itemRoute(coll, method string) (mgmtstore.RouteInfo, bool) {
	for _, r := range c.routes {
		if r.Collection == coll && r.IDParam != "" && r.Method == method {
			return r, true
		}
	}
	return mgmtstore.RouteInfo{}, false
}

// parentPath creates the parent chain of route and returns the concrete path
// of the parent item, or "" for a top-level route.
func (c *conformance) parentPath(r mgmtstore.RouteInfo, base string) string {
	if r.Parent == "" {
		return ""
	}
	var pr mgmtstore.RouteInfo
	for _, x := range c.routes {
		if x.Template == base+r.Parent && x.Kind == mgmtstore.KindRead {
			pr = x
		}
	}
	require.NotEmpty(c.t, pr.Collection, "parent %s is not readable", r.Parent)
	collPath, obj := c.create(pr.Collection, base)
	id, _ := obj[pr.IDField].(string)
	require.NotEmpty(c.t, id, "parent %s created without %s", pr.Collection, pr.IDField)
	return collPath + "/" + url.PathEscape(id)
}

// create makes one object in the collection (and its parents) and returns the
// concrete collection path and the created object.
func (c *conformance) create(coll, base string) (string, map[string]any) {
	c.t.Helper()
	post, ok := c.op("POST", base+coll)
	require.True(c.t, ok)
	var cr mgmtstore.RouteInfo
	for _, r := range c.routes {
		if r.Template == base+coll && r.Method == "POST" {
			cr = r
		}
	}
	prefix := c.parentPath(cr, base)
	collPath := base + coll
	if prefix != "" {
		collPath = prefix + strings.TrimPrefix(coll, cr.Parent)
	}
	body, fixed := conformanceFixtures[coll]
	if !fixed {
		body = c.minimalBody(post)
	}
	code, out := c.call("POST", collPath, body)
	require.Less(c.t, code, 300, "create %s with body %s -> %d %s; add a fixture", coll, body, code, out)
	c.Created = body
	var obj map[string]any
	require.NoError(c.t, json.Unmarshal(out, &obj))
	return collPath, obj
}

func (c *conformance) idParam(coll string) string {
	r, _ := c.itemRoute(coll, "PATCH")
	return r.IDParam
}

// patch sends {}, and when request validation rejects that (minProperties), the
// body the object was created with or a settings fixture.
func (c *conformance) patch(path, method, template string) (int, []byte) {
	code, out := c.call(method, path, "{}")
	if code != http.StatusBadRequest {
		return code, out
	}
	var tries []string
	if fx, ok := patchFixtures[strings.TrimPrefix(template, c.base)]; ok {
		tries = append(tries, fx)
	}
	if op, ok := c.op(method, template); ok {
		tries = append(tries, c.minimalBody(op))
	}
	if c.Created != "" {
		tries = append(tries, c.Created)
	}
	for _, b := range tries {
		if code, out = c.call(method, path, b); code != http.StatusBadRequest {
			break
		}
	}
	return code, out
}

// fillParams replaces path parameters left in a settings path with minimal
// values of their declared schemas.
func (c *conformance) fillParams(path string, op spec.Operation) string {
	for _, p := range op.Op.Parameters {
		if p.Value != nil && p.Value.In == "path" {
			path = strings.ReplaceAll(path, "{"+p.Value.Name+"}", url.PathEscape(fmt.Sprint(mgmtstore.MinimalValue(p.Value.Schema))))
		}
	}
	return path
}

func (c *conformance) collection(coll, base string) {
	t := c.t
	collPath, obj := c.create(coll, base)
	var cr mgmtstore.RouteInfo
	for _, r := range c.routes {
		if r.Template == base+coll && r.Method == "POST" {
			cr = r
		}
	}
	id, _ := obj[cr.IDField].(string)
	require.NotEmpty(t, id, "create response lacks %s", cr.IDField)
	itemPath := collPath + "/" + url.PathEscape(id)

	if _, ok := c.itemRoute(coll, "GET"); ok {
		code, _ := c.call("GET", itemPath, "")
		assert.Equal(t, http.StatusOK, code, "GET item")
	}
	if lr, ok := c.op("GET", base+coll); ok {
		qs := []string{"", "?include_totals=true"}
		for _, p := range lr.Op.Parameters {
			if p.Value != nil && p.Value.In == "query" && p.Value.Name == "take" {
				qs = append(qs, "?take=1")
			}
		}
		for _, q := range qs {
			code, _ := c.call("GET", collPath+q, "")
			assert.Equal(t, http.StatusOK, code, "list %q", q)
		}
	}
	if _, ok := c.itemRoute(coll, "PATCH"); ok {
		code, out := c.patch(itemPath, "PATCH", base+coll+"/{"+c.idParam(coll)+"}")
		assert.Less(t, code, 300, "PATCH %s", out)
	}
	if rr, ok := c.itemRoute(coll, "PUT"); ok {
		op, _ := c.op("PUT", rr.Template)
		code, out := c.call("PUT", itemPath, c.minimalBody(op))
		assert.Less(t, code, 300, "PUT minimal %s", out)
	}
	if _, ok := c.itemRoute(coll, "DELETE"); ok {
		code, out := c.call("DELETE", itemPath, "")
		assert.Less(t, code, 300, "DELETE %s", out)
		if _, ok := c.itemRoute(coll, "GET"); ok {
			code, _ := c.call("GET", itemPath, "")
			assert.Equal(t, http.StatusNotFound, code, "GET after DELETE")
		}
	}
}

func (c *conformance) settings(path, base string) {
	t := c.t
	var get, patch, put mgmtstore.RouteInfo
	for _, r := range c.routes {
		if r.Collection != path {
			continue
		}
		switch r.Kind {
		case mgmtstore.KindSettingsRead:
			get = r
		case mgmtstore.KindSettingsMerge:
			patch = r
		case mgmtstore.KindSettingsReplace:
			put = r
		}
	}
	concrete := base + path
	if get.Parent != "" {
		concrete = c.parentPath(get, base) + strings.TrimPrefix(path, get.Parent)
	}
	getOp, _ := c.op("GET", get.Template)
	concrete = c.fillParams(concrete, getOp)
	code, out := c.call("GET", concrete, "")
	require.Equal(t, http.StatusOK, code, "GET %s", out)
	if patch.Template != "" {
		code, out = c.patch(concrete, "PATCH", patch.Template)
		assert.Less(t, code, 300, "PATCH %s", out)
	}
	if put.Template != "" {
		op, _ := c.op("PUT", put.Template)
		code, out = c.call("PUT", concrete, c.minimalBody(op))
		assert.Less(t, code, 300, "PUT minimal %s", out)
	}
	code, out = c.call("GET", concrete, "")
	assert.Equal(t, http.StatusOK, code, "GET again %s", out)
}

// TestStoreConformance drives every handled collection and settings path end
// to end and checks every 2xx response against the spec.
func TestStoreConformance(t *testing.T) {
	s, v := loadSpec(t)
	engine := mgmtstore.NewEngine(s, v)
	ks, err := jwks.NewKeySet(jwks.Config{Issuer: "https://mock/", AccessTokenTTL: 3600e9})
	require.NoError(t, err)
	token := mintBearer(t, ks)

	ops := map[string]spec.Operation{}
	for op := range s.Operations() {
		ops[op.Method+" "+op.Template] = op
	}
	routes := engine.Routes()

	// Each subtest gets a fresh mount so state never leaks between collections.
	newC := func(t *testing.T) *conformance {
		r := chi.NewRouter()
		r.Use(journal.New(10).Middleware)
		require.NoError(t, Mount(MountOpts{
			Router: r, Spec: s, Validator: v, Store: matches.NewStore(), Keys: ks,
			Log: zerolog.Nop(), Strict: true, Mgmt: engine.NewStore(time.Now),
		}))
		return &conformance{t: t, r: r, v: v, token: token, Ops: ops, routes: routes, base: s.BasePath}
	}

	seen := map[string]bool{}
	for _, r := range routes {
		tmpl := strings.TrimPrefix(r.Template, s.BasePath)
		switch r.Kind {
		case mgmtstore.KindCreate:
			if seen[tmpl] {
				continue
			}
			seen[tmpl] = true
			t.Run(tmpl, func(t *testing.T) { newC(t).collection(r.Collection, s.BasePath) })
		case mgmtstore.KindSettingsRead:
			if seen[tmpl] {
				continue
			}
			seen[tmpl] = true
			t.Run(tmpl, func(t *testing.T) { newC(t).settings(r.Collection, s.BasePath) })
		}
	}
	// Every handled collection and settings route must have been visited.
	// Nothing is skipped.
	for _, r := range routes {
		if r.Kind == mgmtstore.KindCreate || r.Kind == mgmtstore.KindSettingsRead {
			assert.True(t, seen[strings.TrimPrefix(r.Template, s.BasePath)], "route %s %s not visited", r.Method, r.Template)
		}
	}
}
