// Package router builds the http.Handler that fronts the mock service.
package router

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/sergiught/auth0-mock/internal/admin0"
	"github.com/sergiught/auth0-mock/internal/authapi"
	"github.com/sergiught/auth0-mock/internal/authorizeerr"
	"github.com/sergiught/auth0-mock/internal/claims"
	"github.com/sergiught/auth0-mock/internal/clock"
	"github.com/sergiught/auth0-mock/internal/events"
	"github.com/sergiught/auth0-mock/internal/journal"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/mfa"
	"github.com/sergiught/auth0-mock/internal/mgmtapi"
	"github.com/sergiught/auth0-mock/internal/middleware"
	"github.com/sergiught/auth0-mock/internal/namespace"
	"github.com/sergiught/auth0-mock/internal/permissions"
	"github.com/sergiught/auth0-mock/internal/pkce"
	"github.com/sergiught/auth0-mock/internal/seed"
	"github.com/sergiught/auth0-mock/internal/session"
	"github.com/sergiught/auth0-mock/internal/spec"
)

// Deps is the parameter object for New.
type Deps struct {
	Log           zerolog.Logger
	Store         *matches.Store
	Claims        *claims.Store
	ClaimMappings *claims.MappingStore
	// Sessions remembers each user's profile for /userinfo and each refresh
	// token's grant for refresh_token requests. Nil is
	// defaulted to a fresh store; /admin0/reset clears it.
	Sessions *session.Store
	// AuthorizeErrors holds the /authorize failure rules. Nil is defaulted
	// to an empty store; /admin0/reset clears it.
	AuthorizeErrors      *authorizeerr.Store
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
	// LogoutAllowedURLs is the allow-list of absolute URLs that /v2/logout
	// (returnTo) and /oidc/logout (post_logout_redirect_uri) will redirect to.
	// Relative URLs are always allowed.
	LogoutAllowedURLs []string
	// AuthorizeAllowedRedirectURIs is the allow-list of absolute
	// redirect_uri values that /authorize will 302 to. Same threat model
	// as LogoutAllowedURLs but on the higher-value endpoint (it carries
	// `code` / `access_token` in the URL). Empty = no enforcement.
	AuthorizeAllowedRedirectURIs []string
	// CORSAllowedOrigins is the allow-list of browser origins that get CORS
	// headers. Empty (or a "*" entry) allows any origin; see
	// middleware.CORS for the entry syntax. An invalid entry makes New fail.
	CORSAllowedOrigins []string
	// BearerRequireAudience, when non-empty, makes the Mgmt-API bearer
	// middleware reject tokens whose `aud` claim doesn't contain this
	// value. Opt-in to preserve the documented test-friendly default.
	BearerRequireAudience string
	// TokenEndpointStrict turns on Auth0-like authorization-code checks at
	// /oauth/token, drops refresh_token without offline_access, rotates
	// refresh tokens and makes /oauth/revoke end the chain. Opt-in; see
	// config.Config.TokenEndpointStrict.
	TokenEndpointStrict bool
	// BearerRequireScopes makes the Mgmt-API enforce each operation's
	// required token scopes (403 insufficient_scope).
	BearerRequireScopes bool
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
	// EventsReplayBuffer is the cap of the SSE replay ring buffer for
	// GET /events. 100 in production via EVENTS_REPLAY_BUFFER; <= 0
	// disables replay (the endpoint still works; resume params become
	// no-ops). Zero value is the test-friendly default.
	EventsReplayBuffer int
	// EventsReconnectHint is the SSE `retry:` reconnect-delay hint sent on
	// connect (EVENTS_RECONNECT_HINT in production); <= 0 omits it. Zero
	// value omits the hint, which is fine for tests.
	EventsReconnectHint time.Duration

	// Journal records /api/v2 calls for GET /admin0/journal. Nil is
	// defaulted to a disabled journal (size 0).
	Journal *journal.Journal

	// Seed is the loaded SEED_FILE; nil means none.
	Seed *seed.Manager
	// SeedTemplate is the shared seed that /admin0/reset restores when Seed
	// is nil. Set for namespaces; the default instance leaves it nil.
	SeedTemplate *seed.Manager
	// NamespaceMax caps live namespaces served behind the
	// X-Auth0-Mock-Namespace header and /ns/<id>/ prefix. Zero turns
	// namespaces off.
	NamespaceMax int
}

// New constructs the http.Handler with admin0, JWKS, Auth API, Mgmt API
// mounts. When d.NamespaceMax > 0 the handler also serves isolated
// namespaces, each a private copy of the state built from d.
func New(d Deps) (http.Handler, error) {
	def, _, err := build(d)
	if err != nil {
		return nil, err
	}
	if d.NamespaceMax <= 0 {
		return def, nil
	}
	factory := func(id string) (*namespace.Instance, error) {
		nd, err := d.forNamespace(id)
		if err != nil {
			return nil, err
		}
		h, hub, err := build(nd)
		if err != nil {
			return nil, err
		}
		if d.Seed != nil {
			d.Seed.ApplyTo(seed.Stores{
				Matches: nd.Store, Claims: nd.Claims, ClaimMappings: nd.ClaimMappings,
				Permissions: nd.Permissions, MFA: nd.MFA,
			})
		}
		return &namespace.Instance{Handler: h, Close: func(ctx context.Context) {
			if err := hub.Shutdown(ctx); err != nil {
				nd.Log.Debug().Err(err).Msg("namespace events shutdown")
			}
		}}, nil
	}
	// Responses the dispatcher writes itself (errors, lifecycle routes) go
	// through the same cross-cutting middleware a namespace router uses.
	cors, err := middleware.CORS(d.CORSAllowedOrigins)
	if err != nil {
		return nil, fmt.Errorf("cors: %w", err)
	}
	logging := middleware.Logging(d.Log)
	if d.Debug {
		logging = middleware.DebugDump(d.Log, os.Stdout)
	}
	return namespace.New(def, factory, d.NamespaceMax,
		namespace.WithLogger(d.Log),
		namespace.WithMiddleware(middleware.RequestID, middleware.MaxBodyBytes(d.MaxRequestBodyBytes), cors, middleware.Recovery(d.Log), logging),
	), nil
}

