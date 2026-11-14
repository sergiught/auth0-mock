package authapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/sergiught/auth0-mock/internal/session"
)

// RevokeHandler serves /oauth/revoke. With Strict it deletes the refresh-token
// chain of the posted token; otherwise it is a no-op. It always answers 200
// with an empty body, even for unknown tokens.
type RevokeHandler struct {
	Strict   bool
	Sessions *session.Store
}

func (h *RevokeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.Strict && h.Sessions != nil {
		if token := revokeToken(r); token != "" {
			h.Sessions.RevokeRefreshChain(token)
		}
	}
	w.WriteHeader(http.StatusOK)
}

// revokeToken reads `token` from a JSON or form body. It looks at nothing
// else: unrelated fields of any type, malformed pairs and the URL query are
// ignored. It returns "" when there is no token to revoke.
func revokeToken(r *http.Request) string {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return ""
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		// Keep unrelated values raw so none of them (an overflowing number,
		// say) can fail the decode.
		var fields map[string]json.RawMessage
		if json.Unmarshal(body, &fields) != nil {
			return ""
		}
		var token string
		_ = json.Unmarshal(fields["token"], &token)
		return token
	}
	// ParseQuery keeps every well-formed pair even when another one fails.
	values, _ := url.ParseQuery(string(body))
	return values.Get("token")
}
