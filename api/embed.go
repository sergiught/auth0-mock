// Package api embeds Auth0's Management API OpenAPI spec into the binary.
package api

import _ "embed"

// ManagementOpenAPIJSON is the verbatim Auth0 Management API OpenAPI 3.1 spec.
//
//go:embed auth0-management-api.openapi.json
var ManagementOpenAPIJSON []byte
