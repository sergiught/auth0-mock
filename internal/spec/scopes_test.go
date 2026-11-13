package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const scopesSpec = `{
  "openapi": "3.0.0",
  "info":{"title":"t","version":"1"},
  "servers":[{"url":"http://x/api/v2"}],
  "paths":{
    "/a":{"get":{"operationId":"a","security":[{"bearerAuth":[]},{"oAuth2ClientCredentials":["read:b","read:a"]}],"responses":{"200":{"description":"ok"}}}},
    "/b":{"get":{"operationId":"b","security":[{"bearerAuth":[]},{"oAuth2ClientCredentials":["read:a","create:c"]}],"responses":{"200":{"description":"ok"}}}},
    "/c":{"get":{"operationId":"c","responses":{"200":{"description":"ok"}}}}
  }
}`

func TestOperationScopes_KeepsSpecOrder(t *testing.T) {
	t.Parallel()
	s, err := Load([]byte(scopesSpec))
	require.NoError(t, err)
	got := map[string][]string{}
	for op := range s.Operations() {
		got[op.Op.OperationID] = OperationScopes(op.Op)
	}
	assert.Equal(t, []string{"read:b", "read:a"}, got["a"])
	assert.Empty(t, got["c"])
}

func TestManagementScopes_DedupedAndSorted(t *testing.T) {
	t.Parallel()
	s, err := Load([]byte(scopesSpec))
	require.NoError(t, err)
	assert.Equal(t, []string{"create:c", "read:a", "read:b"}, s.ManagementScopes())
}
