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

func newKS(t *testing.T) *jwks.KeySet {
	t.Helper()
	ks, err := jwks.NewKeySet(jwks.Config{Issuer: "https://mock/", AccessTokenTTL: time.Hour})
	require.NoError(t, err)
	return ks
}

func TestMiddleware_Missing401(t *testing.T) {
	t.Parallel()
	ks := newKS(t)
	called := false
	h := Middleware(ks)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
	assert.Equal(t, 401, w.Code)
	assert.False(t, called)
}

func TestMiddleware_Invalid401(t *testing.T) {
	t.Parallel()
	ks := newKS(t)
	h := Middleware(ks)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer not-a-jwt")
	h.ServeHTTP(w, req)
	assert.Equal(t, 401, w.Code)
}

func TestMiddleware_ValidPasses(t *testing.T) {
	t.Parallel()
	ks := newKS(t)
	tok, err := ks.Mint(jwks.MintOpts{Subject: "abc@clients", Audience: []string{"a"}, TTL: time.Hour})
	require.NoError(t, err)

	called := false
	h := Middleware(ks)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		called = true
		c := ClaimsFromContext(r.Context())
		require.NotNil(t, c)
		assert.Equal(t, "abc@clients", c.Subject)
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(w, req)

	assert.True(t, called)
	assert.NotEqual(t, 401, w.Code)
}
