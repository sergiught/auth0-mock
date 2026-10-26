package authapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/session"
)

func newLogoutRouterWithSessions(t *testing.T, store *session.Store) http.Handler {
	t.Helper()
	ks, err := jwks.NewKeySet(jwks.Config{Issuer: "https://mock/", AccessTokenTTL: time.Hour})
	require.NoError(t, err)
	r := chi.NewRouter()
	Mount(Deps{Router: r, Keys: ks, Issuer: "https://mock/", Log: zerolog.Nop(), Sessions: store})
	return r
}

func withLoginCookie(r *http.Request, id string) *http.Request {
	r.AddCookie(&http.Cookie{Name: loginCookieName, Value: id})
	return r
}

func requireClearedCookie(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	sc := w.Header().Values("Set-Cookie")
	require.Len(t, sc, 1)
	assert.Contains(t, sc[0], "auth0=;")
	assert.Contains(t, sc[0], "Max-Age=0")
}

func TestLogoutEndsSession(t *testing.T) {
	store := session.NewStore()
	id := store.StartLogin("auth0|u1", time.Now())
	r := newLogoutRouterWithSessions(t, store)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, withLoginCookie(httptest.NewRequest("GET", "/v2/logout?returnTo=https%3A%2F%2Fapp%2Fbye", nil), id))

	require.Equal(t, http.StatusFound, w.Code)
	requireClearedCookie(t, w)
	_, ok := store.Login(id)
	assert.False(t, ok)
}

func TestLogoutNilSessions(t *testing.T) {
	r := newLogoutRouterWithSessions(t, nil)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, withLoginCookie(httptest.NewRequest("GET", "/v2/logout?returnTo=https%3A%2F%2Fapp%2Fbye", nil), "whatever"))

	require.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "https://app/bye", w.Header().Get("Location"))
	requireClearedCookie(t, w)
}

func TestOIDCLogoutEndsSession(t *testing.T) {
	store := session.NewStore()
	r := newLogoutRouterWithSessions(t, store)

	t.Run("GET logged-out page", func(t *testing.T) {
		id := store.StartLogin("auth0|u1", time.Now())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, withLoginCookie(httptest.NewRequest("GET", "/oidc/logout", nil), id))
		require.Equal(t, http.StatusOK, w.Code)
		requireClearedCookie(t, w)
		_, ok := store.Login(id)
		assert.False(t, ok)
	})

	t.Run("POST redirect", func(t *testing.T) {
		id := store.StartLogin("auth0|u1", time.Now())
		form := url.Values{"client_id": {"demo"}, "post_logout_redirect_uri": {"https://app/bye"}}
		req := httptest.NewRequest("POST", "/oidc/logout", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, withLoginCookie(req, id))
		require.Equal(t, http.StatusFound, w.Code)
		requireClearedCookie(t, w)
		_, ok := store.Login(id)
		assert.False(t, ok)
	})
}

func TestOIDCLogoutErrorKeepsSession(t *testing.T) {
	store := session.NewStore()
	id := store.StartLogin("auth0|u1", time.Now())
	r := newLogoutRouterWithSessions(t, store)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, withLoginCookie(httptest.NewRequest("GET", "/oidc/logout?id_token_hint=garbage", nil), id))

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Empty(t, w.Header().Values("Set-Cookie"))
	_, ok := store.Login(id)
	assert.True(t, ok)
}
