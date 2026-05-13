package authapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscovery_PublishesIssuerAndEndpoints(t *testing.T) {
	r, _ := newAuthRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/.well-known/openid-configuration", nil))

	require.Equal(t, 200, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var doc map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &doc))
	assert.Equal(t, "https://mock/", doc["issuer"])
	assert.True(t, strings.HasSuffix(doc["jwks_uri"].(string), "/.well-known/jwks.json"))
	assert.Contains(t, doc["token_endpoint"].(string), "/oauth/token")
	assert.Contains(t, doc["authorization_endpoint"].(string), "/authorize")
	assert.Contains(t, doc["userinfo_endpoint"].(string), "/userinfo")

	algs, _ := doc["id_token_signing_alg_values_supported"].([]any)
	assert.Contains(t, algs, "RS256")
}
