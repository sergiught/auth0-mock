package mgmtapi

import (
	"net/http"
	"strings"

	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/spec"
)

// ResetHandler clears the registered match for a Management API operation scope.
type ResetHandler struct {
	Op    spec.Operation
	Store *matches.Store
}

func (h *ResetHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	regPath := strings.TrimSuffix(r.URL.Path, "/reset")
	h.Store.ResetEndpoint(h.Op.Method, regPath, KindOfPath(regPath))
	w.WriteHeader(http.StatusNoContent)
}
