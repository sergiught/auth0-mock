package mgmtstore

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func raw(t *testing.T, s string) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(s), &m))
	return m
}

const seedData = `{
  "users": [
    {"user_id": "auth0|u1", "email": "a@example.com", "connection": "Username-Password-Authentication", "password": "Secret123!"},
    {"email": "b@example.com", "connection": "Username-Password-Authentication"}
  ],
  "roles": [{"name": "admin"}],
  "organizations": [{"id": "org_1", "name": "acme"}],
  "organizations/org_1/invitations": [
    {"inviter": {"name": "Ann"}, "invitee": {"email": "c@example.com"}, "client_id": "abc"}
  ],
  "tenants/settings": {"friendly_name": "Mock"}
}`

func TestLoadAndExportRoundTrip(t *testing.T) {
	e, _ := loadEngine(t)
	s1 := e.NewStore(fixedNow)
	require.NoError(t, s1.Load(raw(t, seedData)))
	out1 := s1.Export()
	assert.Len(t, out1["users"], 2)
	assert.Contains(t, out1, "tenants/settings")
	assert.Contains(t, out1, "organizations/org_1/invitations")

	b, err := json.Marshal(out1)
	require.NoError(t, err)
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &m))

	s2 := e.NewStore(fixedNow)
	require.NoError(t, s2.Load(m))
	assert.Equal(t, out1, s2.Export())
}

func TestLoadKeepsIDsAndStamps(t *testing.T) {
	e, _ := loadEngine(t)
	s := e.NewStore(fixedNow)
	require.NoError(t, s.Load(raw(t, `{"organizations": [{"id": "org_1", "name": "acme"}],
	  "organizations/org_1/invitations": [{"id": "inv_1", "created_at": "2020-01-01T00:00:00.000Z",
	    "inviter": {"name": "Ann"}, "invitee": {"email": "c@example.com"}, "client_id": "abc"}]}`)))
	o, ok := s.get("/organizations/org_1/invitations", "inv_1")
	require.True(t, ok)
	assert.Equal(t, "2020-01-01T00:00:00.000Z", o["created_at"])
}

