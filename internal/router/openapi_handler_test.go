package router_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/sergiught/auth0-mock/api"
	"github.com/sergiught/auth0-mock/internal/router"
)

func newOpenAPIRouter(t *testing.T) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	require.NoError(t, router.MountOpenAPI(r))
	return r
}

func TestOpenAPIJSONServesEmbeddedSpec(t *testing.T) {
	h := newOpenAPIRouter(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	body, _ := io.ReadAll(rec.Body)
	assert.Equal(t, api.MockOpenAPIJSON, body)
}

func TestDocsServesScalarHTML(t *testing.T) {
	h := newOpenAPIRouter(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	body := rec.Body.String()
	assert.Contains(t, body, `<html lang="en">`)
	// Scalar bundle must be pinned to an exact version and SRI-guarded — the
	// page mints a real bearer token, so unpinned third-party JS is not OK.
	assert.Contains(t, body, "@scalar/api-reference@1.55.3/dist/browser/standalone.min.js")
	assert.Contains(t, body, `integrity="sha384-`)
	assert.Contains(t, body, `crossorigin="anonymous"`)
	// Fallback when the CDN bundle fails to load.
	assert.Contains(t, body, "typeof Scalar === 'undefined'")
	assert.Contains(t, body, "Scalar.createApiReference('#app'")
	assert.Contains(t, body, "url: '/openapi.json'")
	assert.Contains(t, body, "theme: 'none'")
	assert.Contains(t, body, "layout: 'modern'")
	assert.Contains(t, body, "withDefaultFonts: false")
	assert.Contains(t, body, "hideClientButton: true")
	assert.Contains(t, body, "hideModels: true")
	assert.Contains(t, body, "showDeveloperTools: 'never'",
		"Scalar's navbar dev-tools show on localhost by default — must be off")
	assert.Contains(t, body, "defaultHttpClient: { targetKey: 'shell', clientKey: 'curl' }")
	// Curated code-snippet clients: a denylist that keeps curl, python
	// requests, go, rust, java okhttp, js axios, php guzzle.
	assert.Contains(t, body, "hiddenClients: {")
	assert.Contains(t, body, "ruby: true")
	assert.Contains(t, body, "js: ['fetch', 'jquery', 'ofetch', 'xhr']")
	assert.Contains(t, body, "php: ['curl', 'laravel']")
	assert.Contains(t, body, "agent: { disabled: true }",
		"Scalar Agent (Ask AI) must stay disabled so the spec isn't uploaded")
	assert.Contains(t, body, "prefers-color-scheme: dark",
		"darkMode must follow the OS via prefers-color-scheme, not be hardcoded")
	assert.Contains(t, body, "POST",
		"docs must POST to /oauth/token to preload a bearer for Try-it")
	assert.Contains(t, body, "/oauth/token")
	assert.Contains(t, body, "grant_type=client_credentials")
	assert.Contains(t, body, "preferredSecurityScheme: 'bearerAuth'")
}

func TestDocsServesStylesheet(t *testing.T) {
	h := newOpenAPIRouter(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs/docs.css", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/css")
	body := rec.Body.String()
	assert.Contains(t, body, "@font-face")
	assert.Contains(t, body, "Geist")
}

func TestDocsServesFonts(t *testing.T) {
	h := newOpenAPIRouter(t)
	for _, name := range []string{"Geist-Variable.woff2", "GeistMono-Variable.woff2"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs/fonts/"+name, nil))
		require.Equalf(t, http.StatusOK, rec.Code, "font %s", name)
		assert.Equalf(t, "font/woff2", rec.Header().Get("Content-Type"), "font %s", name)
		assert.NotEmptyf(t, rec.Body.Bytes(), "font %s body", name)
	}
}

func TestDocsFontRejectsUnknownFile(t *testing.T) {
	h := newOpenAPIRouter(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs/fonts/nope.woff2", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDocsFontRejectsPathTraversal(t *testing.T) {
	h := newOpenAPIRouter(t)
	// A percent-encoded traversal attempt must not escape docs/fonts/ and
	// serve, e.g., the embedded index.html. path.Base in serveDocsFont is the
	// guard (chi's routing also rejects a {file} segment containing a slash).
	for _, target := range []string{
		"/docs/fonts/%2e%2e%2findex.html",
		"/docs/fonts/..%2findex.html",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		assert.NotEqualf(t, http.StatusOK, rec.Code, "traversal %q must not succeed", target)
		assert.NotContainsf(t, rec.Body.String(), "<!doctype html>", "traversal %q leaked HTML", target)
	}
}

func TestDocsRendersCustomHeader(t *testing.T) {
	h := newOpenAPIRouter(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `<link rel="stylesheet" href="/docs/docs.css"`)
	assert.Contains(t, body, `class="docs-header"`)
	assert.Contains(t, body, `class="docs-header__wordmark">auth0-mock<`) // wordmark
	assert.Contains(t, body, ">MOCK<")                             // badge
	assert.Contains(t, body, "github.com/sergiught/auth0-mock")    // repo link
	assert.Contains(t, body, `id="theme-toggle"`)                  // toggle button
}

func TestDocsThemeToggleWiring(t *testing.T) {
	h := newOpenAPIRouter(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	// Scalar's built-in toggle is hidden — the header bar owns the control.
	assert.Contains(t, body, "hideDarkModeToggle: true")
	// Preference is persisted across reloads via localStorage.
	assert.Contains(t, body, "auth0-mock-docs-theme")
	assert.Contains(t, body, "localStorage")
	// The header toggle button has a click handler.
	assert.Contains(t, body, "addEventListener('click'")
	// The toggle drives Scalar's live re-theming, not just the header CSS.
	assert.Contains(t, body, "updateConfiguration")
}

func TestOpenAPIYAMLRoundTripsToJSON(t *testing.T) {
	h := newOpenAPIRouter(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/yaml", rec.Header().Get("Content-Type"))

	var parsed map[string]any
	require.NoError(t, yaml.Unmarshal(rec.Body.Bytes(), &parsed))
	jsonBytes, err := json.Marshal(parsed)
	require.NoError(t, err)
	assert.Contains(t, string(jsonBytes), `"/healthz"`)
}
