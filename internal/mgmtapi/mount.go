package mgmtapi

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

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
// Operations are sorted so static paths (no "{param}") are registered before
// wildcard paths within the same prefix. This is required by httprouter: mixing
// static literals (e.g. /clients/cimd/register) with wildcards (/clients/:id)
// only works when static paths are registered first.
//
// Sibling (/match, /reset) are skipped for paths whose direct child in the spec
// is a wildcard — httprouter cannot coexist a static literal "/match" with a
// wildcard "/:id" at the same level.
func Mount(opts MountOpts) error {
	bearerMW := bearer.Middleware(opts.Keys)

	// Collect all operations upfront so we can sort them.
	ops := make([]spec.Operation, 0, 512)
	for op := range opts.Spec.Operations() {
		ops = append(ops, op)
	}

	// Sort: static paths (no wildcards) before parameterised paths.
	// Within the same category, sort lexicographically for determinism.
	slices.SortFunc(ops, func(a, b spec.Operation) int {
		aWild := strings.Contains(a.Template, "{")
		bWild := strings.Contains(b.Template, "{")
		if aWild != bWild {
			if !aWild {
				return -1 // static before wildcard
			}
			return 1
		}
		// Same kind: sort by template then method.
		if a.Template != b.Template {
			return strings.Compare(a.Template, b.Template)
		}
		return strings.Compare(a.Method, b.Method)
	})

	// Pre-compute the set of router paths that have a wildcard immediate child
	// in the spec. Registering a static "/match" suffix for these paths would
	// corrupt the httprouter tree and prevent the wildcard child from mounting.
	hasWildcardChild := buildWildcardChildSet(ops)

	// Pre-compute the set of (method, routerPath) pairs that are real spec
	// endpoints. Siblings must not be registered for paths where /match or
	// /reset already exist as genuine operations (e.g. the Auth0 spec has
	// /branding/phone/templates/{id}/reset as a real PATCH endpoint).
	specPaths := buildSpecPathSet(ops)

	for _, op := range ops {
		base := RouterPath(op.Template)
		matchPath := base + "/match"
		resetPath := base + "/reset"

		generic := bearerMW(genericHandler(op, opts))
		matchH := matchHandler(op, opts)
		resetH := resetHandler(op, opts)

		if err := safeHandle(opts.Router, op.Method, base, generic); err != nil {
			// Auth0's OpenAPI spec contains httprouter-incompatible path
			// combinations that are valid in OpenAPI but not in httprouter:
			//
			//  (a) Duplicate wildcard param names at the same position, e.g.
			//      /actions/actions/{actionId}/... and /actions/actions/{id}/...
			//
			//  (b) Mixed static + wildcard children at the same level, e.g.
			//      /branding/themes/default (static) and /branding/themes/{themeId}
			//      (wildcard) — httprouter rejects the wildcard once a static
			//      sibling is already registered.
			//
			// In both cases we log a warning and skip the conflicting operation.
			// The first-registered route wins.
			if !isRouteConflict(err) {
				return err
			}
			opts.Log.Warn().Str("method", op.Method).Str("path", base).
				Err(err).Msg("skipping incompatible route (spec/httprouter conflict)")
			continue
		}
		// Register siblings best-effort:
		//  • Skip if the base path has a wildcard direct child — a static
		//    "/match" literal would corrupt the httprouter tree.
		//  • Skip if the sibling path is itself a real spec operation — the
		//    primary route will be registered separately.
		if !hasWildcardChild[base] {
			if !specPaths[pathKey(op.Method, matchPath)] {
				_ = safeHandle(opts.Router, op.Method, matchPath, matchH)
			}
			if !specPaths[pathKey(op.Method, resetPath)] {
				_ = safeHandle(opts.Router, op.Method, resetPath, resetH)
			}
		}
	}
	return nil
}

// buildSpecPathSet returns the set of "METHOD:routerPath" keys for every
// operation in ops. Used to avoid registering siblings that shadow real routes.
func buildSpecPathSet(ops []spec.Operation) map[string]bool {
	set := make(map[string]bool, len(ops))
	for _, op := range ops {
		set[pathKey(op.Method, RouterPath(op.Template))] = true
	}
	return set
}

// pathKey constructs the lookup key used by buildSpecPathSet.
func pathKey(method, path string) string { return method + ":" + path }

// buildWildcardChildSet returns the set of httprouter-style paths that have at
// least one immediate wildcard child segment (":param") in the provided
// operation list.
func buildWildcardChildSet(ops []spec.Operation) map[string]bool {
	set := make(map[string]bool)
	for _, op := range ops {
		rp := RouterPath(op.Template)
		// Walk each slash-boundary prefix of rp.
		for i := 1; i < len(rp); i++ {
			if rp[i] == '/' {
				prefix := rp[:i]
				rest := rp[i+1:]
				// The immediate next segment is a wildcard when it starts with ':'.
				firstSeg, _, _ := strings.Cut(rest, "/")
				if strings.HasPrefix(firstSeg, ":") {
					set[prefix] = true
				}
			}
		}
	}
	return set
}

// isRouteConflict reports whether the error from safeHandle is an httprouter
// tree conflict that should be treated as a soft failure. Auth0's OpenAPI spec
// contains path combinations that are valid in OpenAPI but incompatible with
// httprouter:
//
//   - Different wildcard param names at the same tree position
//     (e.g. {actionId} vs {id} under /actions/actions/).
//   - Mixed static + wildcard siblings at the same level
//     (e.g. /branding/themes/default and /branding/themes/{themeId}).
//
// These are identified by the presence of "conflict" in the error message.
func isRouteConflict(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "conflict")
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
