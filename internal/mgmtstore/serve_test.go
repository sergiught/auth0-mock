package mgmtstore

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

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

// serveEscaped sends a request whose escaped path holds %2F straight to Serve:
// the spec router resolves decoded paths, so the operation is resolved from a
// stand-in path with the same shape.
func (h *harness) serveEscaped(method, escaped, standIn, body string) (int, string) {
	h.t.Helper()
	op, err := h.v.Resolve(method, standIn)
	require.NoError(h.t, err)
	r := httptest.NewRequest(method, escaped, strings.NewReader(body))
	w := httptest.NewRecorder()
	h.s.Serve(w, r, op, []byte(body))
	return w.Code, w.Body.String()
}

func TestServeEncodedSlashParentID(t *testing.T) {
	h := newHarness(t)
	code, raw := h.serveEscaped("POST", "/api/v2/users", "/api/v2/users", `{"user_id":"auth0|a/b","connection":"x"}`)
	require.Equal(t, 201, code, raw)
	assert.Contains(t, raw, `"user_id":"auth0|a/b"`)
	const path = "/api/v2/users/auth0%7Ca%2Fb/authentication-methods"
	const standIn = "/api/v2/users/x/authentication-methods"
	code, raw = h.serveEscaped("POST", path, standIn, `{"type":"totp"}`)
	require.Equal(t, 201, code, raw)
	code, raw = h.serveEscaped("GET", path, standIn, "")
	require.Equal(t, 200, code, raw)
	assert.Contains(t, raw, `"type":"totp"`)
	assert.Contains(t, h.s.exportJSON(t), "users/auth0%7Ca%2Fb/authentication-methods")

	code, raw = h.serveEscaped("DELETE", "/api/v2/users/auth0%7Ca%2Fb", "/api/v2/users/x", "")
	require.Equal(t, 204, code, raw)
	assert.NotContains(t, h.s.exportJSON(t), "authentication-methods")
	code, _ = h.serveEscaped("GET", path, standIn, "")
	assert.Equal(t, 404, code)
}

func TestServeRejectsWriteTheReadSchemaRejects(t *testing.T) {
	h := newHarness(t)
	const path = "/api/v2/guardian/factors/push-notification/providers/apns"
	code, _, raw := h.do("PATCH", path, "", `{"bundle_id":"`+strings.Repeat("a", 21)+`"}`)
	assert.Equal(t, 400, code, string(raw))
	assert.Contains(t, string(raw), "bundle_id")
	assert.NotContains(t, string(raw), "must be a JSON object")
	code, _, raw = h.do("PATCH", path, "", `[1]`)
	assert.Equal(t, 400, code, string(raw))
	assert.Contains(t, string(raw), "The request body must be a JSON object.")
	_, before, _ := h.do("GET", path, "", "")
	assert.NotContains(t, before, "bundle_id")

	code, _, raw = h.do("PATCH", path, "", `{"bundle_id":"`+strings.Repeat("a", 20)+`"}`)
	require.Equal(t, 200, code, string(raw))
	code, got, _ := h.do("GET", path, "", "")
	require.Equal(t, 200, code)
	assert.Equal(t, strings.Repeat("a", 20), got["bundle_id"])

	code, _, _ = h.do("PATCH", path, "", `{"bundle_id":"`+strings.Repeat("b", 21)+`"}`)
	assert.Equal(t, 400, code)
	_, got, _ = h.do("GET", path, "", "")
	assert.Equal(t, strings.Repeat("a", 20), got["bundle_id"])
}

