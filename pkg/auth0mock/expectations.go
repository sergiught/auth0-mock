package auth0mock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync/atomic"
)

// Expectation is a stored mock response paired with an optional request
// matcher. Concrete paths ("/api/v2/users/auth0|123") and OpenAPI
// templates ("/api/v2/users/{id}") both work; the server infers which
// is which from the path syntax. A nil Request is a catch-all that
// matches every call to the operation.
//
// ID is assigned by the server on Add() and surfaced in List() responses.
// Callers leave it empty when constructing an Expectation for Add;
// the SDK returns the server-assigned ID via *RegisteredExpectation.
//
// Hits is populated by List() — the count of incoming requests this
// expectation has matched and served. Use [Client.Expectations.List] for
// snapshot assertions or [RegisteredExpectation.Hits] for fresh
// per-stub reads.
//
// Resolution rules (server-side, locked by integration tests in
// internal/mgmtapi):
//
//   - Exact-path stubs beat template stubs.
//   - Within a path, a request-matched expectation beats a catch-all.
//   - Within a tier, newest wins (registration order, not body content).
type Expectation struct {
	ID       string          `json:"id,omitempty"`
	Method   string          `json:"method"`
	Path     string          `json:"path"`
	Request  *RequestMatcher `json:"request,omitempty"`
	Response ResponseDef     `json:"response"`
	Hits     int64           `json:"hits,omitempty"`
}

