package authapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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

func newSilentRouter(t *testing.T, sessions *session.Store) (chi.Router, *jwks.KeySet, *steppedClock, *authorizeerr.Store) {
	t.Helper()
	clk := &steppedClock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	ks, err := jwks.NewKeySet(jwks.Config{
		Issuer: "https://mock/", AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour, Now: clk.Now,
	})
	require.NoError(t, err)
	errs := authorizeerr.NewStore()
	r := chi.NewRouter()
	Mount(Deps{
		Router: r, Keys: ks, Issuer: "https://mock/", DefaultAudience: "https://mock/api/v2/",
		Log: zerolog.Nop(), PKCE: pkce.NewStore(pkce.WithNow(clk.Now)),
		Sessions: sessions, AuthorizeErrors: errs,
	})
	return r, ks, clk, errs
}

// silentAuthorize calls /authorize with the id_token flow by default.
func silentAuthorize(r chi.Router, cookie *http.Cookie, extra url.Values) *httptest.ResponseRecorder {
	q := url.Values{
		"client_id":     {"demo"},
		"redirect_uri":  {"https://app/cb"},
		"response_type": {"id_token"},
		"nonce":         {"n"},
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

func loginCookieOf(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == loginCookieName && c.MaxAge >= 0 {
			return c
		}
	}
	require.Fail(t, "no auth0 cookie set")
	return nil
}

func fragmentIDClaims(t *testing.T, ks *jwks.KeySet, w *httptest.ResponseRecorder) *jwks.Claims {
	t.Helper()
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	u, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	frag, err := url.ParseQuery(u.EscapedFragment())
	require.NoError(t, err)
	require.NotEmpty(t, frag.Get("id_token"), w.Header().Get("Location"))
	c, err := ks.Verify(frag.Get("id_token"), jwks.VerifyOpts{})
	require.NoError(t, err)
	return c
}

func requireLoginRequired(t *testing.T, w *httptest.ResponseRecorder, errCode string) {
	t.Helper()
	require.Equal(t, http.StatusFound, w.Code)
	u, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	frag, err := url.ParseQuery(u.EscapedFragment())
	require.NoError(t, err)
	assert.Equal(t, errCode, frag.Get("error"))
}

func aliceLogin(t *testing.T, r chi.Router) *http.Cookie {
	t.Helper()
	w := silentAuthorize(r, nil, url.Values{"login_hint": {"auth0|alice"}})
	return loginCookieOf(t, w)
}

func TestAuthorizeSetsLoginCookie(t *testing.T) {
	r, _, _, _ := newSilentRouter(t, session.NewStore())
	w := silentAuthorize(r, nil, url.Values{"response_type": {"code"}})
	assert.Contains(t, w.Header().Get("Set-Cookie"), "auth0=")
}

func TestAuthorizePromptNoneWithoutSession(t *testing.T) {
	r, _, _, _ := newSilentRouter(t, session.NewStore())
	w := silentAuthorize(r, nil, url.Values{"response_type": {"code"}, "prompt": {"none"}, "state": {"s1"}})
	require.Equal(t, http.StatusFound, w.Code)
	u, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "login_required", u.Query().Get("error"))
	assert.Equal(t, "Login required", u.Query().Get("error_description"))
	assert.Equal(t, "s1", u.Query().Get("state"))
	assert.Contains(t, w.Header().Get("Location"), "error_description=Login+required")
	assert.Empty(t, w.Header().Values("Set-Cookie"))
}

func TestAuthorizePromptNoneUsesSession(t *testing.T) {
	r, ks, clk, _ := newSilentRouter(t, session.NewStore())
	t0 := clk.Now()
	c := aliceLogin(t, r)
	clk.Advance(time.Hour)
	w := silentAuthorize(r, c, url.Values{"prompt": {"none"}})
	claims := fragmentIDClaims(t, ks, w)
	assert.Equal(t, "auth0|alice", claims.Subject)
	assert.EqualValues(t, t0.Unix(), claims.Extra["auth_time"])
	assert.Empty(t, w.Header().Values("Set-Cookie"))
}

