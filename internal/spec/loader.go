// Package spec wraps Auth0's embedded OpenAPI document. It exposes operation
// iteration, request validation, response validation, and registration-payload
// validation — everything the mgmtapi package needs to drive the Mgmt API.
package spec

import (
	"fmt"

	"github.com/getkin/kin-openapi/openapi3"
)

// Spec wraps an openapi3.T plus a router for request matching.
type Spec struct {
	Doc      *openapi3.T
	BasePath string // e.g. "/api/v2" (derived from servers[0].url path component)
}

// Load parses the given OpenAPI 3.1 JSON document.
func Load(jsonDoc []byte) (*Spec, error) {
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = false
	doc, err := loader.LoadFromData(jsonDoc)
	if err != nil {
		return nil, fmt.Errorf("parse openapi: %w", err)
	}
	// TODO: Re-enable full validation once kin-openapi supports the lookahead
	// patterns used in Auth0's spec (e.g. BrandingThemeFonts uses `(?=` which
	// Go's regexp/syntax package rejects as unsupported Perl syntax).
	basePath := deriveBasePath(doc)
	// Normalize servers to path-only so the openapi3filter router matches by
	// path, not host. Auth0's spec has servers like
	// "https://{tenantDomain}/api/v2"; the router would otherwise refuse to
	// match requests whose Host header isn't the templated value.
	doc.Servers = openapi3.Servers{&openapi3.Server{URL: basePath}}
	return &Spec{Doc: doc, BasePath: basePath}, nil
}

// deriveBasePath extracts the path component of the first server URL.
// Auth0's spec sets servers[0].url like "https://{tenantDomain}/api/v2".
// We only need the suffix after the host: "/api/v2".
func deriveBasePath(doc *openapi3.T) string {
	if doc.Servers == nil || len(doc.Servers) == 0 {
		return ""
	}
	url := doc.Servers[0].URL
	// Strip scheme://host
	for i := 0; i+2 < len(url); i++ {
		if url[i] == ':' && url[i+1] == '/' && url[i+2] == '/' {
			j := i + 3
			for j < len(url) && url[j] != '/' {
				j++
			}
			return url[j:]
		}
	}
	return url
}
