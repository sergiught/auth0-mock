package mgmtstore

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/spec"
)

type harness struct {
	t *testing.T
	s *Store
	v *spec.Validator
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	e, _, v := loadShared(t)
	return &harness{t: t, s: e.NewStore(fixedNow), v: v}
}

// do sends one request through Serve and validates the response against
// the spec, where rawPath is the escaped path without the query.
func (h *harness) do(method, rawPath, query, body string) (int, map[string]any, []byte) {
	h.t.Helper()
	return h.doType(method, rawPath, query, body, "")
}

// doType is do with a request Content-Type.
func (h *harness) doType(method, rawPath, query, body, contentType string) (int, map[string]any, []byte) {
	h.t.Helper()
	u, err := url.Parse(rawPath)
	require.NoError(h.t, err)
	op, err := h.v.Resolve(method, u.Path)
	require.NoError(h.t, err)
	require.True(h.t, h.s.Handles(op), "%s %s not handled", method, rawPath)
	r := httptest.NewRequest(method, rawPath+"?"+query, strings.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	h.s.Serve(w, r, op, []byte(body))
	if w.Code < 400 {
		require.NoError(h.t, h.v.ValidateResponse(op, w.Code, map[string]string{"Content-Type": w.Header().Get("Content-Type")}, w.Body.Bytes()),
			"%s %s: %s", method, rawPath, w.Body.String())
	}
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	return w.Code, m, w.Body.Bytes()
}

func TestServeRoleRoundTrip(t *testing.T) {
	h := newHarness(t)
	code, created, _ := h.do("POST", "/api/v2/roles", "", `{"name":"admin"}`)
	assert.Equal(t, 200, code)
	id, _ := created["id"].(string)
	require.NotEmpty(t, id)

	code, got, _ := h.do("GET", "/api/v2/roles/"+id, "", "")
	assert.Equal(t, 200, code)
	assert.Equal(t, created, got)

	code, patched, _ := h.do("PATCH", "/api/v2/roles/"+id, "", `{"description":"d"}`)
	assert.Equal(t, 200, code)
	assert.Equal(t, "d", patched["description"])
	assert.Equal(t, "admin", patched["name"])

	code, _, _ = h.do("DELETE", "/api/v2/roles/"+id, "", "")
	assert.Equal(t, 200, code)

	code, errBody, _ := h.do("GET", "/api/v2/roles/"+id, "", "")
	assert.Equal(t, 404, code)
	assert.Equal(t, "inexistent_resource", errBody["errorCode"])
}

func TestServeNestedMissingParent(t *testing.T) {
	h := newHarness(t)
	code, body, _ := h.do("GET", "/api/v2/organizations/nope/invitations", "", "")
	assert.Equal(t, 404, code)
	assert.Equal(t, "inexistent_resource", body["errorCode"])

	code, _, _ = h.do("POST", "/api/v2/organizations", "", `{"id":"o1","name":"org"}`)
	require.Equal(t, 201, code)
	code, _, raw := h.do("GET", "/api/v2/organizations/o1/invitations", "", "")
	assert.Equal(t, 200, code)
	assert.JSONEq(t, `[]`, string(raw))
}

func TestServeNestedSettingsMissingParent(t *testing.T) {
	h := newHarness(t)
	path := "/api/v2/self-service-profiles/p1/custom-text/en/get-started"
	code, _, _ := h.do("GET", path, "", "")
	assert.Equal(t, 404, code)

	code, _, _ = h.do("POST", "/api/v2/self-service-profiles", "", `{"id":"p1","name":"p"}`)
	require.Equal(t, 201, code)
	code, _, _ = h.do("GET", path, "", "")
	assert.Equal(t, 200, code)
}

func TestServePipeID(t *testing.T) {
	h := newHarness(t)
	code, _, _ := h.do("POST", "/api/v2/users", "", `{"user_id":"auth0|abc","connection":"x"}`)
	require.Equal(t, 201, code)
	code, _, _ = h.do("GET", "/api/v2/users/auth0|abc", "", "")
	assert.Equal(t, 200, code)
	code, got, _ := h.do("GET", "/api/v2/users/auth0%7Cabc", "", "")
	assert.Equal(t, 200, code)
	assert.Equal(t, "auth0|abc", got["user_id"])
}

func TestServeProjectionDropsPassword(t *testing.T) {
	h := newHarness(t)
	code, created, _ := h.do("POST", "/api/v2/users", "", `{"connection":"x","email":"a@b.co","password":"Secret123!"}`)
	require.Equal(t, 201, code)
	assert.NotContains(t, created, "password")

	code, _, raw := h.do("GET", "/api/v2/users", "", "")
	assert.Equal(t, 200, code)
	assert.NotContains(t, string(raw), "password")
}

func TestServeLimit429(t *testing.T) {
	h := newHarness(t)
	h.s.max = 1
	code, _, _ := h.do("POST", "/api/v2/roles", "", `{"name":"a"}`)
	require.Equal(t, 200, code)
	code, body, _ := h.do("POST", "/api/v2/roles", "", `{"name":"b"}`)
	assert.Equal(t, 429, code)
	assert.Equal(t, "too_many_entities", body["errorCode"])
	assert.True(t, strings.HasPrefix(body["message"].(string), "mock store limit reached"))
}

func TestServeConflict409(t *testing.T) {
	h := newHarness(t)
	code, _, _ := h.do("POST", "/api/v2/users", "", `{"user_id":"u1","connection":"x"}`)
	require.Equal(t, 201, code)
	code, body, _ := h.do("POST", "/api/v2/users", "", `{"user_id":"u1","connection":"x"}`)
	assert.Equal(t, 409, code)
	assert.Equal(t, "already_exists", body["errorCode"])
	assert.Equal(t, "An object with id u1 already exists.", body["message"])
}

func TestServeSettings(t *testing.T) {
	h := newHarness(t)
	code, initial, _ := h.do("GET", "/api/v2/tenants/settings", "", "")
	assert.Equal(t, 200, code)
	assert.NotNil(t, initial)

	code, _, _ = h.do("PATCH", "/api/v2/tenants/settings", "", `{"friendly_name":"x"}`)
	assert.Equal(t, 200, code)
	code, got, _ := h.do("GET", "/api/v2/tenants/settings", "", "")
	assert.Equal(t, 200, code)
	assert.Equal(t, "x", got["friendly_name"])
}

func TestServeBadBody(t *testing.T) {
	h := newHarness(t)
	op, err := h.v.Resolve("POST", "/api/v2/roles")
	require.NoError(t, err)
	w := httptest.NewRecorder()
	h.s.Serve(w, httptest.NewRequest("POST", "/api/v2/roles", nil), op, []byte(`[1]`))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid_body")
}

func TestServeProjectsNestedValuesBySchema(t *testing.T) {
	h := newHarness(t)
	code, created, raw := h.do("POST", "/api/v2/actions/modules", "",
		`{"name":"m","code":"x","secrets":[{"name":"API_KEY","value":"private-value"}]}`)
	require.Equal(t, 201, code, string(raw))
	assert.NotContains(t, string(raw), "private-value")
	assert.Contains(t, string(raw), "API_KEY")

	id, _ := created["id"].(string)
	_, _, raw = h.do("GET", "/api/v2/actions/modules/"+id, "", "")
	assert.NotContains(t, string(raw), "private-value")
	_, _, raw = h.do("GET", "/api/v2/actions/modules", "", "")
	assert.NotContains(t, string(raw), "private-value")
	assert.Contains(t, string(raw), "API_KEY")
	assert.NotContains(t, h.s.exportJSON(t), "private-value")
}

func TestServeKeepsAllowedExtraKeys(t *testing.T) {
	h := newHarness(t)
	code, _, raw := h.do("POST", "/api/v2/users", "",
		`{"user_id":"auth0|x1","connection":"c","email":"a@b.co","password":"Secret123!","user_metadata":{"deep":{"k":1}}}`)
	require.Equal(t, 201, code, string(raw))
	code, patched, raw := h.do("PATCH", "/api/v2/users/auth0%7Cx1", "", `{"custom_flag":true,"password":"Other123!"}`)
	require.Equal(t, 200, code, string(raw))
	assert.Equal(t, true, patched["custom_flag"])
	assert.NotContains(t, string(raw), "password")

	_, got, raw := h.do("GET", "/api/v2/users/auth0%7Cx1", "", "")
	assert.Equal(t, true, got["custom_flag"])
	assert.Equal(t, map[string]any{"deep": map[string]any{"k": float64(1)}}, got["user_metadata"])
	assert.NotContains(t, string(raw), "password")
	_, _, raw = h.do("GET", "/api/v2/users", "", "")
	assert.Contains(t, string(raw), "custom_flag")
	assert.NotContains(t, string(raw), "password")

	exp := h.s.exportJSON(t)
	assert.Contains(t, exp, "custom_flag")
	assert.NotContains(t, exp, "password")
}

func TestServeFormBody(t *testing.T) {
	h := newHarness(t)
	code, role, raw := h.doType("POST", "/api/v2/roles", "", "name=editor", "application/x-www-form-urlencoded")
	require.Equal(t, 200, code, string(raw))
	assert.Equal(t, "editor", role["name"])
	_, got, _ := h.do("GET", "/api/v2/roles/"+role["id"].(string), "", "")
	assert.Equal(t, "editor", got["name"])

	// A form body the operation does not declare is rejected.
	op, err := h.v.Resolve("POST", "/api/v2/users")
	require.NoError(t, err)
	r := httptest.NewRequest("POST", "/api/v2/users", strings.NewReader("a=b"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.s.Serve(w, r, op, []byte("a=b"))
	assert.Equal(t, 400, w.Code)
}

func TestServeBodylessCreate(t *testing.T) {
	h := newHarness(t)
	code, created, raw := h.do("POST", "/api/v2/actions/modules", "", `{"name":"m","code":"x"}`)
	require.Equal(t, 201, code, string(raw))
	id, _ := created["id"].(string)
	code, _, raw = h.do("POST", "/api/v2/actions/modules/"+id+"/versions", "", "")
	require.Less(t, code, 300, string(raw))
	code, _, raw = h.do("GET", "/api/v2/actions/modules/"+id+"/versions", "", "")
	require.Equal(t, 200, code)
	assert.Contains(t, string(raw), "versions")
	assert.Contains(t, string(raw), `"id"`)
}
