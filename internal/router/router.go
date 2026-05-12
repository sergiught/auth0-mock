// Package router builds the http.Handler that fronts the mock service.
// Each milestone extends Mount with another subsystem.
package router

import (
	"net/http"

	"github.com/julienschmidt/httprouter"
	"github.com/rs/zerolog"

	"github.com/sergiught/auth0-mock/internal/admin0"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/middleware"
)

// New constructs the http.Handler with admin0 endpoints and the JWKS endpoint
// wired in. Later milestones add Auth API and Mgmt API mounts.
func New(log zerolog.Logger, store *matches.Store, keys *jwks.KeySet) http.Handler {
	r := httprouter.New()
	admin0.Mount(r, store)
	mountJWKS(r, keys)

	return middleware.RequestID(
		middleware.Recovery(log)(
			middleware.Logging(log)(r)))
}

func mountJWKS(r *httprouter.Router, keys *jwks.KeySet) {
	r.HandlerFunc(http.MethodGet, "/.well-known/jwks.json",
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(keys.JWKSJSON())
		})
}
