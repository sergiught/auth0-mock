package authapi

import _ "embed"

// Fragment is the per-package OpenAPI 3.1 partial document describing every
// Auth API endpoint registered in this package's Mount function. The
// genopenapi bundler merges it with the base Mgmt API spec to produce
// api/auth0-mock.openapi.json.
//
//go:embed authapi.openapi.yaml
var Fragment []byte
