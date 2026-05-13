package router

import _ "embed"

// ServiceFragment is the per-package OpenAPI 3.1 partial doc describing the
// service-plumbing endpoints mounted directly on the root router
// (`/healthz`, `/.well-known/jwks.json`, `/openapi.json`, `/openapi.yaml`).
// The genopenapi bundler reads it from here.
//
//go:embed service.openapi.yaml
var ServiceFragment []byte