func TestServeUpdateBumpsUpdatedAtWhenPatchResponseOmitsIt(t *testing.T) {
	h := newHarness(t)
	code, created, _ := h.do("POST", "/api/v2/token-exchange-profiles", "",
		`{"name":"profile one","subject_token_type":"urn:example:token","action_id":"act_1","type":"custom_authentication"}`)
	require.Equal(t, 201, code)
	id := created["id"].(string)
	h.s.now = func() time.Time { return fixedNow().Add(time.Hour) }
	code, _, _ = h.do("PATCH", "/api/v2/token-exchange-profiles/"+id, "", `{"name":"profile two"}`)
	require.Equal(t, 200, code)
	_, got, _ := h.do("GET", "/api/v2/token-exchange-profiles/"+id, "", "")
	assert.Equal(t, "profile two", got["name"])
	assert.Equal(t, created["created_at"], got["created_at"])
	assert.NotEqual(t, created["updated_at"], got["updated_at"])
}

func TestExportRedactsWriteOnlySettings(t *testing.T) {
	h := newHarness(t)
	const path = "/api/v2/guardian/factors/push-notification/providers/apns"
	code, _, raw := h.do("PUT", path, "", `{"bundle_id":"com.example.app","p12":"c2VjcmV0"}`)
	require.Equal(t, 200, code, string(raw))
	out := h.s.Export()
	got, ok := out["guardian/factors/push-notification/providers/apns"].(Object)
	require.True(t, ok)
	assert.Equal(t, "com.example.app", got["bundle_id"])
	assert.NotContains(t, got, "p12")

	reloaded := h.s.engine.NewStore(fixedNow)
	b, err := json.Marshal(out)
	require.NoError(t, err)
	var data map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &data))
	require.NoError(t, reloaded.Load(data))
	assert.Equal(t, out, reloaded.Export())
}

