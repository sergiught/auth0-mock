package mgmtapi

import (
	"net/http"

	"github.com/sergiught/auth0-mock/internal/spec"
)

// genericHandler is the spec-driven Mgmt API handler. Implemented in M2.7.
func genericHandler(_ spec.Operation, _ MountOpts) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not implemented", http.StatusNotImplemented)
	})
}
