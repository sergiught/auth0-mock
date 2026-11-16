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

	"github.com/sergiught/auth0-mock/internal/authorizeerr"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/pkce"
	"github.com/sergiught/auth0-mock/internal/session"
)

func newLoginPageRouter(t *testing.T, on bool) (chi.Router, *session.Store, *steppedClock, *authorizeerr.Store) {
	t.Helper()
	clk := &steppedClock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	ks, err := jwks.NewKeySet(jwks.Config{
		Issuer: "https://mock/", AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour, Now: clk.Now,
	})
	require.NoError(t, err)
	sessions := session.NewStore()
	errs := authorizeerr.NewStore()
	r := chi.NewRouter()
	Mount(Deps{
		Router: r, Keys: ks, Issuer: "https://mock/", DefaultAudience: "https://mock/api/v2/",
		Log: zerolog.Nop(), PKCE: pkce.NewStore(pkce.WithNow(clk.Now)),
		Sessions: sessions, AuthorizeErrors: errs, LoginPage: on,
	})
	return r, sessions, clk, errs
}

func loginPageAuthorize(r chi.Router, cookie *http.Cookie, extra url.Values) *httptest.ResponseRecorder {
	q := url.Values{
		"client_id":     {"app"},
		"redirect_uri":  {"https://app/cb"},
		"response_type": {"code"},
		"state":         {"s1"},
	}
	for k, v := range extra {
		q[k] = v
	}
	req := httptest.NewRequest("GET", "/authorize?"+q.Encode(), nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func getLoginPage(r chi.Router, id string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/u/login?state="+url.QueryEscape(id), nil))
	return w
}

func TestLoginPage_OffUnchanged(t *testing.T) {
	r, _, _, _ := newLoginPageRouter(t, false)
	w := loginPageAuthorize(r, nil, nil)
	require.Equal(t, http.StatusFound, w.Code)
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "https://app/cb", loc.Scheme+"://"+loc.Host+loc.Path)
	assert.NotEmpty(t, loc.Query().Get("code"))
	assert.Equal(t, "s1", loc.Query().Get("state"))
}

func TestLoginPage_RedirectsWhenLoggedOut(t *testing.T) {
	r, _, _, _ := newLoginPageRouter(t, true)
	w := loginPageAuthorize(r, nil, nil)
	require.Equal(t, http.StatusFound, w.Code)
	loc := w.Header().Get("Location")
	require.True(t, strings.HasPrefix(loc, "u/login?state="), loc)
	assert.NotEqual(t, "s1", strings.TrimPrefix(loc, "u/login?state="))
}

func pageID(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	require.Equal(t, http.StatusFound, w.Code)
	loc := w.Header().Get("Location")
	require.True(t, strings.HasPrefix(loc, "u/login?state="), loc)
	id, err := url.QueryUnescape(strings.TrimPrefix(loc, "u/login?state="))
	require.NoError(t, err)
	return id
}

func TestLoginPage_GetRendersForm(t *testing.T) {
	r, _, _, _ := newLoginPageRouter(t, true)
	id := pageID(t, loginPageAuthorize(r, nil, url.Values{"login_hint": {"jane@example.com"}}))
	w := getLoginPage(r, id)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))
	body := w.Body.String()
	for _, want := range []string{
		`<input id="username" name="username" type="text"`,
		`<input id="password" name="password" type="password"`,
		`<button type="submit" name="action" value="default">Continue</button>`,
		`method="POST"`,
		`Log in`,
		`to continue to app`,
		`value="jane@example.com"`,
	} {
		assert.Contains(t, body, want)
	}
}

func TestLoginPage_UnknownState(t *testing.T) {
	r, _, clk, _ := newLoginPageRouter(t, true)
	w := getLoginPage(r, "nope")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid or expired login")

	id := pageID(t, loginPageAuthorize(r, nil, nil))
	clk.Advance(10 * time.Minute)
	assert.Equal(t, http.StatusBadRequest, getLoginPage(r, id).Code)
}

func TestLoginPage_PromptNoneUnchanged(t *testing.T) {
	r, _, _, _ := newLoginPageRouter(t, true)
	w := loginPageAuthorize(r, nil, url.Values{"prompt": {"none"}})
	require.Equal(t, http.StatusFound, w.Code)
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "https://app/cb", loc.Scheme+"://"+loc.Host+loc.Path)
	assert.Equal(t, "login_required", loc.Query().Get("error"))
}

func TestLoginPage_LiveSessionReused(t *testing.T) {
	r, sessions, clk, _ := newLoginPageRouter(t, true)
	id := sessions.StartLogin("auth0|jane", clk.Now())
	cookie := &http.Cookie{Name: loginCookieName, Value: id}

	w := loginPageAuthorize(r, cookie, nil)
	assert.Contains(t, w.Header().Get("Location"), "https://app/cb?code=")

	w = loginPageAuthorize(r, cookie, url.Values{"prompt": {"login"}})
	assert.True(t, strings.HasPrefix(w.Header().Get("Location"), "u/login?state="))

	w = loginPageAuthorize(r, cookie, url.Values{"login_hint": {"other@example.com"}})
	assert.True(t, strings.HasPrefix(w.Header().Get("Location"), "u/login?state="))

	w = loginPageAuthorize(r, cookie, url.Values{"login_hint": {"auth0|jane"}})
	assert.Contains(t, w.Header().Get("Location"), "https://app/cb?code=")
}

func TestLoginPage_ClientRuleStillFailsAtAuthorize(t *testing.T) {
	r, _, _, errs := newLoginPageRouter(t, true)
	errs.Add(authorizeerr.Rule{
		Match: map[string]string{"client_id": "app"}, Error: "access_denied", ErrorDescription: "Blocked",
	})
	w := loginPageAuthorize(r, nil, nil)
	require.Equal(t, http.StatusFound, w.Code)
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "https://app/cb", loc.Scheme+"://"+loc.Host+loc.Path)
	assert.Equal(t, "access_denied", loc.Query().Get("error"))
}