func TestServeDropsUpdateOnlyFields(t *testing.T) {
	h := newHarness(t)
	code, _, raw := h.do("POST", "/api/v2/users", "", `{"user_id":"auth0|u1","connection":"c","email":"a@b.co"}`)
	require.Equal(t, 201, code, string(raw))
	code, patched, raw := h.do("PATCH", "/api/v2/users/auth0%7Cu1", "", `{"client_id":"abc","verify_phone_number":true,"custom_flag":true}`)
	require.Equal(t, 200, code, string(raw))
	assert.NotContains(t, patched, "client_id")
	assert.NotContains(t, patched, "verify_phone_number")

	_, got, _ := h.do("GET", "/api/v2/users/auth0%7Cu1", "", "")
	_, _, list := h.do("GET", "/api/v2/users", "", "")
	for _, body := range []string{string(mustJSON(t, got)), string(list), h.s.exportJSON(t)} {
		assert.NotContains(t, body, "client_id")
		assert.NotContains(t, body, "verify_phone_number")
		assert.Contains(t, body, "a@b.co")
		assert.Contains(t, body, "custom_flag")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func TestServeDropsVersionFromTemplatePatch(t *testing.T) {
	h := newHarness(t)
	const base = "/api/v2/verifiable-credentials/verification/templates"
	code, created, raw := h.do("POST", base, "", `{"name":"t","type":"t","dialect":"d","presentation":{"org.iso.18013.5.1.mDL":{"org.iso.18013.5.1":{"family_name":true}}},"well_known_trusted_issuers":"x"}`)
	require.Equal(t, 201, code, string(raw))
	code, patched, raw := h.do("PATCH", base+"/"+created["id"].(string), "", `{"version":1,"name":"n"}`)
	require.Equal(t, 200, code, string(raw))
	assert.NotContains(t, patched, "version")
	assert.Equal(t, "n", patched["name"])
}

// TestConnectionPrecedenceRoundTrip creates an auth0 connection with the given precedence
// the way the HTTP handler would: the request validator runs first, and only
// a body it accepts reaches Serve. What it stores must export and reload.
func TestConnectionPrecedenceRoundTrip(t *testing.T) {
	h := newHarness(t)
	post := func(body string) error {
		op, err := h.v.Resolve("POST", "/api/v2/connections")
		require.NoError(t, err)
		r := httptest.NewRequest("POST", "/api/v2/connections", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if err := h.v.ValidateRequest(r, op); err != nil {
			return err
		}
		code, _, raw := h.do("POST", "/api/v2/connections", "", body)
		require.Equal(t, 201, code, string(raw))
		return nil
	}

	// The spec requires exactly three identifiers: the write path rejects two.
	err := post(`{"name":"c2","strategy":"auth0","options":{"precedence":["email","username"]}}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "minimum number of items is 3")
	assert.Empty(t, h.s.Export())

	err = post(`{"name":"c3","strategy":"auth0","options":{"precedence":["email","phone_number","username"]}}`)
	require.NoError(t, err)
	out := h.s.Export()
	m := map[string]json.RawMessage{}
	for k, v := range out {
		m[k] = mustJSON(t, v)
	}
	s2 := h.s.engine.NewStore(fixedNow)
	require.NoError(t, s2.Load(m))
	assert.Equal(t, out, s2.Export())

	// Seed validation agrees with the write path on the two-item body.
	m2 := map[string]json.RawMessage{"connections": json.RawMessage(`[{"name":"c2","strategy":"auth0","options":{"precedence":["email","username"]}}]`)}
	require.Error(t, h.s.engine.NewStore(fixedNow).Load(m2))
}

func TestServeEnforcesSchemaPatterns(t *testing.T) {
	h := newHarness(t)
	code, org, raw := h.do("POST", "/api/v2/organizations", "", `{"name":"acme"}`)
	require.Equal(t, 201, code, string(raw))
	path := "/api/v2/organizations/" + org["id"].(string) + "/discovery-domains"
	code, _, raw = h.do("POST", path, "", `{"domain":"bad domain"}`)
	assert.Equal(t, 400, code, string(raw))
	code, _, raw = h.do("POST", path, "", `{"domain":"example.com"}`)
	assert.Equal(t, 201, code, string(raw))
}

const mixpanelStream = `{"type":"mixpanel","name":"m","sink":{"mixpanelRegion":"us","mixpanelProjectId":"1","mixpanelServiceAccountUsername":"u","mixpanelServiceAccountPassword":"p"}}`

func TestPatchKeepsOmittedSinkSecret(t *testing.T) {
	h := newHarness(t)
	code, created, raw := h.do("POST", "/api/v2/log-streams", "", mixpanelStream)
	require.Equal(t, 200, code, string(raw))
	path := "/api/v2/log-streams/" + created["id"].(string)
	code, got, raw := h.do("PATCH", path, "", `{"sink":{"mixpanelRegion":"eu","mixpanelProjectId":"2","mixpanelServiceAccountUsername":"v"}}`)
	require.Equal(t, 200, code, string(raw))
	sink := got["sink"].(map[string]any)
	assert.Equal(t, "p", sink["mixpanelServiceAccountPassword"])
	assert.Equal(t, "eu", sink["mixpanelRegion"])

	out, err := json.Marshal(h.s.Export())
	require.NoError(t, err)
	var data map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(out, &data))
	s2 := h.s.engine.NewStore(fixedNow)
	require.NoError(t, s2.Load(data))
	again, err := json.Marshal(s2.Export())
	require.NoError(t, err)
	assert.JSONEq(t, string(out), string(again))
}

func TestPatchSinkStaysFullReplaceForReadableKeys(t *testing.T) {
	h := newHarness(t)
	_, created, _ := h.do("POST", "/api/v2/log-streams", "", `{"type":"http","name":"h","sink":{"httpEndpoint":"https://example.com/a","httpAuthorization":"tok"}}`)
	path := "/api/v2/log-streams/" + created["id"].(string)
	code, got, raw := h.do("PATCH", path, "", `{"sink":{"httpEndpoint":"https://example.com/b"}}`)
	require.Equal(t, 200, code, string(raw))
	sink := got["sink"].(map[string]any)
	assert.Equal(t, "https://example.com/b", sink["httpEndpoint"])
	assert.NotContains(t, sink, "httpAuthorization")
}
