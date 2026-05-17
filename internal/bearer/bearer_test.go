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
	h := Middleware(ks, "")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
	assert.Equal(t, 401, w.Code)
	assert.False(t, called)
}

func TestMiddleware_Invalid401(t *testing.T) {
	t.Parallel()
	ks := newKS(t)
	h := Middleware(ks, "")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

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
	h := Middleware(ks, "")(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
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

func TestMiddleware_RequireAudience_Matches(t *testing.T) {
	t.Parallel()
	ks := newKS(t)
	tok, err := ks.Mint(jwks.MintOpts{
		Subject: "abc@clients", Audience: []string{"https://api/expected"}, TTL: time.Hour,
	})
	require.NoError(t, err)

	h := Middleware(ks, "https://api/expected")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(w, req)
	assert.NotEqual(t, 401, w.Code)
}

func TestMiddleware_RequireAudience_Mismatch401(t *testing.T) {
	t.Parallel()
	ks := newKS(t)
	tok, err := ks.Mint(jwks.MintOpts{
		Subject: "abc@clients", Audience: []string{"https://api/other"}, TTL: time.Hour,
	})
	require.NoError(t, err)

	called := false
	h := Middleware(ks, "https://api/expected")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(w, req)
	assert.Equal(t, 401, w.Code)
	assert.False(t, called)
}

// TestMiddleware_RejectsAfterClockAdvance is the direct (not godog)
// proof that the bearer middleware shares its clock with the minter:
// mint a token, advance the controllable clock past its exp, then
// confirm the same middleware instance rejects it 401. Without this
// direct test the property is only covered transitively via the
// jwks-level Verify test and the Clock.feature end-to-end scenario.
func TestMiddleware_RejectsAfterClockAdvance(t *testing.T) {
	t.Parallel()
	var now time.Time
	ks, err := jwks.NewKeySet(jwks.Config{
		Issuer:         "https://mock/",
		AccessTokenTTL: time.Hour,
		Now:            func() time.Time { return now },
	})
	require.NoError(t, err)

	mintAt := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	now = mintAt
	tok, err := ks.Mint(jwks.MintOpts{
		Subject: "abc@clients", Audience: []string{"a"}, TTL: time.Hour,
	})
	require.NoError(t, err)

	called := false
	h := Middleware(ks, "")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

	// Within the TTL — middleware accepts.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(w, req)
	require.True(t, called, "middleware should accept the token while within TTL")
	assert.NotEqual(t, 401, w.Code)

	// Advance the clock past exp + leeway. Same token should now 401.
	now = mintAt.Add(time.Hour + 5*time.Minute)
	called = false
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(w, req)
	assert.Equal(t, 401, w.Code)
	assert.False(t, called, "middleware must reject expired token even though sig is valid")
}
