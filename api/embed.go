// Package api embeds the OpenAPI assets the auth0-mock binary needs:
// the upstream Auth0 Management API spec (input to the bundler), the
// mock-control shared schemas, and the generated merged spec.
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
