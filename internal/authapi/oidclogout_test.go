package authapi

import (
	"encoding/json"
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
)

const bye = "https%3A%2F%2Fapp%2Fbye"

func logoutGet(t *testing.T, r http.Handler, query string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/oidc/logout?"+query, nil))
	return w
}

func mintHint(t *testing.T, ks *jwks.KeySet, ttl time.Duration, extra map[string]any) string {
	t.Helper()
	tok, err := ks.Mint(jwks.MintOpts{Subject: "auth0|u1", Audience: []string{"demo"}, TTL: ttl, Extra: extra})
	require.NoError(t, err)
	return url.QueryEscape(tok)
}

func requireLogoutError(t *testing.T, w *httptest.ResponseRecorder, description string) {
	t.Helper()
	require.Equal(t, http.StatusBadRequest, w.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "invalid_request", body["error"])
	assert.Equal(t, description, body["error_description"])
}

func TestOIDCLogout_RedirectsWithState(t *testing.T) {
	r, ks := newAuthRouter(t)
	w := logoutGet(t, r, "id_token_hint="+mintHint(t, ks, time.Hour, nil)+"&post_logout_redirect_uri="+bye+"&state=s1")
	require.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "https://app/bye?state=s1", w.Header().Get("Location"))
}

func TestOIDCLogout_ClientIDAloneIsEnough(t *testing.T) {
	r, _ := newAuthRouter(t)
	w := logoutGet(t, r, "client_id=demo&post_logout_redirect_uri="+bye)
	require.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "https://app/bye", w.Header().Get("Location"))
}

func TestOIDCLogout_StateMergesIntoExistingQuery(t *testing.T) {
	ks, err := jwks.NewKeySet(jwks.Config{Issuer: "https://mock/", AccessTokenTTL: time.Hour})
	require.NoError(t, err)
	r := chi.NewRouter()
	Mount(Deps{Router: r, Keys: ks, Issuer: "https://mock/", Log: zerolog.Nop()})

	w := logoutGet(t, r, "client_id=demo&state=s1&post_logout_redirect_uri="+url.QueryEscape("https://app/bye?x=1#frag"))
	require.Equal(t, http.StatusFound, w.Code)
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "1", loc.Query().Get("x"))
	assert.Equal(t, "s1", loc.Query().Get("state"))
	assert.Equal(t, "frag", loc.Fragment)
}

func TestOIDCLogout_NoRedirectRendersLoggedOutPage(t *testing.T) {
	r, _ := newAuthRouter(t)
	w := logoutGet(t, r, "")
	require.Equal(t, http.StatusOK, w.Code)
	assert.True(t, strings.HasPrefix(w.Header().Get("Content-Type"), "text/html"))
	assert.Contains(t, w.Body.String(), "You have been logged out.")
}

func TestOIDCLogout_RedirectNeedsClient(t *testing.T) {
	r, _ := newAuthRouter(t)
	requireLogoutError(t, logoutGet(t, r, "post_logout_redirect_uri="+bye),
		"post_logout_redirect_uri requires client_id or id_token_hint")
}

func TestOIDCLogout_RejectsAccessTokenHint(t *testing.T) {
	r, ks := newAuthRouter(t)
	hint := mintHint(t, ks, time.Hour, map[string]any{"gty": "client-credentials"})
	requireLogoutError(t, logoutGet(t, r, "id_token_hint="+hint),
		"id_token_hint is an access token; send the id_token instead")
}

