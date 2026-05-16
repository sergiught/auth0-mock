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
	// MaxRequestBodyBytes caps every incoming request body. Zero or negative
	// disables the cap.
	MaxRequestBodyBytes int64
	// LogoutAllowedURLs is the allow-list of absolute returnTo URLs that
	// /v2/logout will redirect to. Relative URLs are always allowed.
	LogoutAllowedURLs []string
	// BearerRequireAudience, when non-empty, makes the Mgmt-API bearer
	// middleware reject tokens whose `aud` claim doesn't contain this
	// value. Opt-in to preserve the documented test-friendly default.
	BearerRequireAudience string
}

// New constructs the http.Handler with admin0, JWKS, Auth API, Mgmt API mounts.
func New(d Deps) (http.Handler, error) {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recovery(d.Log))
	r.Use(middleware.MaxBodyBytes(d.MaxRequestBodyBytes))
	r.Use(middleware.Logging(d.Log))

	mountHealthz(r, d.Log)
	mountReadyz(r, d.Keys, d.Log)
	admin0.Mount(r, admin0.Deps{
		Matches:     d.Store,
		Claims:      d.Claims,
		Permissions: d.Permissions,
		MFA:         d.MFA,
		Validator:   d.Validator,
	})
	mountJWKS(r, d.Keys, d.Log)
	if err := MountOpenAPI(r); err != nil {
		return nil, fmt.Errorf("mount openapi: %w", err)
	}

	authapi.Mount(authapi.Deps{
		Router:            r,
		Keys:              d.Keys,
		Issuer:            d.Issuer,
		DefaultAudience:   d.DefaultAudience,
		Log:               d.Log,
		Claims:            d.Claims,
		Permissions:       d.Permissions,
		PKCE:              d.PKCE,
		MFA:               d.MFA,
		LogoutAllowedURLs: d.LogoutAllowedURLs,
	})

	if err := mgmtapi.Mount(mgmtapi.MountOpts{
		Router: r, Spec: d.Spec, Validator: d.Validator,
		Store: d.Store, Keys: d.Keys, Log: d.Log, Strict: d.SpecValidationStrict,
		RequireAudience: d.BearerRequireAudience,
	}); err != nil {
		return nil, fmt.Errorf("mount mgmtapi: %w", err)
	}

	return r, nil
}

func mountJWKS(r chi.Router, keys *jwks.KeySet, log zerolog.Logger) {
	r.Get("/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(keys.JWKSJSON()); err != nil {
			log.Debug().Err(err).Str("path", r.URL.Path).Msg("write failed (client likely gone)")
		}
	})
}

// mountHealthz exposes a Kubernetes-style liveness probe. Cheap, no auth, no
// dependencies — returns 200 if the process is up at all.
func mountHealthz(r chi.Router, log zerolog.Logger) {
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"status":"ok"}`)); err != nil {
			log.Debug().Err(err).Str("path", r.URL.Path).Msg("write failed (client likely gone)")
		}
	})
}

// mountReadyz exposes a Kubernetes-style readiness probe. Separate from
// /healthz so orchestrators can distinguish "process exists" from "process
// can actually serve traffic"; today the only thing readiness gates on is
// that the JWKS signing key materialised, which is what every other code
// path eventually needs. Returns 503 with a one-line reason on failure.
func mountReadyz(r chi.Router, keys *jwks.KeySet, log zerolog.Logger) {
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if keys == nil || keys.KeyID() == "" {
			w.WriteHeader(http.StatusServiceUnavailable)
			if _, err := w.Write([]byte(`{"status":"not_ready","reason":"jwks key not initialised"}`)); err != nil {
				log.Debug().Err(err).Str("path", r.URL.Path).Msg("write failed (client likely gone)")
			}
			return
		}
		if _, err := w.Write([]byte(`{"status":"ready"}`)); err != nil {
			log.Debug().Err(err).Str("path", r.URL.Path).Msg("write failed (client likely gone)")
		}
	})
}
