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
