package mgmtapi

import (
	"fmt"
	"net/http"

	"github.com/julienschmidt/httprouter"
	"github.com/rs/zerolog"

	"github.com/sergiught/auth0-mock/internal/bearer"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/spec"
)

// MountOpts is the parameter object for Mount.
type MountOpts struct {
	Router    *httprouter.Router
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
// Returns an error if route conflicts are detected during mount.
func Mount(opts MountOpts) error {
	bearerMW := bearer.Middleware(opts.Keys)

	for op := range opts.Spec.Operations() {
		op := op
		base := RouterPath(op.Template)
		matchPath := base + "/match"
		resetPath := base + "/reset"

		generic := bearerMW(genericHandler(op, opts))
		matchH := matchHandler(op, opts)
		resetH := resetHandler(op, opts)

		if err := safeHandle(opts.Router, op.Method, base, generic); err != nil {
			return err
		}
		if err := safeHandle(opts.Router, op.Method, matchPath, matchH); err != nil {
			return err
		}
		if err := safeHandle(opts.Router, op.Method, resetPath, resetH); err != nil {
			return err
		}
	}
	return nil
}

// safeHandle wraps router.Handler and converts panics from duplicate route
// registrations into errors.
func safeHandle(r *httprouter.Router, method, path string, h http.Handler) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("route conflict %s %s: %v", method, path, rec)
		}
	}()
	r.Handler(method, path, h)
	return nil
}
