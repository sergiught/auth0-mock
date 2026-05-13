// Package router builds the http.Handler that fronts the mock service.
package router

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/sergiught/auth0-mock/internal/admin0"
	"github.com/sergiught/auth0-mock/internal/authapi"
	"github.com/sergiught/auth0-mock/internal/claims"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/mfa"
	"github.com/sergiught/auth0-mock/internal/mgmtapi"
	"github.com/sergiught/auth0-mock/internal/middleware"
	"github.com/sergiught/auth0-mock/internal/permissions"
	"github.com/sergiught/auth0-mock/internal/pkce"
	"github.com/sergiught/auth0-mock/internal/spec"
)

// Deps is the parameter object for New.
type Deps struct {
	Log                  zerolog.Logger
	Store                *matches.Store
	Claims               *claims.Store
	Permissions          *permissions.Store
	PKCE                 *pkce.Store
	MFA                  *mfa.Store
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

	mountHealthz(r)
	admin0.Mount(r, admin0.Deps{
		Matches:     d.Store,
		Claims:      d.Claims,
		Permissions: d.Permissions,
		MFA:         d.MFA,
	})
	mountJWKS(r, d.Keys)
	if err := MountOpenAPI(r); err != nil {
		return nil, fmt.Errorf("mount openapi: %w", err)
	}

	authapi.Mount(authapi.Deps{
		Router:          r,
		Keys:            d.Keys,
		Issuer:          d.Issuer,
		DefaultAudience: d.DefaultAudience,
		Log:             d.Log,
		Claims:          d.Claims,
		Permissions:     d.Permissions,
		PKCE:            d.PKCE,
		MFA:             d.MFA,
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

// mountHealthz exposes a Kubernetes-style liveness probe. Cheap, no auth, no
// dependencies — returns 200 if the process is up at all.
func mountHealthz(r chi.Router) {
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
}
