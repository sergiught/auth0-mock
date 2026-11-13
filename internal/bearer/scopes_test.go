package bearer

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/jwks"
)

func callWithScope(t *testing.T, required []string, scope string) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	ks := newKS(t)
	tok, err := ks.Mint(jwks.MintOpts{Subject: "abc@clients", Audience: []string{"a"}, Scope: scope, TTL: time.Hour})
	require.NoError(t, err)
	called := false
	h := Middleware(ks, "")(RequireScopes(required)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })))
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(w, req)
	return w, called
}

func TestRequireScopes_AnyOneOfSeveralPasses(t *testing.T) {
	t.Parallel()
	w, called := callWithScope(t, []string{"read:users", "read:user_idp_tokens"}, "openid read:user_idp_tokens")
	assert.True(t, called)
	assert.Equal(t, 200, w.Code)
}

func TestRequireScopes_MissingScope403(t *testing.T) {
	t.Parallel()
	w, called := callWithScope(t, []string{"read:users", "read:user_idp_tokens"}, "read:clients")
	assert.False(t, called)
	assert.Equal(t, 403, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.Empty(t, w.Header().Get("WWW-Authenticate"))
	assert.JSONEq(t, `{"statusCode":403,"error":"Forbidden","message":"Insufficient scope, expected any of: read:users,read:user_idp_tokens","errorCode":"insufficient_scope"}`, w.Body.String())
}

func TestRequireScopes_NoScopeClaim403(t *testing.T) {
	t.Parallel()
	w, called := callWithScope(t, []string{"read:users"}, "")
	assert.False(t, called)
	assert.Equal(t, 403, w.Code)
}

func TestRequireScopes_NoRequirementPasses(t *testing.T) {
	t.Parallel()
	_, called := callWithScope(t, nil, "")
	assert.True(t, called)
}
