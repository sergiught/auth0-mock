package admin0

import _ "embed"

// Fragment is the per-package OpenAPI 3.1 partial document describing every
// /admin0/* route registered in this package's Mount function. The genopenapi
// bundler merges it with the base Mgmt API spec to produce
// api/auth0-mock.openapi.json.
//
//go:embed admin0.openapi.yaml
var Fragment []byte
