package authapi

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sergiught/auth0-mock/internal/permissions"
)

func TestToken_ClientCredentials_ManagementAudienceIsAnyApiV2URL(t *testing.T) {
	r, ks := newMgmtScopeRouter(t, "http://localhost:8080/", permissions.NewStore())

	cases := map[string]string{
		"https://localhost:8443/api/v2/":     "create:users read:users", // DEFAULT_AUDIENCE with another issuer.
		"http://example/api/v2/":             "create:users read:users",
		"https://tenant.us.auth0.com/api/v2": "create:users read:users",
		"https://api.example.com/":           "",
		"https://x/api/v2/users":             "",
		"/api/v2/":                           "",
	}
	for aud, want := range cases {
		_, c := postClientCredentials(t, r, ks, url.Values{"audience": []string{aud}})
		assert.Equal(t, want, c.Scope, aud)
	}
}