func TestOIDCLogout_RejectsGarbageHint(t *testing.T) {
	r, _ := newAuthRouter(t)
	w := logoutGet(t, r, "id_token_hint=not-a-jwt")
	require.Equal(t, http.StatusBadRequest, w.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.True(t, strings.HasPrefix(body["error_description"], "id_token_hint is not a valid id_token issued by this mock: "))
}

func TestOIDCLogout_RejectsHintForOtherClient(t *testing.T) {
	r, ks := newAuthRouter(t)
	requireLogoutError(t, logoutGet(t, r, "client_id=other&id_token_hint="+mintHint(t, ks, time.Hour, nil)),
		"id_token_hint was issued to a different client than client_id")
}

func TestOIDCLogout_AcceptsExpiredHint(t *testing.T) {
	r, ks := newAuthRouter(t)
	w := logoutGet(t, r, "id_token_hint="+mintHint(t, ks, -time.Hour, nil)+"&post_logout_redirect_uri="+bye)
	require.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "https://app/bye", w.Header().Get("Location"))
}

func TestOIDCLogout_RejectsUnlistedRedirect(t *testing.T) {
	r, _ := newAuthRouter(t)
	requireLogoutError(t, logoutGet(t, r, "client_id=demo&post_logout_redirect_uri=https%3A%2F%2Fevil.tld"),
		"post_logout_redirect_uri is not in the configured allow-list (LOGOUT_ALLOWED_URLS); add it there to permit this redirect")
}

func TestOIDCLogout_PostForm(t *testing.T) {
	r, _ := newAuthRouter(t)
	req := httptest.NewRequest("POST", "/oidc/logout",
		strings.NewReader("client_id=demo&post_logout_redirect_uri="+bye+"&state=s1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "https://app/bye?state=s1", w.Header().Get("Location"))
}

func newPermissiveLogoutRouter(t *testing.T) http.Handler {
	t.Helper()
	ks, err := jwks.NewKeySet(jwks.Config{Issuer: "https://mock/", AccessTokenTTL: time.Hour})
	require.NoError(t, err)
	r := chi.NewRouter()
	Mount(Deps{Router: r, Keys: ks, Issuer: "https://mock/", Log: zerolog.Nop()})
	return r
}

func TestOIDCLogout_StateKeepsExistingQueryVerbatim(t *testing.T) {
	r := newPermissiveLogoutRouter(t)
	w := logoutGet(t, r, "client_id=demo&state=s1&post_logout_redirect_uri="+url.QueryEscape("https://app/bye?b=2&a=1&flag#frag"))
	require.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "https://app/bye?b=2&a=1&flag&state=s1#frag", w.Header().Get("Location"))
}

func TestOIDCLogout_RejectsUnparseableRedirect(t *testing.T) {
	r := newPermissiveLogoutRouter(t)
	requireLogoutError(t, logoutGet(t, r, "client_id=demo&post_logout_redirect_uri=https%3A%2F%2Fapp%2F%25zz"),
		"post_logout_redirect_uri is not a valid URL")
}

func TestOIDCLogout_HintWithMatchingClientID(t *testing.T) {
	r, ks := newAuthRouter(t)
	w := logoutGet(t, r, "client_id=demo&id_token_hint="+mintHint(t, ks, time.Hour, nil)+"&post_logout_redirect_uri="+bye)
	require.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "https://app/bye", w.Header().Get("Location"))
}

func TestOIDCLogout_ValidHintNoRedirectRendersPage(t *testing.T) {
	r, ks := newAuthRouter(t)
	w := logoutGet(t, r, "id_token_hint="+mintHint(t, ks, time.Hour, nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, loggedOutPage, w.Body.String())
}

func TestOIDCLogout_PostFormHint(t *testing.T) {
	r, ks := newAuthRouter(t)
	req := httptest.NewRequest("POST", "/oidc/logout",
		strings.NewReader("id_token_hint="+mintHint(t, ks, time.Hour, nil)+"&post_logout_redirect_uri="+bye))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "https://app/bye", w.Header().Get("Location"))
}

func TestOIDCLogout_MalformedFormBody(t *testing.T) {
	r, _ := newAuthRouter(t)
	req := httptest.NewRequest("POST", "/oidc/logout", strings.NewReader("%zz"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.True(t, strings.HasPrefix(body["error_description"], "invalid form body: "))
}
