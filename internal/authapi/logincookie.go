package authapi

import (
	"net/http"

	"github.com/sergiught/auth0-mock/internal/session"
)

// loginCookieName is the cookie Auth0 uses for its login session.
const loginCookieName = "auth0"

// setLoginCookie sends the login session id. Over TLS it is SameSite=None
// and Secure so cross-site silent-auth iframes carry it; browsers reject
// SameSite=None without Secure, so plain HTTP falls back to Lax.
func setLoginCookie(w http.ResponseWriter, r *http.Request, id string) {
	http.SetCookie(w, loginCookie(r, id))
}

// clearLoginCookie expires the login session cookie.
func clearLoginCookie(w http.ResponseWriter, r *http.Request) {
	c := loginCookie(r, "") //nolint:gosec // Secure follows the request scheme; plain HTTP cannot carry Secure cookies
	c.MaxAge = -1
	http.SetCookie(w, c)
}

func loginCookie(r *http.Request, value string) *http.Cookie {
	c := &http.Cookie{Name: loginCookieName, Value: value, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode}
	if r.TLS != nil {
		c.Secure = true
		c.SameSite = http.SameSiteNoneMode
	}
	return c
}

// currentLogin resolves the request's login cookie to a live session.
func currentLogin(r *http.Request, s *session.Store) (string, session.Login, bool) {
	if s == nil {
		return "", session.Login{}, false
	}
	c, err := r.Cookie(loginCookieName)
	if err != nil {
		return "", session.Login{}, false
	}
	l, ok := s.Login(c.Value)
	if !ok {
		return "", session.Login{}, false
	}
	return c.Value, l, true
}
