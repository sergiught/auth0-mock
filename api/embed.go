// Package api embeds the OpenAPI assets the auth0-mock binary needs:
// the upstream Auth0 Management API spec (input to the bundler), the
// mock-control shared schemas for /match and /reset bodies, and the merged
// OpenAPI document served at /openapi.json.
package api

import _ "embed"

// ManagementOpenAPIJSON is the verbatim Auth0 Management API OpenAPI 3.1 spec.
//
//go:embed auth0-management-api.openapi.json
var ManagementOpenAPIJSON []byte

// MockControlOpenAPIYAML is the shared OpenAPI fragment defining schemas for
// the `/match` and `/reset` request and response bodies that the bundler
// stitches into the merged spec.
//
//go:embed mock-control.openapi.yaml
var MockControlOpenAPIYAML []byte

// MockOpenAPIJSON is the merged OpenAPI 3.1 document for this mock — the
// upstream Mgmt API plus auth-api/admin0/service fragments and synthesised
// /match + /reset siblings. Regenerate via `make openapi`.
//
//go:embed auth0-mock.openapi.json
var MockOpenAPIJSON []byte
