package admin0_test

import (
	"context"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/admin0"
)

func TestAdmin0FragmentDescribesEveryMountedRoute(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromData(admin0.Fragment)
	require.NoError(t, err)
	require.NoError(t, doc.Validate(context.Background()))
	want := [][2]string{
		{"POST", "/admin0/reset"},
		{"GET", "/admin0/matches"},
		{"GET", "/admin0/claims"},
		{"PUT", "/admin0/claims"},
		{"DELETE", "/admin0/claims"},
		{"GET", "/admin0/permissions"},
		{"DELETE", "/admin0/permissions"},
		{"GET", "/admin0/permissions/{audience}"},
		{"PUT", "/admin0/permissions/{audience}"},
		{"DELETE", "/admin0/permissions/{audience}"},
		{"GET", "/admin0/mfa-required"},
		{"PUT", "/admin0/mfa-required"},
	}
	for _, mp := range want {
		method, path := mp[0], mp[1]
		item := doc.Paths.Value(path)
		require.NotNilf(t, item, "missing path %s", path)
		op := item.GetOperation(method)
		require.NotNilf(t, op, "%s %s missing", method, path)
		assert.Containsf(t, op.Tags, "admin0", "%s %s missing tag admin0", method, path)
	}
}
