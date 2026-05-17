package auth0mock_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaims_Get_PopulatedMap(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"https://example.com/role":"admin","tenant":"acme"}`))
	}

	got, err := c.Claims.Get(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "admin", got["https://example.com/role"])
	assert.Equal(t, "acme", got["tenant"])

	call := rec.last(t)
	assert.Equal(t, http.MethodGet, call.Method)
	assert.Equal(t, "/admin0/claims", call.Path)
}

func TestClaims_Get_EmptyMap(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`null`))
	}

	got, err := c.Claims.Get(context.Background())
	require.NoError(t, err)
	// Nil → empty map per the SDK contract; callers shouldn't have to
	// nil-check before iterating.
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestClaims_Set_WireShape(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	require.NoError(t, c.Claims.Set(context.Background(), map[string]any{
		"https://example.com/role": "admin",
		"tenant":                   "acme",
	}))

	call := rec.last(t)
	assert.Equal(t, http.MethodPut, call.Method)
	assert.Equal(t, "/admin0/claims", call.Path)
	assert.Equal(t, "application/json", call.ContentType)

	var got map[string]any
	require.NoError(t, json.Unmarshal(call.Body, &got))
	assert.Equal(t, "admin", got["https://example.com/role"])
	assert.Equal(t, "acme", got["tenant"])
}

func TestClaims_Set_NilSendsEmptyObject(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	require.NoError(t, c.Claims.Set(context.Background(), nil))

	// A nil map would JSON-encode to `null` — but the server's PUT
	// handler decodes into `map[string]any` and would silently no-op.
	// Worse, a real-world bug here is hard to notice. Sending `{}`
	// makes the wipe explicit and matches Clear's semantics.
	assert.JSONEq(t, `{}`, string(rec.last(t).Body))
}

func TestClaims_Clear(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	require.NoError(t, c.Claims.Clear(context.Background()))

	call := rec.last(t)
	assert.Equal(t, http.MethodDelete, call.Method)
	assert.Equal(t, "/admin0/claims", call.Path)
	assert.Empty(t, call.Body)
}
