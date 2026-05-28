package router

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/sergiught/auth0-mock/api"
)

// openapiYAML converts the embedded spec to YAML once, lazily, and caches the
// result (value + error) for every subsequent /openapi.yaml request.
var openapiYAML = sync.OnceValues(func() ([]byte, error) {
	doc, err := openapi3.NewLoader().LoadFromData(api.MockOpenAPIJSON)
	if err != nil {
		return nil, fmt.Errorf("parse embedded spec: %w", err)
	}
	body, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("marshal yaml: %w", err)
	}
	return body, nil
})

// MountOpenAPI registers `GET /openapi.json`, `GET /openapi.yaml`, and the
// `/docs` API reference page (with its static assets) on r. All endpoints are
// unauthenticated.
func MountOpenAPI(r chi.Router) error {
	r.Method(http.MethodGet, "/openapi.json", http.HandlerFunc(serveOpenAPIJSON))
	r.Method(http.MethodGet, "/openapi.yaml", http.HandlerFunc(serveOpenAPIYAML))
	mountDocs(r)
	return nil
}

func serveOpenAPIJSON(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(api.MockOpenAPIJSON)
}

func serveOpenAPIYAML(w http.ResponseWriter, _ *http.Request) {
	body, err := openapiYAML()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(body)
}
