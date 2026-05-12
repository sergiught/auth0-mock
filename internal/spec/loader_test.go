package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/api"
)

func TestLoad_ParsesEmbeddedSpec(t *testing.T) {
	s, err := Load(api.ManagementOpenAPIJSON)
	require.NoError(t, err)
	require.NotNil(t, s)
	assert.NotEmpty(t, s.Doc.Paths.Map())
}

func TestLoad_RejectsGarbage(t *testing.T) {
	_, err := Load([]byte("not json"))
	assert.Error(t, err)
}
