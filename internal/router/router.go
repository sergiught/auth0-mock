// Package router builds the http.Handler that fronts the mock service.
package router

import (
	"fmt"
	"net/http"

	"github.com/julienschmidt/httprouter"
	"github.com/rs/zerolog"

	"github.com/sergiught/auth0-mock/internal/admin0"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/mgmtapi"
	"github.com/sergiught/auth0-mock/internal/middleware"
	"github.com/sergiught/auth0-mock/internal/spec"
)

// Deps is the parameter object for New.
type Deps struct {
	Log                  zerolog.Logger
	Store                *matches.Store
	Keys                 *jwks.KeySet
	Spec                 *spec.Spec
	Validator            *spec.Validator
	SpecValidationStrict bool
}

// New constructs the http.Handler with admin0, JWKS, and Mgmt API mounts.
// (Auth API is added in M3.)
func New(d Deps) (http.Handler, error) {
	r := httprouter.New()
	admin0.Mount(r, d.Store)
	mountJWKS(r, d.Keys)

	if err := mgmtapi.Mount(mgmtapi.MountOpts{
		Router: r, Spec: d.Spec, Validator: d.Validator,
		Store: d.Store, Keys: d.Keys, Log: d.Log, Strict: d.SpecValidationStrict,
	}); err != nil {
		return nil, fmt.Errorf("mount mgmtapi: %w", err)
	}

	return middleware.RequestID(
		middleware.Recovery(d.Log)(
			middleware.Logging(d.Log)(r))), nil
}

func mountJWKS(r *httprouter.Router, keys *jwks.KeySet) {
	r.HandlerFunc(http.MethodGet, "/.well-known/jwks.json",
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(keys.JWKSJSON())
		})
}
