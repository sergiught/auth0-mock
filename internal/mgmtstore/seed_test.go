package mgmtstore

import (
	"encoding/json"
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
