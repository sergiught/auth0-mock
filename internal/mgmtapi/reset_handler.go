package mgmtapi

import (
	"net/http"
	"strings"

	"github.com/sergiught/auth0-mock/internal/spec"
)

func resetHandler(op spec.Operation, opts MountOpts) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		regPath := strings.TrimSuffix(r.URL.Path, "/reset")
		opts.Store.ResetEndpoint(op.Method, regPath, KindOfPath(regPath))
		w.WriteHeader(http.StatusNoContent)
	})
}
