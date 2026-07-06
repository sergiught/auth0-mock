package auth0mock_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaimMappings_Get_PopulatedMap(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resource":"https://example.com/resource"}`))
	}

	got, err := c.Claims.Mappings.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/resource", got["resource"])

	call := rec.last(t)
	assert.Equal(t, http.MethodGet, call.Method)
	assert.Equal(t, "/admin0/claims/mappings", call.Path)
}

func TestClaimMappings_Get_EmptyMap(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`null`))
	}

	got, err := c.Claims.Mappings.Get(t.Context())
	require.NoError(t, err)
	// Nil → empty map per the SDK contract; callers shouldn't have to
	// nil-check before iterating.
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestClaimMappings_Get_ServerError(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}

	got, err := c.Claims.Mappings.Get(t.Context())
	require.Error(t, err)
	assert.Nil(t, got)
}

func TestClaimMappings_Set_WireShape(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	require.NoError(t, c.Claims.Mappings.Set(t.Context(), map[string]string{
		"resource": "https://example.com/resource",
	}))

	call := rec.last(t)
	assert.Equal(t, http.MethodPut, call.Method)
	assert.Equal(t, "/admin0/claims/mappings", call.Path)
	assert.Equal(t, "application/json", call.ContentType)

	var got map[string]string
	require.NoError(t, json.Unmarshal(call.Body, &got))
	assert.Equal(t, "https://example.com/resource", got["resource"])
}

func TestClaimMappings_Set_NilSendsEmptyObject(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	require.NoError(t, c.Claims.Mappings.Set(t.Context(), nil))

	// A nil map would JSON-encode to `null`, which the server happens to
	// treat as a wipe today (Decode leaves the map nil; Set(nil) installs
	// a fresh empty map). Sending `{}` doesn't rely on that coincidence:
	// it makes the wipe explicit on the wire and matches Clear's semantics.
	assert.JSONEq(t, `{}`, string(rec.last(t).Body))
}

func TestClaimMappings_Clear(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	require.NoError(t, c.Claims.Mappings.Clear(t.Context()))

	call := rec.last(t)
	assert.Equal(t, http.MethodDelete, call.Method)
	assert.Equal(t, "/admin0/claims/mappings", call.Path)
	assert.Empty(t, call.Body)
}
