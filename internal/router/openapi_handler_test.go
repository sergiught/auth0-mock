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
	assert.Contains(t, body, `data-url="/openapi.json"`)
	assert.Contains(t, body, "@scalar/api-reference")
	assert.Contains(t, body, `"agent":{"disabled":true}`,
		"Scalar Agent (Ask AI) must stay disabled so the spec isn't uploaded")
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
