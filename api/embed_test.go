package api

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmbeddedSpecHasContent(t *testing.T) {
	assert.Greater(t, len(ManagementOpenAPIJSON), 100_000)
}

func TestMockOpenAPIJSONParses(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromData(MockOpenAPIJSON)
	require.NoError(t, err)
	require.NotNil(t, doc.Paths)
	// Spot-check a path from each surface.
	require.NotNil(t, doc.Paths.Value("/oauth/token"))
	require.NotNil(t, doc.Paths.Value("/admin0/reset"))
	require.NotNil(t, doc.Paths.Value("/healthz"))
	require.NotNil(t, doc.Paths.Value("/api/v2/users/{id}"))
	require.Len(t, doc.Servers, 1)
	require.Equal(t, "http://localhost:8080", doc.Servers[0].URL)
}
