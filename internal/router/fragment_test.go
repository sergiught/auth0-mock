package router_test

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/router"
)

func TestServiceFragmentDescribesEveryServiceEndpoint(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromData(router.ServiceFragment)
	require.NoError(t, err)
	require.NotNil(t, doc.Paths)
	want := map[string][]string{
		"/healthz":                       {"GET"},
		"/.well-known/jwks.json":         {"GET"},
		"/openapi.json":                  {"GET"},
		"/openapi.yaml":                  {"GET"},
	}
	for path, methods := range want {
		item := doc.Paths.Value(path)
		require.NotNilf(t, item, "missing path %s", path)
		for _, m := range methods {
			op := item.GetOperation(m)
			assert.NotNilf(t, op, "%s %s missing", m, path)
		}
	}
}
