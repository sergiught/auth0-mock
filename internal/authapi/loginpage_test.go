package authapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
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

func TestLoginPage_OffNotMounted(t *testing.T) {
	r, _, _, _ := newLoginPageRouter(t, false)
	assert.Equal(t, http.StatusNotFound, getLoginPage(r, "x").Code)
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
		`<h1>Log in</h1>`,
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

func TestLoginPage_EscapesHostileValues(t *testing.T) {
	r, _, _, _ := newLoginPageRouter(t, true)
	id := pageID(t, loginPageAuthorize(r, nil, url.Values{
		"login_hint": {`"><script>x</script>`},
		"client_id":  {`<b>evil</b>`},
	}))
	body := getLoginPage(r, id).Body.String()
	assert.NotContains(t, body, "<script>x</script>")
	assert.NotContains(t, body, "<b>evil</b>")
	assert.Contains(t, body, "&lt;b&gt;evil&lt;/b&gt;")
	assert.Contains(t, body, "&#34;&gt;&lt;script&gt;")
}

const pkceVerifier = "v1v1v1v1v1v1v1v1v1v1v1v1v1v1v1v1v1v1v1v1v1v1"

func s256(v string) string {
	sum := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func codeQuery(extra url.Values) url.Values {
	q := url.Values{
		"nonce":                 {"n1"},
		"code_challenge":        {s256(pkceVerifier)},
		"code_challenge_method": {"S256"},
		"scope":                 {"openid"},
		"state":                 {"app-xyz"},
	}
	for k, v := range extra {
		q[k] = v
	}
	return q
}

func postLogin(r chi.Router, id, username, password string) *httptest.ResponseRecorder {
	form := url.Values{"username": {username}, "password": {password}, "action": {"default"}}
	req := httptest.NewRequest("POST", "/u/login?state="+url.QueryEscape(id), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func redirectCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "https://app/cb", loc.Scheme+"://"+loc.Host+loc.Path)
	assert.Equal(t, "app-xyz", loc.Query().Get("state"))
	require.NotEmpty(t, loc.Query().Get("code"))
	return loc.Query().Get("code")
}

func rawClaims(t *testing.T, tok string) map[string]any {
	t.Helper()
	parts := strings.Split(tok, ".")
	require.Len(t, parts, 3)
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	return m
}

func loginExchange(t *testing.T, r chi.Router, code string) (access, id map[string]any) {
	t.Helper()
	w := exchangeCode(r, code, pkceVerifier)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp tokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return rawClaims(t, resp.AccessToken), rawClaims(t, resp.IDToken)
}

func withoutTimes(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		if k != "iat" && k != "exp" && k != "auth_time" && k != "jti" {
			out[k] = v
		}
	}
	return out
}

func TestLoginPage_PostIssuesCode(t *testing.T) {
	r, _, _, _ := newLoginPageRouter(t, true)
	id := pageID(t, loginPageAuthorize(r, nil, codeQuery(nil)))
	w := postLogin(r, id, " jane@example.com ", "pw")
	code := redirectCode(t, w)
	assert.Contains(t, w.Header().Get("Set-Cookie"), loginCookieName+"=")
	_, idc := loginExchange(t, r, code)
	assert.Equal(t, "jane@example.com", idc["sub"])
	assert.Equal(t, "n1", idc["nonce"])
}

func TestLoginPage_MatchesLoginHint(t *testing.T) {
	r, _, _, _ := newLoginPageRouter(t, true)
	id := pageID(t, loginPageAuthorize(r, nil, codeQuery(nil)))
	_, viaPage := loginExchange(t, r, redirectCode(t, postLogin(r, id, "jane@example.com", "pw")))

	off, _, _, _ := newLoginPageRouter(t, false)
	w := loginPageAuthorize(off, nil, codeQuery(url.Values{"login_hint": {"jane@example.com"}}))
	_, viaHint := loginExchange(t, off, redirectCode(t, w))
	assert.Equal(t, withoutTimes(viaHint), withoutTimes(viaPage))
}

func TestLoginPage_FormPostImplicit(t *testing.T) {
	r, _, _, _ := newLoginPageRouter(t, true)
	id := pageID(t, loginPageAuthorize(r, nil, url.Values{
		"response_type": {"id_token token"}, "response_mode": {"form_post"}, "nonce": {"n2"},
	}))
	w := postLogin(r, id, "jane@example.com", "pw")
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "https://app/cb")
	assert.Contains(t, body, `name="access_token"`)
	idTok := regexpValue(t, body, "id_token")
	c := rawClaims(t, idTok)
	assert.Equal(t, "jane@example.com", c["sub"])
	assert.Equal(t, "n2", c["nonce"])
}

func regexpValue(t *testing.T, body, name string) string {
	t.Helper()
	m := regexp.MustCompile(`name="` + name + `" value="([^"]*)"`).FindStringSubmatch(body)
	require.Len(t, m, 2, body)
	return m[1]
}

