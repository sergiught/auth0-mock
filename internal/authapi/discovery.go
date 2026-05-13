package authapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/render"
)

// DiscoveryHandler serves the OIDC discovery document.
type DiscoveryHandler struct {
	Issuer string
}

func (h *DiscoveryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	issuer := h.Issuer
	base := strings.TrimSuffix(issuer, "/")
	doc := map[string]any{
		"issuer":                                issuer,
		"authorization_endpoint":                base + "/authorize",
		"token_endpoint":                        base + "/oauth/token",
		"userinfo_endpoint":                     base + "/userinfo",
		"jwks_uri":                              base + "/.well-known/jwks.json",
		"end_session_endpoint":                  base + "/v2/logout",
		"revocation_endpoint":                   base + "/oauth/revoke",
		"response_types_supported":              []string{"code", "token", "id_token", "code token", "code id_token", "token id_token", "code token id_token"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
		"scopes_supported":                      []string{"openid", "profile", "email", "offline_access"},
		"grant_types_supported":                 []string{"client_credentials", "password", "refresh_token", "authorization_code"},
	}
	render.JSON(w, r, doc)
}