func TestLoadErrorsNameItem(t *testing.T) {
	e, _ := loadEngine(t)
	err := e.NewStore(fixedNow).Load(raw(t, `{"users": [
	  {"email": "a@example.com", "connection": "c"},
	  {"email": 5, "connection": "c"}]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "management.users[1]: ")

	err = e.NewStore(fixedNow).Load(raw(t, `{"roles": [{"name": "a", "bogus": 1}]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "management.roles[0]: ")
	assert.Contains(t, err.Error(), "bogus")

	err = e.NewStore(fixedNow).Load(raw(t, `{"organizations": [{"id": "x", "name": "a"}, {"id": "x", "name": "b"}]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "management.organizations[1]: ")
}

func TestLoadUnknownKey(t *testing.T) {
	e, _ := loadEngine(t)
	err := e.NewStore(fixedNow).Load(raw(t, `{"nope": []}`))
	require.Error(t, err)
	assert.Equal(t, `management."nope": not a stored collection or settings path`, err.Error())
}

func TestLoadNestedNeedsParent(t *testing.T) {
	e, _ := loadEngine(t)
	err := e.NewStore(fixedNow).Load(raw(t, `{"organizations/org_9/invitations": [
	  {"inviter": {"name": "Ann"}, "invitee": {"email": "c@example.com"}, "client_id": "abc"}]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `management."organizations/org_9/invitations"`)
}

func TestExportOmitsPassword(t *testing.T) {
	e, _ := loadEngine(t)
	s := e.NewStore(fixedNow)
	require.NoError(t, s.Load(raw(t, seedData)))
	u, ok := s.get("/users", "auth0|u1")
	require.True(t, ok)
	assert.Equal(t, "Secret123!", u["password"])
	for _, item := range s.Export()["users"].([]Object) {
		assert.NotContains(t, item, "password")
	}
}

func TestCheckSeedDoesNotTouchStore(t *testing.T) {
	e, _ := loadEngine(t)
	require.NoError(t, e.CheckSeed(raw(t, seedData)))
	assert.Error(t, e.CheckSeed(raw(t, `{"nope": []}`)))
}

func TestLoadFailureLeavesStoreUnchanged(t *testing.T) {
	e, _ := loadEngine(t)
	s := e.NewStore(fixedNow)
	require.NoError(t, s.Load(raw(t, seedData)))
	before := s.Export()

	// "roles" sorts before "users" and loads; "users" then fails.
	err := s.Load(raw(t, `{"roles": [{"name": "new"}], "users": [{"email": 5, "connection": "c"}]}`))
	require.Error(t, err)
	assert.Equal(t, before, s.Export())
}

func TestLoadStoresEncodedKeyWhereServeLooksIt(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, h.s.Load(raw(t, `{"users": [{"user_id": "auth0|abc", "connection": "x"}],
	  "users/auth0%7Cabc/authentication-methods": [{"id": "am_1", "type": "totp"}]}`)))
	code, _, body := h.do("GET", "/api/v2/users/auth0%7Cabc/authentication-methods", "", "")
	assert.Equal(t, 200, code)
	assert.Contains(t, string(body), "am_1")
}

// exportJSON is the export as one JSON string, for substring checks.
func (s *Store) exportJSON(t *testing.T) string {
	t.Helper()
	b, err := json.Marshal(s.Export())
	require.NoError(t, err)
	return string(b)
}

func TestLoadRejectsBadResponseOnlyValue(t *testing.T) {
	e, _ := loadEngine(t)
	err := e.NewStore(fixedNow).Load(raw(t, `{"users": [{"user_id": "auth0|seeded", "connection": "c", "logins_count": "many"}]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "management.users[0]: ")
	assert.Contains(t, err.Error(), `"logins_count"`)

	require.NoError(t, e.NewStore(fixedNow).Load(raw(t, `{"users": [{"user_id": "auth0|seeded", "connection": "c", "logins_count": 3}]}`)))
}

func TestEmptiedSettingsRoundTrip(t *testing.T) {
	h := newHarness(t)
	code, _, body := h.do("PATCH", "/api/v2/tenants/settings", "", `{"friendly_name":"x"}`)
	require.Equal(t, 200, code, string(body))
	code, _, body = h.do("PATCH", "/api/v2/tenants/settings", "", `{"friendly_name":null}`)
	require.Equal(t, 200, code, string(body))
	out := h.s.Export()
	assert.Equal(t, Object{}, out["tenants/settings"])

	b, err := json.Marshal(out)
	require.NoError(t, err)
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &m))
	s2 := h.s.engine.NewStore(fixedNow)
	require.NoError(t, s2.Load(m))
	assert.Equal(t, out, s2.Export())
}

func TestExportRoundTripsEncodedParentID(t *testing.T) {
	e, _ := loadEngine(t)
	s1 := e.NewStore(fixedNow)
	require.NoError(t, s1.Load(raw(t, `{"users": [{"user_id": "auth0|100%", "connection": "x"}],
	  "users/auth0%7C100%25/authentication-methods": [{"id": "am_1", "type": "totp"}]}`)))
	out1 := s1.Export()
	require.Contains(t, out1, "users/auth0%7C100%25/authentication-methods")

	b, err := json.Marshal(out1)
	require.NoError(t, err)
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &m))
	s2 := e.NewStore(fixedNow)
	require.NoError(t, s2.Load(m))
	assert.Equal(t, out1, s2.Export())
	am, ok := s2.get("/users/auth0%7C100%25/authentication-methods", "am_1")
	require.True(t, ok)
	assert.Equal(t, "totp", am["type"])
}

func TestLoadShapeErrors(t *testing.T) {
	e, _ := loadEngine(t)
	cases := map[string]string{
		`{"tenants/settings": 5}`:                           `management."tenants/settings": must be an object`,
		`{"tenants/settings": {"bogus": 1}}`:                "management.tenants/settings: ",
		`{"roles": {"name": "a"}}`:                          `management."roles": must be an array of objects`,
		`{"roles": [null]}`:                                 "management.roles[0]: must be an object",
		`{"roles": [{"name": 5}]}`:                          "management.roles[0]: ",
		`{"users/auth0%7Cnone/authentication-methods": []}`: "is not in the seed",
	}
	for in, want := range cases {
		err := e.NewStore(fixedNow).Load(raw(t, in))
		require.Error(t, err, in)
		assert.Contains(t, err.Error(), want, in)
	}
}

func TestLoadLimit(t *testing.T) {
	e, _ := loadEngine(t)
	s := e.NewStore(fixedNow)
	s.max = 1
	err := s.Load(raw(t, `{"roles": [{"name": "a"}, {"name": "b"}]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "1 objects is the most /roles holds")
}

func TestStoreCheckSeed(t *testing.T) {
	e, _ := loadEngine(t)
	s := e.NewStore(fixedNow)
	require.NoError(t, s.CheckSeed(raw(t, seedData)))
	assert.Empty(t, s.Export())
	assert.Error(t, s.CheckSeed(raw(t, `{"nope": []}`)))
}

func TestExportReloadsCustomUserField(t *testing.T) {
	h := newHarness(t)
	code, _, raw := h.do("POST", "/api/v2/users", "", `{"user_id":"auth0|c1","connection":"c","email":"a@b.co"}`)
	require.Equal(t, 201, code, string(raw))
	code, _, raw = h.do("PATCH", "/api/v2/users/auth0%7Cc1", "", `{"custom_flag":true}`)
	require.Equal(t, 200, code, string(raw))
	out1 := h.s.Export()
	assert.Contains(t, h.s.exportJSON(t), "custom_flag")
	b, err := json.Marshal(out1)
	require.NoError(t, err)
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &m))
	s2 := h.s.engine.NewStore(fixedNow)
	require.NoError(t, s2.Load(m))
	assert.Equal(t, out1, s2.Export())
}

func TestExportReloadsRedactedSecrets(t *testing.T) {
	h := newHarness(t)
	code, _, raw := h.do("POST", "/api/v2/actions/modules", "", `{"name":"m","code":"x","secrets":[{"name":"API_KEY","value":"private-value"}]}`)
	require.Equal(t, 201, code, string(raw))
	out1 := h.s.Export()
	b, err := json.Marshal(out1)
	require.NoError(t, err)
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &m))
	s2 := h.s.engine.NewStore(fixedNow)
	require.NoError(t, s2.Load(m))
	assert.Equal(t, out1, s2.Export())
	assert.NotContains(t, s2.exportJSON(t), "private-value")
	assert.NotContains(t, s2.exportJSON(t), `"value"`)
}

func TestLoadNestedSeedErrors(t *testing.T) {
	e, _ := loadEngine(t)
	// Nested response-only key with a bad value.
	err := e.NewStore(fixedNow).Load(raw(t, `{"actions/modules": [{"name":"m","code":"x","secrets":[{"name":"a","updated_at":5}]}]}`))
	require.Error(t, err)
	// Unknown key nested in an array item.
	err = e.NewStore(fixedNow).Load(raw(t, `{"actions/modules": [{"name":"m","code":"x","secrets":[{"name":"a","zzz":5}]}]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "zzz")
}

func TestLoadValidatesPresentKeysWithoutRequestSchema(t *testing.T) {
	e, _ := loadEngine(t)
	const mod = `"actions/modules": [{"id": "m1", "name": "m", "code": "x"}], `
	err := e.NewStore(fixedNow).Load(raw(t, `{`+mod+`"actions/modules/m1/versions": [{"id": "v1", "version_number": "many"}]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "management.actions/modules/m1/versions[0]: ")
	assert.Contains(t, err.Error(), `"version_number"`)

	require.NoError(t, e.NewStore(fixedNow).Load(raw(t, `{`+mod+`"actions/modules/m1/versions": [{"id": "v1", "version_number": 2}]}`)))
}

func TestEncodedSlashParentIDKeepsChildren(t *testing.T) {
	const key = "users/auth0%7Ca%2Fb/authentication-methods"
	e, _ := loadEngine(t)
	s1 := e.NewStore(fixedNow)
	require.NoError(t, s1.Load(raw(t, `{"users": [{"user_id": "auth0|a/b", "connection": "x"}], "`+key+`": [{"id": "am_1", "type": "totp"}]}`)))
	out1 := s1.Export()
	require.Contains(t, out1, key)
	b, err := json.Marshal(out1)
	require.NoError(t, err)
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &m))
	s2 := e.NewStore(fixedNow)
	require.NoError(t, s2.Load(m))
	assert.Equal(t, out1, s2.Export())
	assert.Contains(t, s2.exportJSON(t), "am_1")
}

func TestLoadChecksSettingsSeedAgainstReadSchema(t *testing.T) {
	e, _ := loadEngine(t)
	seed := func(n int) map[string]json.RawMessage {
		return raw(t, `{"guardian/factors/push-notification/providers/apns": {"bundle_id": "`+strings.Repeat("b", n)+`"}}`)
	}
	err := e.NewStore(fixedNow).Load(seed(21))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "guardian/factors/push-notification/providers/apns")
	assert.Contains(t, err.Error(), `"bundle_id"`)

	require.NoError(t, e.NewStore(fixedNow).Load(seed(20)))
}
