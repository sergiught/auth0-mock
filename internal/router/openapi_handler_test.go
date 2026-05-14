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
	assert.Contains(t, body, "@scalar/api-reference")
	assert.Contains(t, body, "Scalar.createApiReference('#app'")
	assert.Contains(t, body, "url: '/openapi.json'")
	assert.Contains(t, body, "theme: 'fastify'")
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
