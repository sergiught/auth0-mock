package authapi

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/session"
)

func TestSetLoginCookieHTTP(t *testing.T) {
	w := httptest.NewRecorder()
	setLoginCookie(w, httptest.NewRequest(http.MethodGet, "/authorize", nil), "abc")
	c := w.Result().Cookies()
	require.Len(t, c, 1)
	assert.Equal(t, "auth0", c[0].Name)
	assert.Equal(t, "abc", c[0].Value)
	assert.Equal(t, "/", c[0].Path)
	assert.True(t, c[0].HttpOnly)
	assert.False(t, c[0].Secure)
	assert.Equal(t, http.SameSiteLaxMode, c[0].SameSite)
}

func TestSetLoginCookieHTTPS(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/authorize", nil)
	r.TLS = &tls.ConnectionState{}
	w := httptest.NewRecorder()
	setLoginCookie(w, r, "abc")
	c := w.Result().Cookies()
	require.Len(t, c, 1)
	assert.True(t, c[0].Secure)
	assert.Equal(t, http.SameSiteNoneMode, c[0].SameSite)
}

func TestClearLoginCookie(t *testing.T) {
	w := httptest.NewRecorder()
	clearLoginCookie(w, httptest.NewRequest(http.MethodGet, "/v2/logout", nil))
	c := w.Result().Cookies()
	require.Len(t, c, 1)
	assert.Equal(t, "auth0", c[0].Name)
	assert.Equal(t, -1, c[0].MaxAge)
	assert.Contains(t, w.Header().Get("Set-Cookie"), "Max-Age=0")
}

func TestCurrentLogin(t *testing.T) {
	s := session.NewStore()
	t0 := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	id := s.StartLogin("auth0|a", t0)

	req := func(v string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/authorize", nil)
		if v != "" {
			r.AddCookie(&http.Cookie{Name: loginCookieName, Value: v})
		}
		return r
	}

	gotID, l, ok := currentLogin(req(id), s)
	assert.True(t, ok)
	assert.Equal(t, id, gotID)
	assert.Equal(t, "auth0|a", l.Subject)

	_, _, ok = currentLogin(req(""), s)
	assert.False(t, ok)
	_, _, ok = currentLogin(req("nope"), s)
	assert.False(t, ok)
	_, _, ok = currentLogin(req(id), nil)
	assert.False(t, ok)
}
