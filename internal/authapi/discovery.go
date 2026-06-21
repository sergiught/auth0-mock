package authapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
)

// DiscoveryHandler serves the OIDC discovery document. The document depends
// only on the issuer (fixed at Mount time), so it is marshaled once in
// NewDiscoveryHandler and every request just writes the cached bytes —
// mirroring jwks.KeySet's precomputed JWKSJSON.
type DiscoveryHandler struct {
	doc []byte
}

// NewDiscoveryHandler builds the OIDC discovery document for issuer once.
func NewDiscoveryHandler(issuer string) *DiscoveryHandler {
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
	// Match render.JSON's encoding (HTML-escaped, trailing newline) so the wire
	// output is byte-identical to the previous per-request render. A map of
	// strings and string slices cannot fail to marshal.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(doc)
	return &DiscoveryHandler{doc: buf.Bytes()}
}

func (h *DiscoveryHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(h.doc)
}
