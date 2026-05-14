// Package api embeds the OpenAPI assets the auth0-mock binary needs:
// the Auth0 Management API skeleton (input to the bundler) and the merged
// OpenAPI document served at /openapi.json.
package api

import _ "embed"

// ManagementOpenAPIJSON is the stripped skeleton of Auth0's Management API
// OpenAPI 3.1 spec — paths, methods, parameters and schema shapes only, with
// Auth0's authored prose (descriptions, externalDocs, x-* extensions) removed.
// It is all the mock needs to route and validate requests. Regenerate via
// `make refresh-spec`; see CONTRIBUTING.md.
//
//go:embed auth0-management-api.openapi.json
var ManagementOpenAPIJSON []byte

// MockOpenAPIJSON is the merged OpenAPI 3.1 document for this mock — the
// upstream Mgmt API plus auth-api/admin0/service fragments. Regenerate via
// `make openapi`.
//
//go:embed auth0-mock.openapi.json
var MockOpenAPIJSON []byte
