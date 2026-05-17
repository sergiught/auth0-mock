// Package router builds the http.Handler that fronts the mock service.
package router

import (
	"fmt"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/sergiught/auth0-mock/internal/admin0"
	"github.com/sergiught/auth0-mock/internal/authapi"
	"github.com/sergiught/auth0-mock/internal/claims"
	"github.com/sergiught/auth0-mock/internal/clock"
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
	// AuthorizeAllowedRedirectURIs is the allow-list of absolute
	// redirect_uri values that /authorize will 302 to. Same threat model
	// as LogoutAllowedURLs but on the higher-value endpoint (it carries
	// `code` / `access_token` in the URL). Empty = no enforcement.
	AuthorizeAllowedRedirectURIs []string
	// BearerRequireAudience, when non-empty, makes the Mgmt-API bearer
	// middleware reject tokens whose `aud` claim doesn't contain this
	// value. Opt-in to preserve the documented test-friendly default.
	BearerRequireAudience string
	// Debug enables the request/response dump middleware. Off by default;
	// when on, every request and response gets a full method/path/query/
	// headers/body log line at INFO level. Authorization + Cookie headers
	// are redacted, bodies truncated at 8 KiB.
	Debug bool
	// Clock is the controllable time source mounted at /admin0/clock and
	// surfaced via the SDK's Client.Clock. May be nil in tests that don't
	// exercise the admin surface, in which case /admin0/clock handlers
	// will panic if hit.
	Clock *clock.Controlled
}

// New constructs the http.Handler with admin0, JWKS, Auth API, Mgmt API mounts.
func New(d Deps) (http.Handler, error) {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recovery(d.Log))
	r.Use(middleware.MaxBodyBytes(d.MaxRequestBodyBytes))
	// DebugDump and Logging are mutually exclusive: when DEBUG is on the
	// per-request summary line is redundant noise (DebugDump's `←` line
	// already carries method, path, status, body_bytes, and latency),
	// when DEBUG is off the body block isn't needed and Logging's one
	// liner is exactly the right amount of information.
	if d.Debug {
		r.Use(middleware.DebugDump(d.Log, os.Stdout))
	} else {
		r.Use(middleware.Logging(d.Log))
	}

	mountHealthz(r, d.Log)
	mountReadyz(r, d.Keys, d.Log)
	admin0.Mount(r, admin0.Deps{
		Matches:     d.Store,
		Claims:      d.Claims,
		Permissions: d.Permissions,
		MFA:         d.MFA,
		Validator:   d.Validator,
		Clock:       d.Clock,
	})
	mountJWKS(r, d.Keys, d.Log)
	if err := MountOpenAPI(r); err != nil {
		return nil, fmt.Errorf("mount openapi: %w", err)
	}

	authapi.Mount(authapi.Deps{
		Router:                       r,
		Keys:                         d.Keys,
		Issuer:                       d.Issuer,
		DefaultAudience:              d.DefaultAudience,
		Log:                          d.Log,
		Claims:                       d.Claims,
		Permissions:                  d.Permissions,
		PKCE:                         d.PKCE,
		MFA:                          d.MFA,
		LogoutAllowedURLs:            d.LogoutAllowedURLs,
		AuthorizeAllowedRedirectURIs: d.AuthorizeAllowedRedirectURIs,
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

// mountReadyz exposes a Kubernetes-style readiness probe. The mock's only
// init dependency (RSA keygen in jwks.NewKeySet) is synchronous and runs
// before cmd/api/main.go returns, so today this 503 branch is effectively
// unreachable and /readyz answers identically to /healthz. The endpoint is
// kept for orchestrator-convention parity (liveness vs readiness probe
// separation) and as a future-proof seam if init ever gains async work.
// Returns 503 with a one-line reason on failure.
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
