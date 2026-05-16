// Package mgmtapi mounts the spec-driven Management API surface onto chi: one
// generic, bearer-protected handler per Auth0 Management API operation.
package mgmtapi

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/sergiught/auth0-mock/internal/bearer"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/spec"
)

// MountOpts is the parameter object for Mount.
type MountOpts struct {
	Router    chi.Router
	Spec      *spec.Spec
	Validator *spec.Validator
	Store     *matches.Store
	Keys      *jwks.KeySet
	Log       zerolog.Logger
	Strict    bool // SPEC_VALIDATION_STRICT.
	// RequireAudience, when non-empty, makes the bearer middleware reject
	// tokens whose `aud` claim doesn't contain this value. Empty = no
	// audience enforcement (the documented default).
	RequireAudience string
}

// Mount walks the spec and registers one bearer-protected generic handler per
// Management API operation. Canned responses are registered out-of-band via
// the /admin0/expectations control plane, not per-operation siblings.
func Mount(opts MountOpts) error {
	bearerMW := bearer.Middleware(opts.Keys, opts.RequireAudience)

	for op := range opts.Spec.Operations() {
		var generic http.Handler = &GenericHandler{
			Op: op, Validator: opts.Validator, Store: opts.Store,
			Log: opts.Log, Strict: opts.Strict,
		}
		generic = bearerMW(generic)

		if err := safeHandle(opts.Router, op.Method, op.Template, generic); err != nil {
			if !isRouteConflict(err) {
				return err
			}
			opts.Log.Warn().Str("method", op.Method).Str("path", op.Template).
				Err(err).Msg("skipping incompatible route (spec/chi conflict)")
		}
	}
	return nil
}

// isRouteConflict reports whether the error from safeHandle is a chi route
// conflict that should be treated as a soft failure.
func isRouteConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "conflict") ||
		strings.Contains(msg, "existing pattern") ||
		strings.Contains(msg, "duplicate")
}

// safeHandle wraps r.Method and converts panics from duplicate route
// registrations into errors.
func safeHandle(r chi.Router, method, path string, h http.Handler) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("route conflict %s %s: %v", method, path, rec)
		}
	}()
	r.Method(method, path, h)
	return nil
}
