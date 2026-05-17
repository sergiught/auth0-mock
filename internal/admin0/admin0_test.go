package admin0

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/claims"
	"github.com/sergiught/auth0-mock/internal/clock"
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
		Clock:       clock.NewControlled(),
	}
}

func TestReset_WipesAllMatches(t *testing.T) {
	d := newDeps()
	d.Matches.Put(matches.Expectation{Method: "GET", Path: "/api/v2/users/{id}", Kind: matches.KindTemplate, Response: matches.ResponseDef{Status: 200}})
	d.Claims.Set(map[string]any{"role": "admin"})
	d.Permissions.Set("api", []string{"read:users"})
	d.MFA.SetRequired(true)
	d.Clock.Freeze(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))

	r := newRouter(d)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/admin0/reset", nil))

	assert.Equal(t, 204, w.Code)
	assert.Empty(t, d.Matches.List())
	assert.Empty(t, d.Claims.Get())
	assert.Empty(t, d.Permissions.All())
	assert.False(t, d.MFA.IsRequired())
	mode, _ := d.Clock.State()
	assert.Equal(t, clock.ModeReal, mode)
}

// --- clock handler tests -----------------------------------------------------.

func TestClock_GetReturnsRealByDefault(t *testing.T) {
	t.Parallel()
	d := newDeps()
	r := newRouter(d)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/admin0/clock", nil))
	require.Equal(t, 200, w.Code)

	var got struct {
		Mode string `json:"mode"`
		Now  string `json:"now"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	assert.Equal(t, "real", got.Mode)
	assert.NotEmpty(t, got.Now)
}

func TestClock_PutFreezeAndGet(t *testing.T) {
	t.Parallel()
	d := newDeps()
	r := newRouter(d)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("PUT", "/admin0/clock",
		strings.NewReader(`{"now":"2030-01-01T00:00:00Z"}`)))
	require.Equal(t, 204, w.Code)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/admin0/clock", nil))
	require.Equal(t, 200, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"mode":"frozen"`)
	assert.Contains(t, body, `"now":"2030-01-01T00:00:00Z"`)
}

func TestClock_PutOffset_IncludesOffsetInGet(t *testing.T) {
	t.Parallel()
	d := newDeps()
	r := newRouter(d)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("PUT", "/admin0/clock",
		strings.NewReader(`{"offset":"25h"}`)))
	require.Equal(t, 204, w.Code)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/admin0/clock", nil))
	require.Equal(t, 200, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"mode":"offset"`)
	assert.Contains(t, body, `"offset":"25h0m0s"`)
}

func TestClock_PostAdvance_Frozen(t *testing.T) {
	t.Parallel()
	d := newDeps()
	d.Clock.Freeze(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	r := newRouter(d)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/admin0/clock/advance",
		strings.NewReader(`{"by":"25h"}`)))
	require.Equal(t, 204, w.Code)

	_, now := d.Clock.State()
	assert.True(t, now.Equal(time.Date(2030, 1, 2, 1, 0, 0, 0, time.UTC)))
}

func TestClock_PostAdvance_RealMode_400(t *testing.T) {
	t.Parallel()
	d := newDeps()
	r := newRouter(d)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/admin0/clock/advance",
		strings.NewReader(`{"by":"1h"}`)))
	require.Equal(t, 400, w.Code)
	assert.Contains(t, w.Body.String(), "invalid_clock_state")
}

func TestClock_Delete_RestoresReal(t *testing.T) {
	t.Parallel()
	d := newDeps()
	d.Clock.Freeze(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	r := newRouter(d)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("DELETE", "/admin0/clock", nil))
	require.Equal(t, 204, w.Code)
	mode, _ := d.Clock.State()
	assert.Equal(t, clock.ModeReal, mode)
}

func TestClock_PutBothNowAndOffset_400(t *testing.T) {
	t.Parallel()
	d := newDeps()
	r := newRouter(d)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("PUT", "/admin0/clock",
		strings.NewReader(`{"now":"2030-01-01T00:00:00Z","offset":"1h"}`)))
	require.Equal(t, 400, w.Code)
	assert.Contains(t, w.Body.String(), "invalid_clock_request")
}

func TestClock_PutNeither_400(t *testing.T) {
	t.Parallel()
	d := newDeps()
	r := newRouter(d)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("PUT", "/admin0/clock",
		strings.NewReader(`{}`)))
	require.Equal(t, 400, w.Code)
	assert.Contains(t, w.Body.String(), "invalid_clock_request")
}

func TestClock_PutBadRFC3339_400(t *testing.T) {
	t.Parallel()
	d := newDeps()
	r := newRouter(d)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("PUT", "/admin0/clock",
		strings.NewReader(`{"now":"not-a-timestamp"}`)))
	require.Equal(t, 400, w.Code)
	assert.Contains(t, w.Body.String(), "invalid_clock_time")
}

func TestClock_PutBadDuration_400(t *testing.T) {
	t.Parallel()
	d := newDeps()
	r := newRouter(d)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("PUT", "/admin0/clock",
		strings.NewReader(`{"offset":"twenty-five hours"}`)))
	require.Equal(t, 400, w.Code)
	assert.Contains(t, w.Body.String(), "invalid_clock_duration")
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
