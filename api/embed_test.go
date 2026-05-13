package api

import (
	"context"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmbeddedSpecHasContent(t *testing.T) {
	assert.Greater(t, len(ManagementOpenAPIJSON), 100_000)
}

func TestMockControlOpenAPIYAMLParses(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(MockControlOpenAPIYAML)
	require.NoError(t, err)
	require.NoError(t, doc.Validate(context.Background()))
	require.NotNil(t, doc.Components)
	require.Contains(t, doc.Components.Schemas, "MatchRegistration")
	require.Contains(t, doc.Components.Schemas, "MatchRegistrationResponse")
	require.Contains(t, doc.Components.Schemas, "ResetResponse")
}

func TestMockOpenAPIJSONParses(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromData(MockOpenAPIJSON)
	require.NoError(t, err)
	require.NotNil(t, doc.Paths)
	// Spot-check a path from each surface.
	require.NotNil(t, doc.Paths.Value("/oauth/token"))
	require.NotNil(t, doc.Paths.Value("/admin0/reset"))
	require.NotNil(t, doc.Paths.Value("/healthz"))
	require.NotNil(t, doc.Paths.Value("/api/v2/users/{id}/match"))
	require.Len(t, doc.Servers, 1)
	require.Equal(t, "http://localhost:8080", doc.Servers[0].URL)
}
