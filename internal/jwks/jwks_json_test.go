package jwks

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJWKSJSON_Shape(t *testing.T) {
	ks := newTestKeySet(t)
	raw := ks.JWKSJSON()
	require.NotEmpty(t, raw)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))
	keys, _ := doc["keys"].([]any)
	require.Len(t, keys, 1)
	first, _ := keys[0].(map[string]any)
	assert.Equal(t, "RSA", first["kty"])
	assert.Equal(t, "RS256", first["alg"])
	assert.Equal(t, "sig", first["use"])
	assert.Equal(t, ks.KeyID(), first["kid"])
	assert.NotEmpty(t, first["n"])
	assert.NotEmpty(t, first["e"])
}
