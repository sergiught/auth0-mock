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
// The Scalar bundle is pinned to an exact version and guarded with a
// Subresource Integrity hash — the page mints a real (if short-lived) bearer
// token, so unpinned third-party JS is not something we want to run. To bump
// Scalar: change the version, then recompute the hash with
//
//	curl -sL <url> | openssl dgst -sha384 -binary | openssl base64 -A
//
// Boot sequence on the client:
//  1. Fetch a fresh access_token via `/oauth/token` (client_credentials).
//  2. Bail out to a plain-text fallback if the Scalar bundle didn't load.
//  3. Read `prefers-color-scheme` so dark mode follows the OS.
//  4. Mount Scalar with the token preloaded in the `bearerAuth` scheme so the
//     "Try it" panel can call Mgmt API endpoints without any user input.
//
// `agent: { disabled: true }` switches off Scalar's "Ask AI" panel — that
// feature would upload the OpenAPI document to Scalar's Agent backend, which
// has no documented retention policy.
const scalarDocsHTML = `<!doctype html>
<html lang="en">
  <head>
    <title>auth0-mock API reference</title>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
  </head>
  <body>
    <div id="app"></div>
    <script
      src="https://cdn.jsdelivr.net/npm/@scalar/api-reference@1.55.3/dist/browser/standalone.min.js"
      integrity="sha384-u/Zg79PtgsQJqvDyaod9gOK+Vd81OvakzjgLu6I6m35qTFuFb+MqepR1ErooTvp1"
      crossorigin="anonymous"></script>
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
        if (typeof Scalar === 'undefined') {
          document.getElementById('app').innerHTML =
            '<p style="font-family: sans-serif; max-width: 40rem; margin: 4rem auto; padding: 0 1rem">' +
            'The API reference UI failed to load (the Scalar bundle could not be fetched). ' +
            'The raw OpenAPI document is still available at ' +
            '<a href="/openapi.json">/openapi.json</a> and ' +
            '<a href="/openapi.yaml">/openapi.yaml</a>.</p>';
          return;
        }
        const prefersDark = !!(window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches);
        const config = {
          url: '/openapi.json',
          theme: 'fastify',
          layout: 'modern',
          darkMode: prefersDark,
          withDefaultFonts: false,
          hideClientButton: true,
          hideModels: true,
          // Scalar's navbar dev-tools default to showing on localhost — which
          // is exactly where this mock runs — so switch them off explicitly.
          showDeveloperTools: 'never',
          defaultHttpClient: { targetKey: 'shell', clientKey: 'curl' },
          // hiddenClients is a denylist, so to show only the curated set
          // (curl, python requests, go, rust, java okhttp, js axios, php
          // guzzle) every other language is hidden outright and the kept
          // languages list the *other* clients to hide.
          hiddenClients: {
            shell: ['httpie', 'wget'],
            python: ['aiohttp', 'httpx_async', 'httpx_sync', 'python3'],
            go: false,
            rust: false,
            java: ['asynchttp', 'nethttp', 'unirest'],
            js: ['fetch', 'jquery', 'ofetch', 'xhr'],
            php: ['curl', 'laravel'],
            c: true,
            clojure: true,
            csharp: true,
            dart: true,
            fsharp: true,
            http: true,
            kotlin: true,
            node: true,
            objc: true,
            ocaml: true,
            powershell: true,
            r: true,
            ruby: true,
            swift: true,
          },
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
