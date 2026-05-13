package authapi_test

import (
	"context"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/authapi"
)

func TestAuthAPIFragmentDescribesEveryMountedRoute(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromData(authapi.Fragment)
	require.NoError(t, err)
	require.NoError(t, doc.Validate(context.Background()))
	want := map[string]string{
		"POST /oauth/token":                     "auth-api",
		"GET /authorize":                        "auth-api",
		"GET /userinfo":                         "auth-api",
		"GET /.well-known/openid-configuration": "auth-api",
		"GET /v2/logout":                        "auth-api",
		"POST /oauth/revoke":                    "auth-api",
		"POST /dbconnections/signup":            "auth-api",
		"POST /dbconnections/change_password":   "auth-api",
		"POST /passwordless/start":              "auth-api",
		"POST /passwordless/verify":             "auth-api",
	}
	for key, tag := range want {
		parts := strings.SplitN(key, " ", 2)
		require.Len(t, parts, 2)
		method, path := parts[0], parts[1]
		item := doc.Paths.Value(path)
		require.NotNilf(t, item, "missing path %s", path)
		op := item.GetOperation(method)
		require.NotNilf(t, op, "%s %s missing", method, path)
		assert.Containsf(t, op.Tags, tag, "%s %s missing tag %s", method, path, tag)
	}
}