func TestLoginPage_Organization(t *testing.T) {
	r, _, _, _ := newLoginPageRouter(t, true)
	id := pageID(t, loginPageAuthorize(r, nil, codeQuery(url.Values{"organization": {"acme"}})))
	viaPage, _ := loginExchange(t, r, redirectCode(t, postLogin(r, id, "jane@example.com", "pw")))

	off, _, _, _ := newLoginPageRouter(t, false)
	auto, _ := loginExchange(t, off, redirectCode(t, loginPageAuthorize(off, nil, codeQuery(url.Values{"organization": {"acme"}}))))
	assert.Equal(t, "acme", viaPage["org_name"])
	assert.NotEmpty(t, viaPage["org_id"])
	assert.Equal(t, auto["org_id"], viaPage["org_id"])
	assert.Equal(t, auto["org_name"], viaPage["org_name"])
}

func TestLoginPage_EmptyFields(t *testing.T) {
	r, _, _, _ := newLoginPageRouter(t, true)
	id := pageID(t, loginPageAuthorize(r, nil, codeQuery(nil)))
	w := postLogin(r, id, "", "pw")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Email is required")
	w = postLogin(r, id, "jane@example.com", "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Password is required")
	assert.Contains(t, w.Body.String(), `value="jane@example.com"`)
	redirectCode(t, postLogin(r, id, "jane@example.com", "pw"))
}

func TestLoginPage_FailureRule(t *testing.T) {
	r, _, _, errs := newLoginPageRouter(t, true)
	errs.Add(authorizeerr.Rule{
		Match: map[string]string{"username": "locked@example.com"}, Error: "access_denied",
		ErrorDescription: "Wrong email or password.",
	})
	id := pageID(t, loginPageAuthorize(r, nil, codeQuery(nil)))
	w := postLogin(r, id, "locked@example.com", "pw")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Wrong email or password.")
	assert.Contains(t, w.Body.String(), `value="locked@example.com"`)
	redirectCode(t, postLogin(r, id, "jane@example.com", "pw"))

	errs.Clear()
	errs.Add(authorizeerr.Rule{Match: map[string]string{"username": "locked@example.com"}, Error: "access_denied"})
	id = pageID(t, loginPageAuthorize(r, nil, codeQuery(nil)))
	w = postLogin(r, id, "locked@example.com", "pw")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Wrong email or password.")
}

func TestLoginPage_ReusedState(t *testing.T) {
	r, _, _, _ := newLoginPageRouter(t, true)
	id := pageID(t, loginPageAuthorize(r, nil, codeQuery(nil)))
	redirectCode(t, postLogin(r, id, "jane@example.com", "pw"))
	w := postLogin(r, id, "jane@example.com", "pw")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid or expired login")
}

func TestLoginPage_SessionThenSilent(t *testing.T) {
	r, _, _, _ := newLoginPageRouter(t, true)
	id := pageID(t, loginPageAuthorize(r, nil, codeQuery(nil)))
	w := postLogin(r, id, "jane@example.com", "pw")
	redirectCode(t, w)
	cookies := w.Result().Cookies()
	require.NotEmpty(t, cookies)

	w = loginPageAuthorize(r, cookies[0], codeQuery(url.Values{"prompt": {"none"}}))
	_, idc := loginExchange(t, r, redirectCode(t, w))
	assert.Equal(t, "jane@example.com", idc["sub"])
}

// A path-relative redirect_uri must land where it would from /authorize, not
// from /u/login one level deeper, with or without a namespace prefix.
func TestLoginPage_RelativeRedirectResolvesLikeAuthorize(t *testing.T) {
	for _, tc := range []struct{ redirect, prefix, want string }{
		{"cb", "/", "/cb"},
		{"cb", "/ns/t1/", "/ns/t1/cb"},
		{"./app/cb", "/ns/t1/", "/ns/t1/app/cb"},
		{"../cb", "/ns/t1/", "/ns/cb"},
		{"?x=1", "/ns/t1/", "/ns/t1/authorize"},
		{"/cb", "/ns/t1/", "/cb"},
	} {
		t.Run(tc.redirect+" under "+tc.prefix, func(t *testing.T) {
			r, _, _, _ := newLoginPageRouter(t, true)
			id := pageID(t, loginPageAuthorize(r, nil, url.Values{"redirect_uri": {tc.redirect}}))
			w := postLogin(r, id, "jane@example.com", "pw")
			require.Equal(t, http.StatusFound, w.Code, w.Body.String())
			page, err := url.Parse(tc.prefix + "u/login?state=x")
			require.NoError(t, err)
			loc, err := url.Parse(w.Header().Get("Location"))
			require.NoError(t, err)
			got := page.ResolveReference(loc)
			assert.Equal(t, tc.want, got.Path)
			assert.NotEmpty(t, got.Query().Get("code"))
			assert.Equal(t, "s1", got.Query().Get("state"))
		})
	}
}
