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

var openapiBytes struct {
	once sync.Once
	yaml []byte
	err  error
}

// MountOpenAPI registers `GET /openapi.json`, `GET /openapi.yaml`, and
// `GET /docs` on r, serving the embedded merged spec and a Scalar-rendered
// HTML reference. All three endpoints are unauthenticated.
func MountOpenAPI(r chi.Router) error {
	r.Method(http.MethodGet, "/openapi.json", http.HandlerFunc(serveOpenAPIJSON))
	r.Method(http.MethodGet, "/openapi.yaml", http.HandlerFunc(serveOpenAPIYAML))
	r.Method(http.MethodGet, "/docs", http.HandlerFunc(serveDocs))
	return nil
}

// scalarDocsHTML is the single-page reference UI: Scalar loaded from jsdelivr
// pulls /openapi.json at runtime and renders the docs from it. The spec's
// servers[0].url already points at this same mock, so "Try it" works without
// further config.
const scalarDocsHTML = `<!doctype html>
<html>
  <head>
    <title>auth0-mock API reference</title>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
  </head>
  <body>
    <script id="api-reference" data-url="/openapi.json"></script>
    <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
  </body>
</html>
`

func serveDocs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(scalarDocsHTML))
}

func serveOpenAPIJSON(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(api.MockOpenAPIJSON)
}

func serveOpenAPIYAML(w http.ResponseWriter, _ *http.Request) {
	openapiBytes.once.Do(func() {
		doc, err := openapi3.NewLoader().LoadFromData(api.MockOpenAPIJSON)
		if err != nil {
			openapiBytes.err = fmt.Errorf("parse embedded spec: %w", err)
			return
		}
		body, err := yaml.Marshal(doc)
		if err != nil {
			openapiBytes.err = fmt.Errorf("marshal yaml: %w", err)
			return
		}
		openapiBytes.yaml = body
	})
	if openapiBytes.err != nil {
		http.Error(w, openapiBytes.err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(openapiBytes.yaml)
}
