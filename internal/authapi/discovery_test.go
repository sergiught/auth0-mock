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
	assert.Equal(t, "https://mock/oidc/logout", doc["end_session_endpoint"])

	algs, _ := doc["id_token_signing_alg_values_supported"].([]any)
	assert.Contains(t, algs, "RS256")
}

func TestDiscovery_AdvertisesPKCEMethodsAndClaims(t *testing.T) {
	r, _ := newAuthRouter(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/.well-known/openid-configuration", nil))
	require.Equal(t, 200, w.Code)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &doc))
	assert.Equal(t, []any{"S256", "plain"}, doc["code_challenge_methods_supported"])
	assert.Equal(t, []any{"query", "fragment", "form_post"}, doc["response_modes_supported"])
	assert.Equal(t, []any{"aud", "auth_time", "azp", "email", "email_verified", "exp", "iat", "iss", "name", "nonce", "sub"}, doc["claims_supported"])
}
