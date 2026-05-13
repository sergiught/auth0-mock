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
	Strict    bool // SPEC_VALIDATION_STRICT
}

// Mount walks the spec and registers three routes per operation: the original
// Auth0 endpoint (bearer + spec-validate + generic handler), the /match sibling
// (no bearer; spec-validates the registration body), and the /reset sibling
// (no bearer; clears scope).
//
// chi resolves static paths before parameterised paths at the same level, so
// no static-before-wildcard sort is needed. Siblings (/match, /reset) are
// skipped only when the path is already a real spec operation (not because
// of router tree constraints).
func Mount(opts MountOpts) error {
	// Pre-compute the set of (method, path) pairs that are real spec endpoints.
	// Siblings must not be registered for paths where /match or /reset already
	// exist as genuine operations (e.g. the Auth0 spec has
	// /branding/phone/templates/{id}/reset as a real PATCH endpoint).
	specPaths := buildSpecPathSet(opts.Spec)
	bearerMW := bearer.Middleware(opts.Keys)

	for op := range opts.Spec.Operations() {
		base := op.Template // chi uses {id} natively, no translation needed
		matchPath := base + "/match"
		resetPath := base + "/reset"

		var generic http.Handler = &GenericHandler{
			Op: op, Validator: opts.Validator, Store: opts.Store,
			Log: opts.Log, Strict: opts.Strict,
		}
		generic = bearerMW(generic)

		if err := safeHandle(opts.Router, op.Method, base, generic); err != nil {
			if !isRouteConflict(err) {
				return err
			}
			opts.Log.Warn().Str("method", op.Method).Str("path", base).
				Err(err).Msg("skipping incompatible route (spec/chi conflict)")
			continue
		}
		if !specPaths[pathKey(op.Method, matchPath)] {
			_ = safeHandle(opts.Router, op.Method, matchPath,
				&MatchHandler{Op: op, Validator: opts.Validator, Store: opts.Store})
		}
		if !specPaths[pathKey(op.Method, resetPath)] {
			_ = safeHandle(opts.Router, op.Method, resetPath,
				&ResetHandler{Op: op, Store: opts.Store})
		}
	}
	return nil
}

// buildSpecPathSet returns the set of "METHOD:path" keys for every operation in
// the spec. Used to avoid registering siblings that shadow real routes.
func buildSpecPathSet(s *spec.Spec) map[string]bool {
	set := make(map[string]bool, 512)
	for op := range s.Operations() {
		set[pathKey(op.Method, op.Template)] = true
	}
	return set
}

// pathKey constructs the lookup key used by buildSpecPathSet.
func pathKey(method, path string) string { return method + ":" + path }

// isRouteConflict reports whether the error from safeHandle is a chi route
// conflict that should be treated as a soft failure. Chi panics with messages
// containing "pattern" or "conflict" for duplicate registrations.
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
