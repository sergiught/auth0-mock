package admin0

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/claims"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/mfa"
	"github.com/sergiught/auth0-mock/internal/permissions"
)

func newRouter(d Deps) chi.Router {
	r := chi.NewRouter()
	Mount(r, d)
	return r
}

func newDeps() Deps {
	return Deps{
		Matches:     matches.NewStore(),
		Claims:      claims.NewStore(),
		Permissions: permissions.NewStore(),
		MFA:         mfa.NewStore(),
	}
}

func TestReset_WipesAllMatches(t *testing.T) {
	d := newDeps()
	d.Matches.Put(matches.Match{Method: "GET", Path: "/api/v2/users/{id}", Kind: matches.KindTemplate, Status: 200})
	d.Claims.Set(map[string]any{"role": "admin"})
	d.Permissions.Set("api", []string{"read:users"})
	d.MFA.SetRequired(true)

	r := newRouter(d)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/admin0/reset", nil))

	assert.Equal(t, 204, w.Code)
	assert.Empty(t, d.Matches.List())
	assert.Empty(t, d.Claims.Get())
	assert.Empty(t, d.Permissions.All())
	assert.False(t, d.MFA.IsRequired())
}

func TestMFARequired_PutGet(t *testing.T) {
	d := newDeps()
	r := newRouter(d)

	// PUT true.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("PUT", "/admin0/mfa-required", strings.NewReader(`{"required":true}`)))
	require.Equal(t, 204, w.Code)
	assert.True(t, d.MFA.IsRequired())

	// GET.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/admin0/mfa-required", nil))
	require.Equal(t, 200, w.Code)
	var body map[string]bool
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.True(t, body["required"])

	// PUT false.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("PUT", "/admin0/mfa-required", strings.NewReader(`{"required":false}`)))
	require.Equal(t, 204, w.Code)
	assert.False(t, d.MFA.IsRequired())
}

func TestClaims_PutGetDelete(t *testing.T) {
	d := newDeps()
	r := newRouter(d)

	// PUT.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("PUT", "/admin0/claims", strings.NewReader(`{"role":"admin","org_id":"o1"}`)))
	require.Equal(t, 204, w.Code)
	assert.Equal(t, "admin", d.Claims.Get()["role"])

	// GET.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/admin0/claims", nil))
	require.Equal(t, 200, w.Code)
	var got map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "admin", got["role"])
	assert.Equal(t, "o1", got["org_id"])

	// DELETE.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("DELETE", "/admin0/claims", nil))
	require.Equal(t, 204, w.Code)
	assert.Empty(t, d.Claims.Get())
}

func TestClaims_Put_InvalidJSON_400(t *testing.T) {
	d := newDeps()
	r := newRouter(d)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("PUT", "/admin0/claims", strings.NewReader(`not json`)))
	assert.Equal(t, 400, w.Code)
}

func TestPermissions_PutGetDeletePerAudience(t *testing.T) {
	d := newDeps()
	r := newRouter(d)

	// PUT myapi.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("PUT", "/admin0/permissions/myapi", strings.NewReader(`["read:users","write:users"]`)))
	require.Equal(t, 204, w.Code)
	assert.ElementsMatch(t, []string{"read:users", "write:users"}, d.Permissions.Get("myapi"))

	// GET single audience.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/admin0/permissions/myapi", nil))
	require.Equal(t, 200, w.Code)
	var perms []string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &perms))
	assert.ElementsMatch(t, []string{"read:users", "write:users"}, perms)

	// GET unregistered audience → empty array.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/admin0/permissions/other", nil))
	require.Equal(t, 200, w.Code)
	var none []string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &none))
	assert.Empty(t, none)

	// DELETE single.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("DELETE", "/admin0/permissions/myapi", nil))
	require.Equal(t, 204, w.Code)
	assert.Nil(t, d.Permissions.Get("myapi"))
}

func TestPermissions_ListAndDeleteAll(t *testing.T) {
	d := newDeps()
	r := newRouter(d)

	d.Permissions.Set("a", []string{"x"})
	d.Permissions.Set("b", []string{"y"})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/admin0/permissions", nil))
	require.Equal(t, 200, w.Code)
	var all map[string][]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &all))
	assert.Len(t, all, 2)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("DELETE", "/admin0/permissions", nil))
	require.Equal(t, 204, w.Code)
	assert.Empty(t, d.Permissions.All())
}
