package authapi

import (
	"net/http"
	"net/url"

	"github.com/google/uuid"

	"github.com/sergiught/auth0-mock/internal/httperr"
)

// AuthorizeHandler handles OIDC authorization requests.
type AuthorizeHandler struct{}

func (h *AuthorizeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirect := q.Get("redirect_uri")
	if redirect == "" {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "missing redirect_uri")
		return
	}
	state := q.Get("state")
	responseType := q.Get("response_type")
	if responseType == "" {
		responseType = "code"
	}

	u, err := url.Parse(redirect)
	if err != nil {
		httperr.WriteAuth(w, http.StatusBadRequest, "invalid_request", "invalid redirect_uri")
		return
	}
	params := u.Query()
	switch responseType {
	case "token":
		params.Set("access_token", "mock-implicit-token-"+uuid.NewString())
		params.Set("token_type", "Bearer")
	default:
		params.Set("code", uuid.NewString())
	}
	if state != "" {
		params.Set("state", state)
	}
	u.RawQuery = params.Encode()

	w.Header().Set("Location", u.String())
	w.WriteHeader(http.StatusFound)
}
