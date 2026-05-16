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

func TestAuthorize_RedirectsToCallback(t *testing.T) {
	r, _ := newAuthRouter(t)
	req := httptest.NewRequest("GET", "/authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&state=s1&response_type=code", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusFound, w.Code)
	loc := w.Header().Get("Location")
	assert.Contains(t, loc, "https://app/cb")
	assert.Contains(t, loc, "code=")
	assert.Contains(t, loc, "state=s1")
}

func TestAuthorize_MissingRedirectURI_400(t *testing.T) {
	r, _ := newAuthRouter(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/authorize?client_id=abc", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAuthorize_RejectsShortCodeChallenge(t *testing.T) {
	r, _ := newAuthRouter(t)
	// 42 chars — one short of RFC 7636 §4.1's lower bound (43).
	short := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOP"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET",
		"/authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=code"+
			"&code_challenge="+short+"&code_challenge_method=S256", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "code_challenge")
	assert.Contains(t, w.Body.String(), "RFC 7636")
}

func TestAuthorize_RejectsLongCodeChallenge(t *testing.T) {
	r, _ := newAuthRouter(t)
	long := strings.Repeat("x", 129) // 1 over RFC 7636 §4.1's upper bound (128).
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET",
		"/authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=code"+
			"&code_challenge="+long+"&code_challenge_method=S256", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUserinfo_ReturnsClaims(t *testing.T) {
	r, ks := newAuthRouter(t)
	tok, err := ks.Mint(jwks.MintOpts{
		Subject:  "auth0|123",
		Audience: []string{"a"},
		TTL:      time.Hour,
		Extra:    map[string]any{"email": "a@x", "name": "Alice"},
	})
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "auth0|123", body["sub"])
	assert.Equal(t, "a@x", body["email"])
	assert.Equal(t, "Alice", body["name"])
}

func TestUserinfo_NoBearer_401(t *testing.T) {
	r, _ := newAuthRouter(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/userinfo", nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestLogout_RedirectsToAllowedAbsoluteURL(t *testing.T) {
	// The router fixture wires LogoutAllowedURLs=["https://app/bye"], so
	// this returnTo is on the allow-list and gets 302'd.
	r, _ := newAuthRouter(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/v2/logout?returnTo=https%3A%2F%2Fapp%2Fbye", nil))
	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "https://app/bye", w.Header().Get("Location"))
}

func TestLogout_RejectsAbsoluteURLNotOnAllowList(t *testing.T) {
	// Open-redirect guard: an absolute URL not in LogoutAllowedURLs must
	// 400 instead of 302'ing the victim to attacker-controlled content.
	r, _ := newAuthRouter(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/v2/logout?returnTo=https%3A%2F%2Fevil.tld", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Empty(t, w.Header().Get("Location"))
	assert.Contains(t, w.Body.String(), "LOGOUT_ALLOWED_URLS")
}

func TestLogout_AllowsRelativeReturnTo(t *testing.T) {
	// Relative URLs can't escape the mock's origin so they need no allow-list.
	r, _ := newAuthRouter(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/v2/logout?returnTo=%2Fpost-logout", nil))
	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "/post-logout", w.Header().Get("Location"))
}

func TestLogout_DefaultsToSlashWhenReturnToMissing(t *testing.T) {
	r, _ := newAuthRouter(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/v2/logout", nil))
	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "/", w.Header().Get("Location"))
}

func TestLogout_RejectsDangerousSchemes(t *testing.T) {
	// `Host == ""` was treated as safe-because-relative before, but
	// these URLs all parse with empty host and a dangerous scheme:
	//   javascript:alert(1)  data:text/html,xx  mailto:a@b
	// Any of them would be reflected into Location and followed by
	// SDKs that don't pre-filter the scheme list.
	r, _ := newAuthRouter(t)
	for _, raw := range []string{
		"javascript:alert(1)",
		"data:text/html,xss",
		"mailto:a@b",
		"file:///etc/passwd",
	} {
		t.Run(raw, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/v2/logout?returnTo="+url.QueryEscape(raw), nil))
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Empty(t, w.Header().Get("Location"))
		})
	}
}

func TestLogout_RejectsBackslashBypass(t *testing.T) {
	// Browsers normalise `\` → `/` in Location, so "/\evil.tld"
	// resolves as "//evil.tld" (protocol-relative cross-origin).
	// The isAllowed check rejects any returnTo containing backslash
	// regardless of scheme so neither parser-quirk slips through.
	r, _ := newAuthRouter(t)
	for _, raw := range []string{`/\evil.tld`, `/\\evil.tld`, `\\evil.tld`} {
		t.Run(raw, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/v2/logout?returnTo="+url.QueryEscape(raw), nil))
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Empty(t, w.Header().Get("Location"))
		})
	}
}

func TestAuthorize_RejectsRedirectURINotOnAllowList(t *testing.T) {
	// When AllowedRedirectURIs is set, /authorize must reject any
	// absolute redirect_uri not on the list. Without this guard,
	// /authorize is an open-redirect that leaks the `code` /
	// `access_token` it appends to the URL to attacker-controlled hosts.
	ks, err := jwks.NewKeySet(jwks.Config{Issuer: "https://mock/", AccessTokenTTL: time.Hour})
	require.NoError(t, err)
	r := chi.NewRouter()
	Mount(Deps{
		Router: r, Keys: ks,
		Issuer: "https://mock/", DefaultAudience: "https://mock/api/v2/",
		Log:                          zerolog.Nop(),
		AuthorizeAllowedRedirectURIs: []string{"https://app/cb"},
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET",
		"/authorize?client_id=abc&redirect_uri=https%3A%2F%2Fevil.tld&response_type=token", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Empty(t, w.Header().Get("Location"))
	assert.Contains(t, w.Body.String(), "AUTHORIZE_ALLOWED_CALLBACKS")
}

func TestAuthorize_AllowsRedirectURIOnAllowList(t *testing.T) {
	ks, err := jwks.NewKeySet(jwks.Config{Issuer: "https://mock/", AccessTokenTTL: time.Hour})
	require.NoError(t, err)
	r := chi.NewRouter()
	Mount(Deps{
		Router: r, Keys: ks,
		Issuer: "https://mock/", DefaultAudience: "https://mock/api/v2/",
		Log:                          zerolog.Nop(),
		AuthorizeAllowedRedirectURIs: []string{"https://app/cb"},
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET",
		"/authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=code", nil))
	require.Equal(t, http.StatusFound, w.Code)
	assert.Contains(t, w.Header().Get("Location"), "https://app/cb")
}

func TestAuthorize_NoAllowListIsPermissive(t *testing.T) {
	// Default behaviour: when AllowedRedirectURIs is empty, /authorize
	// accepts any redirect_uri (the test-friendly documented default).
	// Closing the open redirect is opt-in via AUTHORIZE_ALLOWED_CALLBACKS.
	r, _ := newAuthRouter(t) // no allow-list configured
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET",
		"/authorize?client_id=abc&redirect_uri=https%3A%2F%2Fanywhere%2Fcb&response_type=code", nil))
	require.Equal(t, http.StatusFound, w.Code)
	assert.Contains(t, w.Header().Get("Location"), "https://anywhere/cb")
}

func TestRevoke_AlwaysReturns200(t *testing.T) {
	r, _ := newAuthRouter(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/oauth/revoke", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}
