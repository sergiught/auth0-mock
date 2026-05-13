package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/api"
)

func TestOperations_YieldsAllMethodPathPairs(t *testing.T) {
	s, err := Load(api.ManagementOpenAPIJSON)
	require.NoError(t, err)

	count := 0
	for op := range s.Operations() {
		assert.NotEmpty(t, op.Method)
		assert.NotEmpty(t, op.Template)
		assert.NotNil(t, op.Op)
		count++
	}
	assert.Greater(t, count, 10, "expected many operations in Auth0 spec")
}
