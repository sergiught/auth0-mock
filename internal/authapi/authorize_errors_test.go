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
)

func newAuthRouterWithErrors(t *testing.T, allowed ...string) (chi.Router, *authorizeerr.Store, *pkce.Store) {
	t.Helper()
	ks, err := jwks.NewKeySet(jwks.Config{
		Issuer: "https://mock/", AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour,
	})
	require.NoError(t, err)
	errs := authorizeerr.NewStore()
	p := pkce.NewStore()
	r := chi.NewRouter()
	Mount(Deps{
		Router: r, Keys: ks,
		Issuer: "https://mock/", DefaultAudience: "https://mock/api/v2/",
		Log:                          zerolog.Nop(),
		PKCE:                         p,
		AuthorizeErrors:              errs,
		AuthorizeAllowedRedirectURIs: allowed,
	})
	return r, errs, p
}

func getAuthorize(r chi.Router, rawQuery string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/authorize?"+rawQuery, nil))
	return w
}

func location(t *testing.T, w *httptest.ResponseRecorder) *url.URL {
	t.Helper()
	require.Equal(t, http.StatusFound, w.Code)
	u, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	return u
}

const cbQuery = "client_id=a&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=code"

func TestAuthorize_ImplicitDefaultsToFragment(t *testing.T) {
	r, _, _ := newAuthRouterWithErrors(t)
	loc := location(t, getAuthorize(r, "client_id=a&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=token&state=s1"))
	assert.False(t, loc.Query().Has("access_token"))
	frag, err := url.ParseQuery(loc.EscapedFragment())
	require.NoError(t, err)
	assert.NotEmpty(t, frag.Get("access_token"))
	assert.Equal(t, "Bearer", frag.Get("token_type"))
	assert.Equal(t, "s1", frag.Get("state"))
}

func TestAuthorize_CodeHonoursFragmentAndFormPost(t *testing.T) {
	r, _, _ := newAuthRouterWithErrors(t)
	loc := location(t, getAuthorize(r, cbQuery+"&response_mode=fragment"))
	frag, err := url.ParseQuery(loc.EscapedFragment())
	require.NoError(t, err)
	assert.NotEmpty(t, frag.Get("code"))
	assert.False(t, loc.Query().Has("code"))

	w := getAuthorize(r, cbQuery+"&response_mode=form_post")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `name="code"`)
}

func addDeniedRule(store *authorizeerr.Store, match map[string]string, desc string) {
	store.Add(authorizeerr.Rule{Match: match, Error: "access_denied", ErrorDescription: desc})
}

func TestAuthorize_RuleRedirectsError(t *testing.T) {
	r, errs, _ := newAuthRouterWithErrors(t)
	addDeniedRule(errs, map[string]string{"client_id": "a"}, "User cancelled")

	loc := location(t, getAuthorize(r, cbQuery+"&state=s1"))
	q := loc.Query()
	assert.Equal(t, "access_denied", q.Get("error"))
	assert.Equal(t, "User cancelled", q.Get("error_description"))
	assert.Equal(t, "s1", q.Get("state"))
	assert.False(t, q.Has("code"))

	other := location(t, getAuthorize(r, strings.Replace(cbQuery, "client_id=a", "client_id=b", 1)))
	assert.NotEmpty(t, other.Query().Get("code"))
}

func TestAuthorize_RuleErrorFollowsMode(t *testing.T) {
	r, errs, _ := newAuthRouterWithErrors(t)
	addDeniedRule(errs, map[string]string{"client_id": "a"}, "User cancelled")

	w := getAuthorize(r, cbQuery+"&response_mode=form_post")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `name="error"`)
	assert.Contains(t, w.Body.String(), "access_denied")

	loc := location(t, getAuthorize(r, strings.Replace(cbQuery, "response_type=code", "response_type=token", 1)))
	frag, err := url.ParseQuery(loc.EscapedFragment())
	require.NoError(t, err)
	assert.Equal(t, "access_denied", frag.Get("error"))
}

func TestAuthorize_RuleOmitsEmptyDescriptionAndState(t *testing.T) {
	r, errs, _ := newAuthRouterWithErrors(t)
	addDeniedRule(errs, nil, "")
	q := location(t, getAuthorize(r, cbQuery)).Query()
	assert.Equal(t, url.Values{"error": {"access_denied"}}, q)
}

func TestAuthorize_FailureStoresNoPKCE(t *testing.T) {
	r, errs, _ := newAuthRouterWithErrors(t)
	addDeniedRule(errs, nil, "")
	challenge := strings.Repeat("c", 43)
	q := location(t, getAuthorize(r, cbQuery+"&code_challenge="+challenge+"&code_challenge_method=S256")).Query()
	assert.Equal(t, "access_denied", q.Get("error"))
	assert.False(t, q.Has("code"))
}

func TestAuthorize_RuleNeverFiresOnBadRedirect(t *testing.T) {
	r, errs, _ := newAuthRouterWithErrors(t, "https://app/cb")
	addDeniedRule(errs, nil, "")

	w := getAuthorize(r, "client_id=a")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "missing redirect_uri")

	w = getAuthorize(r, "client_id=a&redirect_uri=https%3A%2F%2Fevil.tld")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "allow-list")
	assert.Empty(t, w.Header().Get("Location"))

	w = getAuthorize(r, "client_id=a&redirect_uri=%25zz")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid redirect_uri")
	assert.Empty(t, w.Header().Get("Location"))
}

func TestAuthorize_FormPostFallsBackForNonHTTPScheme(t *testing.T) {
	r, _, _ := newAuthRouterWithErrors(t)
	loc := location(t, getAuthorize(r, "client_id=a&redirect_uri=com.example.app%3A%2Fcb&response_type=code&response_mode=form_post"))
	assert.Equal(t, "com.example.app", loc.Scheme)
	assert.NotEmpty(t, loc.Query().Get("code"))
}
