package auth0mock_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPermissions_All(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
            "myapi":["read:users","write:users"],
            "https://api.example.com/":["admin"]
        }`))
	}

	got, err := c.Permissions.All(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"read:users", "write:users"}, got["myapi"])
	assert.Equal(t, []string{"admin"}, got["https://api.example.com/"])

	call := rec.last(t)
	assert.Equal(t, http.MethodGet, call.Method)
	assert.Equal(t, "/admin0/permissions", call.Path)
}

func TestPermissions_All_NilNormalisedToEmpty(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`null`))
	}

	got, err := c.Permissions.All(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestPermissions_Get_SimpleAudience(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`["read:users","write:users"]`))
	}

	got, err := c.Permissions.Get(context.Background(), "myapi")
	require.NoError(t, err)
	assert.Equal(t, []string{"read:users", "write:users"}, got)

	call := rec.last(t)
	assert.Equal(t, http.MethodGet, call.Method)
	assert.Equal(t, "/admin0/permissions/myapi", call.Path)
}

// TestPermissions_Get_URLFormAudience locks the audience-encoding
// contract — the SDK url.PathEscape-encodes URL-form audiences so
// the request line stays well-formed even when the audience contains
// "?" or "#"; chi's wildcard route on the server URL-decodes back
// to the original string. Auth0 audiences are typically URLs like
// "https://api.example.com/" and the existing COOKBOOK examples show
// curl using them unescaped (curl handles the encoding too).
func TestPermissions_Get_URLFormAudience(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`["admin"]`))
	}

	got, err := c.Permissions.Get(context.Background(), "https://api.example.com/")
	require.NoError(t, err)
	assert.Equal(t, []string{"admin"}, got)

	// Path on the recorded call is the URL-decoded form because
	// httptest.NewServer hands the handler r.URL.Path post-decode —
	// asserting on the decoded form is what the server's chi handler
	// would also see (chi.URLParam URL-decodes wildcards).
	assert.Equal(t, "/admin0/permissions/https://api.example.com/", rec.last(t).Path)
	// And the raw request line carries the encoded form, proving the
	// SDK actually escaped before sending.
	assert.Equal(t, "/admin0/permissions/https:%2F%2Fapi.example.com%2F", rec.last(t).RawPath)
}

// TestPermissions_Get_AudienceWithSpecialChars covers the case the
// old "we don't support ? Or #" doc warning was supposed to defend
// against — escaped properly, they round-trip without breaking
// request parsing.
func TestPermissions_Get_AudienceWithSpecialChars(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`["read"]`))
	}

	_, err := c.Permissions.Get(context.Background(), "weird?one#two")
	require.NoError(t, err)
	assert.Equal(t, "/admin0/permissions/weird%3Fone%23two", rec.last(t).RawPath)
	// Decoded path is what chi sees server-side.
	assert.Equal(t, "/admin0/permissions/weird?one#two", rec.last(t).Path)
}

func TestPermissions_Get_NilNormalisedToEmpty(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`null`))
	}

	got, err := c.Permissions.Get(context.Background(), "ghost")
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestPermissions_Set_WireShape(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	require.NoError(t, c.Permissions.Set(context.Background(), "myapi", []string{"read:users", "write:users"}))

	call := rec.last(t)
	assert.Equal(t, http.MethodPut, call.Method)
	assert.Equal(t, "/admin0/permissions/myapi", call.Path)
	assert.Equal(t, "application/json", call.ContentType)

	var got []string
	require.NoError(t, json.Unmarshal(call.Body, &got))
	assert.Equal(t, []string{"read:users", "write:users"}, got)
}

func TestPermissions_Set_NilSendsEmptyArray(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	require.NoError(t, c.Permissions.Set(context.Background(), "myapi", nil))

	// A nil slice would marshal to `null`, which decodes into a nil
	// []string and silently no-ops in the server's PUT handler. Send
	// `[]` instead so the wipe is explicit and matches Delete's
	// semantics.
	assert.JSONEq(t, `[]`, string(rec.last(t).Body))
}

func TestPermissions_Delete(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	require.NoError(t, c.Permissions.Delete(context.Background(), "myapi"))

	call := rec.last(t)
	assert.Equal(t, http.MethodDelete, call.Method)
	assert.Equal(t, "/admin0/permissions/myapi", call.Path)
}

func TestPermissions_Clear(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	require.NoError(t, c.Permissions.Clear(context.Background()))

	call := rec.last(t)
	assert.Equal(t, http.MethodDelete, call.Method)
	assert.Equal(t, "/admin0/permissions", call.Path)
}