// RequestMatcher narrows an Expectation to requests that satisfy a
// subset of query parameters, headers, and/or a JSON body. Subset
// semantics — extra incoming fields don't disqualify a match. A nil
// RequestMatcher is a catch-all; a non-nil one with every field empty
// collapses to the same catch-all server-side.
//
// Headers are compared case-insensitively (HTTP canonical form). Use
// header matchers to stub different responses based on Authorization
// (Bearer vs DPoP), tenant headers, Accept-Language, etc.
type RequestMatcher struct {
	Query   map[string]string `json:"query,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// ResponseDef is the canned response an Expectation returns. Status is
// required — POSTing with Status: 0 returns 400 invalid_body.
type ResponseDef struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// ExpectationsClient owns the /admin0/expectations CRUD surface. Reach
// it via Client.Expectations; construction is internal.
type ExpectationsClient struct {
	c *Client
}

// Add registers an Expectation against the mock and returns a handle
// to the stored entry. The handle exposes per-stub operations (Clear,
// Hits, Times/AtLeast/AtMost/AnyTimes constraints checked by Verify).
// Discard the handle (assign to `_`) if you don't need per-stub
// introspection.
//
// Returns *APIError for validation failures — known error codes are
// invalid_body, unknown_operation, invalid_match, invalid_request_match.
// The server validates response.body against the operation's response
// schema at registration time so test code fails on bad fixtures
// instead of at SDK-call time.
//
// The Client tracks every returned handle so Verify can iterate them;
// Reset() clears the tracking list alongside the server-side state.
func (e *ExpectationsClient) Add(ctx context.Context, exp Expectation) (*RegisteredExpectation, error) {
	var resp struct {
		ID string `json:"id"`
	}
	if err := e.c.do(ctx, http.MethodPost, "/admin0/expectations", exp, &resp); err != nil {
		return nil, err
	}
	re := &RegisteredExpectation{
		ID:           resp.ID,
		method:       exp.Method,
		path:         exp.Path,
		registeredAt: callerFrame(),
		client:       e.c,
	}
	e.c.trackRegistered(re)
	return re, nil
}

// sdkPkgPrefix is the canonical import path for the SDK's own
// packages — runtime.Frame.Function values starting with this prefix
// (followed by a "." for methods or "/" for subpackages like
// auth0mocktest) are SDK internals and skipped by callerFrame.
//
// Anchored on the full import path with an explicit boundary so a
// downstream fork that happens to vendor a path containing the same
// substring (e.g. /vendor/example.com/auth0-mock-fork/pkg/auth0mock)
// doesn't accidentally match.
const sdkPkgPrefix = "github.com/sergiught/auth0-mock/pkg/auth0mock"

// callerFrame walks the runtime stack looking for the first frame
// outside the auth0mock / auth0mocktest packages — i.e. where the
// user actually called into the SDK. Returns a "file:line" string
// suitable for inclusion in Verify violation messages, or "" if no
// outside-package frame is found (shouldn't happen in practice).
//
// Used at Add-time so Verify failures can name the test that set the
// expectation instead of just dumping the server-assigned UUID.
// Wrappers around Add (e.g. a test helper) will be the first frame
// callerFrame reports — inline the Add call to point at the test
// body instead.
func callerFrame() string {
	const maxFrames = 16
	pcs := make([]uintptr, maxFrames)
	n := runtime.Callers(2, pcs) // Skip runtime.Callers + callerFrame.
	if n == 0 {
		return ""
	}
	frames := runtime.CallersFrames(pcs[:n])
	for {
		f, more := frames.Next()
		// Skip frames inside the SDK itself or its auth0mocktest
		// sibling so the user sees their own call site.
		if !isSDKFrame(f.Function) {
			return fmt.Sprintf("%s:%d", trimRepoPath(f.File), f.Line)
		}
		if !more {
			break
		}
	}
	return ""
}

// isSDKFrame reports whether fn (a fully-qualified function name
// from runtime.Frame.Function) is inside the auth0mock SDK packages.
// Uses a prefix + boundary check rather than substring contains to
// avoid false-positives on forked vendor paths.
func isSDKFrame(fn string) bool {
	if !strings.HasPrefix(fn, sdkPkgPrefix) {
		return false
	}
	rest := fn[len(sdkPkgPrefix):]
	// "github.com/.../auth0mock.(*Client).Foo" → rest starts with ".".
	// "github.com/.../auth0mock/auth0mocktest.Foo" → rest starts with "/".
	return strings.HasPrefix(rest, ".") || strings.HasPrefix(rest, "/")
}

// trimRepoPath shortens an absolute path to the segment starting at
// the user's repo root (heuristic: last two components — dir/file).
// Keeps Verify messages readable when paths are 100+ chars deep.
func trimRepoPath(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) <= 2 {
		return p
	}
	return strings.Join(parts[len(parts)-2:], "/")
}

// Verify checks every registered expectation that has a hit-count
// constraint (Times / AtLeast / AtMost) against the server's actual
// counter. Returns a single error joining every violation; returns nil
// when all constraints are satisfied or no constraints were set.
//
// Verify queries List once and matches by ID, so it's O(N) in the
// number of registered expectations regardless of how many have
// constraints. Use it as the test-end assertion:
//
//	if err := c.Expectations.Verify(ctx); err != nil {
//	    t.Fatal(err)
//	}
//
// Or use auth0mocktest.MustVerify(t, c) for the t.Fatal wrapper.
func (e *ExpectationsClient) Verify(ctx context.Context) error {
	// Consume the safety-net flag UNCONDITIONALLY so it can't carry
	// past this Verify into a future cycle. The flag is only
	// surfaced as an error when the ledger is empty — the actual
	// "constraints silently dropped" signature. If the user
	// re-registered fresh stubs after Reset (legitimate pattern),
	// tracked is non-empty and we proceed to check those.
	resetFlag := e.c.resetAfterUnverified.Swap(false)

	tracked := e.c.registeredSnapshot()
	if len(tracked) == 0 {
		if resetFlag {
			return errors.New("auth0mock: Verify: Reset() ran before Verify() on this Client, so any " +
				"Times/AtLeast/AtMost constraints set since the prior Reset were silently dropped — " +
				"order cleanups so Verify runs before Reset (Go's t.Cleanup is LIFO, register Reset first) " +
				"or use auth0mocktest.Bracket(t, c) which gets the order right")
		}
		return nil
	}
	exps, err := e.List(ctx)
	if err != nil {
		return fmt.Errorf("auth0mock: Verify: list expectations: %w", err)
	}
	byID := make(map[string]int64, len(exps))
	for _, exp := range exps {
		byID[exp.ID] = exp.Hits
	}
	var violations []string
	for _, re := range tracked {
		want := re.expected.Load()
		if want == nil || want.mode == hitsModeUnset {
			continue
		}
		label := re.describeLabel()
		actual, present := byID[re.ID]
		if !present {
			violations = append(violations,
				fmt.Sprintf("%s — expected %s, but the expectation was cleared before Verify ran (Reset/Clear fired between Add and Verify)",
					label, want.describe()))
			continue
		}
		if !want.satisfied(actual) {
			violations = append(violations,
				fmt.Sprintf("%s — expected %s, got %d", label, want.describe(), actual))
		}
	}
	// Successful Verify "consumes" the pending-adds counter so a
	// subsequent Reset on the same Client doesn't flag the bug.
	e.c.pendingAdds.Store(0)
	if len(violations) == 0 {
		return nil
	}
	return errors.New("auth0mock: Verify:\n  " + strings.Join(violations, "\n  "))
}

// describeLabel returns a human-friendly identifier for use in Verify
// violation messages: "METHOD path (file:line)" when both registration
// metadata + caller frame are available, falling back to the bare ID
// when they aren't. Lets test output point at the actual stub instead
// of a UUID.
func (re *RegisteredExpectation) describeLabel() string {
	switch {
	case re.method != "" && re.path != "" && re.registeredAt != "":
		return fmt.Sprintf("%s %s (%s)", re.method, re.path, re.registeredAt)
	case re.method != "" && re.path != "":
		return fmt.Sprintf("%s %s", re.method, re.path)
	default:
		return re.ID
	}
}

// RegisteredExpectation is the handle returned by Expectations.Add and
// the fluent ResponseBuilder.Apply. It identifies a single stored stub
// and exposes per-stub operations (Clear, Hits, Times / AtLeast /
// AtMost / AnyTimes).
//
// # Lifetime
//
// The handle remains usable for as long as the underlying expectation
// is registered server-side. After [RegisteredExpectation.Clear],
// [Client.Reset], or any per-operation clear that drops this stub:
//
//   - [RegisteredExpectation.Hits] returns *[APIError] with statusCode
//     404 and errorCode "unknown_id".
//   - [ExpectationsClient.Verify] reports the handle as "cleared" and
//     fails any non-zero Times/AtLeast constraint it carried, since
//     "cleared before Verify ran" means the SUT never had a chance
//     to hit the stub.
//
// # Concurrency
//
// Wire-call methods (Clear, Hits) are safe for concurrent use across
// goroutines. Constraint setters (Times / AtLeast / AtMost / AnyTimes)
// mutate the handle in place and are NOT synchronised — call them
// from one goroutine at registration time, not from the SUT under
// test.
type RegisteredExpectation struct {
	// ID is the server-assigned identifier for this expectation.
	ID string

	client *Client

	// Method, path snapshot what was registered so Verify violation
	// messages can name "GET /api/v2/users/{id}" instead of just the
	// opaque UUID. Frozen at Add() time; later server-side mutation
	// won't change what describeLabel reports.
	method, path string

	// RegisteredAt is the file:line of the caller that invoked Add,
	// captured via runtime.Callers and trimmed to a short form. Lets
	// Verify failures point at the test that set the expectation
	// instead of forcing the user to grep the UUID. Also frozen at
	// Add() time.
	registeredAt string

	// Expected records the hit-count constraint set via
	// Times / AtLeast / AtMost / AnyTimes for verification. Nil means
	// no expectation set — Verify treats this handle as a no-op.
	//
	// Atomic.Pointer so Times/AtLeast/AtMost/AnyTimes (writers) and
	// Verify's read can race cleanly under -race. Each setter swaps
	// in a fresh *hitsConstraint; readers Load atomically.
	expected atomic.Pointer[hitsConstraint]
}

// Method returns the HTTP method this expectation was registered
// for, snapshotted at Add() time.
func (re *RegisteredExpectation) Method() string { return re.method }

// Path returns the path (concrete or template) this expectation was
// registered for, snapshotted at Add() time.
func (re *RegisteredExpectation) Path() string { return re.path }

// RegisteredAt returns the "file:line" location of the caller that
// invoked Add() to register this expectation, as captured by the
// runtime stack walk. Returns "" if the caller couldn't be resolved
// (shouldn't happen in practice). Useful for logging "what was this
// handle for?" without re-listing every server-side expectation.
func (re *RegisteredExpectation) RegisteredAt() string { return re.registeredAt }

// Hits returns the current number of times this expectation has been
// matched and served by the mock. Fresh read on every call — the SDK
// doesn't cache. Returns *APIError if the expectation has been cleared
// (404).
func (re *RegisteredExpectation) Hits(ctx context.Context) (int64, error) {
	var exp Expectation
	if err := re.client.do(ctx, http.MethodGet, "/admin0/expectations/"+url.PathEscape(re.ID), nil, &exp); err != nil {
		return 0, err
	}
	return exp.Hits, nil
}

// Clear removes this specific expectation. Idempotent — clearing an
// already-cleared expectation is a no-op (no error). Also removes the
// handle from the Client's local verification ledger so Verify after
// Clear doesn't report it as "cleared before Verify ran".
func (re *RegisteredExpectation) Clear(ctx context.Context) error {
	if err := re.client.do(ctx, http.MethodDelete, "/admin0/expectations/"+url.PathEscape(re.ID), nil, nil); err != nil {
		return err
	}
	re.client.untrackByID(re.ID)
	return nil
}

// Times sets an exact-count expectation: Verify(ctx) will fail unless
// this stub was matched exactly n times. Equivalent to gomock's
// .Times(n). Setting n = 0 asserts the stub is never hit (equivalent
// to AtMost(0)); use AnyTimes() to opt out of count checks instead.
//
// Mutates the handle in place and returns it for chaining. The
// constraint slot holds exactly one entry — calling Times after
// AtLeast / AtMost / Times overwrites the previous constraint.
// "Last-write-wins" so the most recent intention is the one Verify
// asserts.
//
// Times is silent feedback: the constraint is only checked when
// [Client.Expectations.Verify] or [auth0mocktest.MustVerify] runs.
// A test that sets a constraint but never calls Verify will pass
// regardless of whether the stub was hit. Use auth0mocktest.Bracket
// to wire Verify into t.Cleanup automatically.
func (re *RegisteredExpectation) Times(n int64) *RegisteredExpectation {
	re.expected.Store(&hitsConstraint{mode: hitsModeExact, exact: n})
	return re
}

// AtLeast sets a lower-bound expectation: Verify(ctx) will fail unless
// this stub was matched at least n times. Useful for retry-tolerant
// flows where the SUT may make extra calls.
//
// Mutates the handle in place and returns it for chaining; overwrites
// any prior Times / AtMost / AtLeast — see [RegisteredExpectation.Times]
// for the last-write-wins contract.
func (re *RegisteredExpectation) AtLeast(n int64) *RegisteredExpectation {
	re.expected.Store(&hitsConstraint{mode: hitsModeAtLeast, atLeast: n})
	return re
}

// AtMost sets an upper-bound expectation: Verify(ctx) will fail unless
// this stub was matched at most n times. Useful for catching SUT bugs
// that retry too aggressively.
//
// Mutates the handle in place and returns it for chaining; overwrites
// any prior Times / AtLeast / AtMost — see [RegisteredExpectation.Times]
// for the last-write-wins contract.
func (re *RegisteredExpectation) AtMost(n int64) *RegisteredExpectation {
	re.expected.Store(&hitsConstraint{mode: hitsModeAtMost, atMost: n})
	return re
}

// AnyTimes opts this stub out of Verify checks entirely — equivalent
// to leaving Times/AtLeast/AtMost unset. Useful when you've previously
// set a constraint and want to drop it without losing the handle.
func (re *RegisteredExpectation) AnyTimes() *RegisteredExpectation {
	re.expected.Store(&hitsConstraint{})
	return re
}

// hitsConstraint describes the constraint Verify enforces for one
// expectation. Mode picks which fields are meaningful.
type hitsConstraint struct {
	mode    hitsMode
	exact   int64
	atLeast int64
	atMost  int64
}

type hitsMode uint8

const (
	hitsModeUnset hitsMode = iota
	hitsModeExact
	hitsModeAtLeast
	hitsModeAtMost
)

// describe returns a human-readable form of the constraint for error
// messages. Returns "" when no constraint is set.
func (h hitsConstraint) describe() string {
	switch h.mode {
	case hitsModeExact:
		return fmt.Sprintf("exactly %d", h.exact)
	case hitsModeAtLeast:
		return fmt.Sprintf("at least %d", h.atLeast)
	case hitsModeAtMost:
		return fmt.Sprintf("at most %d", h.atMost)
	default:
		return ""
	}
}

// satisfied reports whether actual hits meet the constraint.
func (h hitsConstraint) satisfied(actual int64) bool {
	switch h.mode {
	case hitsModeExact:
		return actual == h.exact
	case hitsModeAtLeast:
		return actual >= h.atLeast
	case hitsModeAtMost:
		return actual <= h.atMost
	default:
		return true
	}
}

// List returns every registered Expectation in registration order
// (newest last). Useful for asserting that test setup actually
// registered what you expected.
func (e *ExpectationsClient) List(ctx context.Context) ([]Expectation, error) {
	var resp struct {
		Expectations []Expectation `json:"expectations"`
	}
	if err := e.c.do(ctx, http.MethodGet, "/admin0/expectations", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Expectations, nil
}

// Clear removes every registered Expectation. Idempotent — safe to
// call from t.Cleanup, and safe to call before any expectations have
// been registered. Also empties the Client's local verification
// ledger so Verify after Clear doesn't carry stale handles.
func (e *ExpectationsClient) Clear(ctx context.Context) error {
	if err := e.c.do(ctx, http.MethodDelete, "/admin0/expectations", nil, nil); err != nil {
		return err
	}
	e.c.untrackAll()
	return nil
}

// ClearOp removes every Expectation registered for a specific
// {method, path} pair — the catch-all and every request-matched
// variant. Forgiving: clearing an operation that was never registered
// is a no-op (returns nil, not an *APIError). Also prunes the Client's
// local verification ledger of any handles for that operation.
func (e *ExpectationsClient) ClearOp(ctx context.Context, method, path string) error {
	body := struct {
		Method string `json:"method"`
		Path   string `json:"path"`
	}{Method: method, Path: path}
	if err := e.c.do(ctx, http.MethodDelete, "/admin0/expectations", body, nil); err != nil {
		return err
	}
	e.c.untrackByMethodPath(method, path)
	return nil
}

// MustJSON marshals v to a json.RawMessage and panics on error.
//
// Use this only with values you know marshal cleanly — i.e. constants
// and literals in test code. For values that come from somewhere else
// at runtime, call json.Marshal yourself and handle the error. The
// point of MustJSON is to keep test setup one-liners:
//
//	c.Expectations.Add(ctx, auth0mock.Expectation{
//	    Method: "GET", Path: "/api/v2/users/auth0|123",
//	    Response: auth0mock.ResponseDef{
//	        Status: 200,
//	        Body:   auth0mock.MustJSON(map[string]any{"user_id": "auth0|123"}),
//	    },
//	})
func MustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("auth0mock.MustJSON: %v", err))
	}
	return b
}
