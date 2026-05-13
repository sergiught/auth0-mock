// Package router builds the http.Handler that fronts the mock service.
package router

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/sergiught/auth0-mock/internal/admin0"
	"github.com/sergiught/auth0-mock/internal/authapi"
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
	Issuer               string
	DefaultAudience      string
	SpecValidationStrict bool
}

// New constructs the http.Handler with admin0, JWKS, Auth API, Mgmt API mounts.
func New(d Deps) (http.Handler, error) {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recovery(d.Log))
	r.Use(middleware.Logging(d.Log))

	admin0.Mount(r, d.Store)
	mountJWKS(r, d.Keys)

	authapi.Mount(authapi.Deps{
		Router:          r,
		Keys:            d.Keys,
		Issuer:          d.Issuer,
		DefaultAudience: d.DefaultAudience,
		Log:             d.Log,
	})

	if err := mgmtapi.Mount(mgmtapi.MountOpts{
		Router: r, Spec: d.Spec, Validator: d.Validator,
		Store: d.Store, Keys: d.Keys, Log: d.Log, Strict: d.SpecValidationStrict,
	}); err != nil {
		return nil, fmt.Errorf("mount mgmtapi: %w", err)
	}

	return r, nil
}

func mountJWKS(r chi.Router, keys *jwks.KeySet) {
	r.Get("/.well-known/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(keys.JWKSJSON())
	})
}