func TestAuthorizePromptNoneCodeExchange(t *testing.T) {
	r, ks, clk, _ := newSilentRouter(t, session.NewStore())
	t0 := clk.Now()
	c := aliceLogin(t, r)
	clk.Advance(time.Hour)
	w := silentAuthorize(r, c, url.Values{"prompt": {"none"}, "response_type": {"code"}})
	require.Equal(t, http.StatusFound, w.Code)
	u, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	code := u.Query().Get("code")
	require.NotEmpty(t, code)
	claims := idTokenClaims(t, ks, exchangeCode(r, code, ""))
	assert.Equal(t, "auth0|alice", claims.Subject)
	assert.EqualValues(t, t0.Unix(), claims.Extra["auth_time"])
}

func TestAuthorizeSSOWithoutHint(t *testing.T) {
	r, ks, _, _ := newSilentRouter(t, session.NewStore())
	c := aliceLogin(t, r)
	w := silentAuthorize(r, c, nil)
	assert.Equal(t, "auth0|alice", fragmentIDClaims(t, ks, w).Subject)
	assert.Empty(t, w.Header().Values("Set-Cookie"))
}

func TestAuthorizeLoginHintReplacesSession(t *testing.T) {
	r, ks, _, _ := newSilentRouter(t, session.NewStore())
	old := aliceLogin(t, r)
	w := silentAuthorize(r, old, url.Values{"login_hint": {"auth0|bob"}})
	fresh := loginCookieOf(t, w)
	assert.NotEqual(t, old.Value, fresh.Value)
	assert.Equal(t, "auth0|bob", fragmentIDClaims(t, ks, silentAuthorize(r, fresh, url.Values{"prompt": {"none"}})).Subject)
	requireLoginRequired(t, silentAuthorize(r, old, url.Values{"prompt": {"none"}}), "login_required")
}

func TestAuthorizePromptLoginIgnoresSession(t *testing.T) {
	r, ks, _, _ := newSilentRouter(t, session.NewStore())
	old := aliceLogin(t, r)
	w := silentAuthorize(r, old, url.Values{"prompt": {"login"}})
	assert.Equal(t, DefaultSubject, fragmentIDClaims(t, ks, w).Subject)
	assert.NotEqual(t, old.Value, loginCookieOf(t, w).Value)
}

func TestAuthorizePromptNoneErrorRuleWins(t *testing.T) {
	r, _, _, errs := newSilentRouter(t, session.NewStore())
	c := aliceLogin(t, r)
	errs.Add(authorizeerr.Rule{Match: map[string]string{"prompt": "none"}, Error: "consent_required"})
	requireLoginRequired(t, silentAuthorize(r, c, url.Values{"prompt": {"none"}}), "consent_required")
}

func TestAuthorizeStaleCookie(t *testing.T) {
	r, ks, _, _ := newSilentRouter(t, session.NewStore())
	stale := &http.Cookie{Name: loginCookieName, Value: "unknown"}
	requireLoginRequired(t, silentAuthorize(r, stale, url.Values{"prompt": {"none"}}), "login_required")
	w := silentAuthorize(r, stale, nil)
	assert.Equal(t, DefaultSubject, fragmentIDClaims(t, ks, w).Subject)
	assert.NotEqual(t, "unknown", loginCookieOf(t, w).Value)
}

func TestAuthorizeNilSessions(t *testing.T) {
	r, ks, _, _ := newSilentRouter(t, nil)
	requireLoginRequired(t, silentAuthorize(r, nil, url.Values{"prompt": {"none"}}), "login_required")
	w := silentAuthorize(r, nil, nil)
	assert.Empty(t, w.Header().Values("Set-Cookie"))
	assert.Equal(t, DefaultSubject, fragmentIDClaims(t, ks, w).Subject)
	w = silentAuthorize(r, nil, url.Values{"login_hint": {"auth0|alice"}})
	assert.Equal(t, "auth0|alice", fragmentIDClaims(t, ks, w).Subject)
}
