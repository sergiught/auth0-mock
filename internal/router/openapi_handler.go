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

// MountOpenAPI registers `GET /openapi.json` and `GET /openapi.yaml` on r,
// serving the embedded merged spec. Both endpoints are unauthenticated.
func MountOpenAPI(r chi.Router) error {
	r.Method(http.MethodGet, "/openapi.json", http.HandlerFunc(serveOpenAPIJSON))
	r.Method(http.MethodGet, "/openapi.yaml", http.HandlerFunc(serveOpenAPIYAML))
	return nil
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