// forNamespace returns Deps for a private copy of the router state.
func (d Deps) forNamespace(id string) (Deps, error) {
	if d.Keys == nil {
		return Deps{}, errors.New("namespaces require signing keys")
	}
	clk := clock.NewControlled()
	nd := d
	nd.Log = d.Log.With().Str("namespace", id).Logger()
	nd.Store = matches.NewStore()
	nd.Claims = claims.NewStore()
	nd.ClaimMappings = claims.NewMappingStore()
	nd.Permissions = permissions.NewStore()
	nd.PKCE = pkce.NewStore(pkce.WithNow(clk.Now))
	nd.MFA = mfa.NewStore(mfa.WithNow(clk.Now))
	nd.Sessions = nil
	nd.AuthorizeErrors = nil
	nd.Clock = clk
	nd.Keys = d.Keys.WithNow(clk.Now)
	size := 0
	if d.Journal != nil {
		size = d.Journal.Size()
	}
	nd.Journal = journal.New(size)
	nd.Seed = nil
	nd.SeedTemplate = d.Seed
	nd.NamespaceMax = 0
	return nd, nil
}

// build constructs the handler for one set of stores and returns its
// events hub so callers can shut it down.
func build(d Deps) (http.Handler, *events.Hub, error) {
	r := chi.NewRouter()
	// Off-by-default journal for callers (tests) that don't wire one; the
	// middleware must be installed before any route registration.
	if d.Journal == nil {
		d.Journal = journal.New(0)
	}
	r.Use(middleware.RequestID)
	r.Use(middleware.MaxBodyBytes(d.MaxRequestBodyBytes))
	// CORS precedes the journal, logging and spec validation so preflights
	// are answered before any of them runs.
	cors, err := middleware.CORS(d.CORSAllowedOrigins)
	if err != nil {
		return nil, nil, fmt.Errorf("cors: %w", err)
	}
	r.Use(cors)
	// Recovery sits inside the journal so a recovered panic's 500 is
	// recorded too.
	r.Use(d.Journal.Middleware)
	r.Use(middleware.Recovery(d.Log))
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

	var clockNow func() time.Time
	if d.Clock != nil {
		clockNow = d.Clock.Now
	}
	hub, err := events.NewHub(d.EventsReplayBuffer, clockNow, events.WithReconnectHint(d.EventsReconnectHint))
	if err != nil {
		return nil, nil, fmt.Errorf("events hub: %w", err)
	}

	// Claim mappings are off-by-default (empty map = feature off), so a
	// caller that never heard of the store shouldn't have to wire one for
	// the /admin0/claims/mappings routes to be safe to hit. Default it
	// here rather than nil-guarding in the handlers.
	if d.ClaimMappings == nil {
		d.ClaimMappings = claims.NewMappingStore()
	}

	if d.Sessions == nil {
		d.Sessions = session.NewStore()
	}
	if d.AuthorizeErrors == nil {
		d.AuthorizeErrors = authorizeerr.NewStore()
	}
	admin0.Mount(r, admin0.Deps{
		Matches:         d.Store,
		Claims:          d.Claims,
		ClaimMappings:   d.ClaimMappings,
		Sessions:        d.Sessions,
		AuthorizeErrors: d.AuthorizeErrors,
		Permissions:     d.Permissions,
		MFA:             d.MFA,
		Validator:       d.Validator,
		Clock:           d.Clock,
		Events:          hub,
		Journal:         d.Journal,
		Seed:            d.Seed,
		SeedTemplate:    d.SeedTemplate,
	})
	mountJWKS(r, d.Keys, d.Log)
	if err := MountOpenAPI(r); err != nil {
		return nil, nil, fmt.Errorf("mount openapi: %w", err)
	}

	authapi.Mount(authapi.Deps{
		Router:                       r,
		Keys:                         d.Keys,
		Issuer:                       d.Issuer,
		DefaultAudience:              d.DefaultAudience,
		ManagementScopes:             d.Spec.ManagementScopes(),
		TokenEndpointStrict:          d.TokenEndpointStrict,
		Log:                          d.Log,
		Claims:                       d.Claims,
		ClaimMappings:                d.ClaimMappings,
		Sessions:                     d.Sessions,
		Permissions:                  d.Permissions,
		PKCE:                         d.PKCE,
		MFA:                          d.MFA,
		LogoutAllowedURLs:            d.LogoutAllowedURLs,
		AuthorizeAllowedRedirectURIs: d.AuthorizeAllowedRedirectURIs,
		AuthorizeErrors:              d.AuthorizeErrors,
	})

	if err := mgmtapi.Mount(mgmtapi.MountOpts{
		Router: r, Spec: d.Spec, Validator: d.Validator,
		Store: d.Store, Keys: d.Keys, Log: d.Log, Strict: d.SpecValidationStrict,
		RequireAudience: d.BearerRequireAudience,
		RequireScopes:   d.BearerRequireScopes,
		EventsHandler:   hub.Handler(),
		Now:             clockNow,
	}); err != nil {
		return nil, nil, fmt.Errorf("mount mgmtapi: %w", err)
	}

	return r, hub, nil
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
