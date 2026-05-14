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
//
// Boot sequence on the client:
//  1. Fetch a fresh access_token via `/oauth/token` (client_credentials).
//  2. Read `prefers-color-scheme` so dark mode follows the OS.
//  3. Mount Scalar with the token preloaded in the `bearerAuth` scheme so the
//     "Try it" panel can call Mgmt API endpoints without any user input.
//
// `agent: { disabled: true }` switches off Scalar's "Ask AI" panel — that
// feature would upload the OpenAPI document to Scalar's Agent backend, which
// has no documented retention policy.
const scalarDocsHTML = `<!doctype html>
<html>
  <head>
    <title>auth0-mock API reference</title>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
  </head>
  <body>
    <div id="app"></div>
    <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
    <script>
      (async () => {
        let token = '';
        try {
          const resp = await fetch('/oauth/token', {
            method: 'POST',
            headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
            body: 'grant_type=client_credentials&client_id=docs&client_secret=docs',
          });
          if (resp.ok) {
            const data = await resp.json();
            token = data.access_token || '';
          }
        } catch (e) {
          console.warn('docs: token preload failed', e);
        }
        const prefersDark = !!(window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches);
        const config = {
          url: '/openapi.json',
          theme: 'moon',
          layout: 'modern',
          darkMode: prefersDark,
          withDefaultFonts: false,
          agent: { disabled: true },
        };
        if (token) {
          config.authentication = {
            preferredSecurityScheme: 'bearerAuth',
            securitySchemes: { bearerAuth: { token: token } },
          };
        }
        Scalar.createApiReference('#app', config);
      })();
    </script>
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
