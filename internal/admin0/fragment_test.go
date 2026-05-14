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
	// {method, path, expected tag} — admin0 is split into Claims / Permissions
	// / MFA / Matches so the docs sidebar group→tag nesting is meaningful.
	want := [][3]string{
		{"POST", "/admin0/reset", "Matches"},
		{"GET", "/admin0/matches", "Matches"},
		{"GET", "/admin0/claims", "Claims"},
		{"PUT", "/admin0/claims", "Claims"},
		{"DELETE", "/admin0/claims", "Claims"},
		{"GET", "/admin0/permissions", "Permissions"},
		{"DELETE", "/admin0/permissions", "Permissions"},
		{"GET", "/admin0/permissions/{audience}", "Permissions"},
		{"PUT", "/admin0/permissions/{audience}", "Permissions"},
		{"DELETE", "/admin0/permissions/{audience}", "Permissions"},
		{"GET", "/admin0/mfa-required", "MFA"},
		{"PUT", "/admin0/mfa-required", "MFA"},
	}
	for _, mp := range want {
		method, path, tag := mp[0], mp[1], mp[2]
		item := doc.Paths.Value(path)
		require.NotNilf(t, item, "missing path %s", path)
		op := item.GetOperation(method)
		require.NotNilf(t, op, "%s %s missing", method, path)
		assert.Containsf(t, op.Tags, tag, "%s %s missing tag %s", method, path, tag)
	}
}
