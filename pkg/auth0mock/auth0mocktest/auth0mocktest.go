// Package auth0mocktest provides testing.TB-aware helpers for the
// pkg/auth0mock SDK. It lives in a subpackage (matching the standard
// library's net/http + net/http/httptest split) so production
// binaries that import auth0mock don't drag in the testing package
// just to call SDK methods.
//
// Use this package from _test.go files only — every helper takes a
// testing.TB and calls Fatalf on failure, which is meaningless
// outside a test runner.
//
//	import (
//	    "github.com/sergiught/auth0-mock/pkg/auth0mock"
//	    "github.com/sergiught/auth0-mock/pkg/auth0mock/auth0mocktest"
//	)
//
//	func TestFoo(t *testing.T) {
//	    c, err := auth0mock.NewClient("http://localhost:8080")
//	    if err != nil {
//	        t.Fatal(err)
//	    }
//	    auth0mocktest.Bracket(t, c) // Reset on entry + exit, Verify at end
//
//	    reg := auth0mocktest.MustApply(t, c.ExpectGet("/api/v2/users/123").
//	        Respond(200).
//	        JSON(map[string]any{"user_id": "123"}))
//	    reg.Times(1)
//
//	    // ... exercise the system-under-test ...
//	}
package auth0mocktest

import (
	"context"
	"testing"

	"github.com/sergiught/auth0-mock/pkg/auth0mock"
)

// ResetOnCleanup resets the mock immediately and registers a t.Cleanup
// callback that resets it again on test end (including on failure).
// The recommended one-liner to bracket a test in known-empty state:
//
//	func TestMyFlow(t *testing.T) {
//	    c := auth0mock.NewClient(mockURL)
//	    auth0mocktest.ResetOnCleanup(t, c)
//
//	    // ... test setup + execution ...
//	}
//
// The cleanup callback ignores reset errors — by the time t.Cleanup
// fires the test verdict is already decided, and a failing cleanup
// would only obscure the original failure. Use Client.Reset directly
// if you need to assert that teardown succeeded.
func ResetOnCleanup(t testing.TB, c *auth0mock.Client) {
	t.Helper()
	if err := c.Reset(context.Background()); err != nil {
		t.Fatalf("auth0mocktest: pre-test reset failed: %v", err)
	}
	t.Cleanup(func() {
		_ = c.Reset(context.Background())
	})
}

// MustReset wraps Client.Reset with t.Fatalf on error. Use it when you
// want a one-line assertion at a specific point in the test — for the
// common "reset at start, reset at end" pattern, prefer ResetOnCleanup.
func MustReset(t testing.TB, c *auth0mock.Client) {
	t.Helper()
	if err := c.Reset(context.Background()); err != nil {
		t.Fatalf("auth0mocktest: reset failed: %v", err)
	}
}

// MustApply wraps a ResponseBuilder.Apply call with t.Fatalf on error.
// Designed for the fluent chain:
//
//	reg := auth0mocktest.MustApply(t, c.ExpectGet("/api/v2/users/123").
//	    Respond(200).
//	    JSON(map[string]any{"id": "123"}))
//	reg.Times(1)
//	// Discard the handle with `_ = auth0mocktest.MustApply(...)`
//	// if you don't need to set hit-count constraints on it.
//
// Returns the *auth0mock.RegisteredExpectation handle from Apply so
// per-stub operations (Clear, Hits, Times / AtLeast / AtMost /
// AnyTimes) chain naturally. The unwrapped *APIError (if any) is
// included in the failure message so the test output explains what
// the server rejected, not just "Apply failed".
func MustApply(t testing.TB, r *auth0mock.ResponseBuilder) *auth0mock.RegisteredExpectation {
	t.Helper()
	reg, err := r.Apply(context.Background())
	if err != nil {
		t.Fatalf("auth0mocktest: register expectation: %v", err)
	}
	return reg
}

// MustAdd wraps Expectations.Add with t.Fatalf on error. The non-fluent
// counterpart to MustApply — use when you already have an Expectation
// struct built (e.g. shared across multiple tests). Returns the
// *auth0mock.RegisteredExpectation handle for per-stub operations.
func MustAdd(t testing.TB, c *auth0mock.Client, exp auth0mock.Expectation) *auth0mock.RegisteredExpectation {
	t.Helper()
	reg, err := c.Expectations.Add(context.Background(), exp)
	if err != nil {
		t.Fatalf("auth0mocktest: add expectation: %v", err)
	}
	return reg
}

// MustVerify wraps Client.Expectations.Verify with t.Fatalf on
// constraint violations. Register this as the last t.Cleanup so
// every test that sets Times / AtLeast / AtMost on a stub has its
// expectations checked at test end:
//
//	t.Cleanup(func() { auth0mocktest.MustVerify(t, c) })
//
// Or use Bracket(t, c) for the one-liner that does both Reset and
// Verify automatically.
//
// MustVerify is a no-op when no stub on the Client has a constraint set.
func MustVerify(t testing.TB, c *auth0mock.Client) {
	t.Helper()
	if err := c.Expectations.Verify(context.Background()); err != nil {
		t.Fatalf("auth0mocktest: verify expectations: %v", err)
	}
}

// Bracket is the one-liner setup for the canonical "register stubs +
// assert hits" flow:
//
//	func TestUserLookup(t *testing.T) {
//	    c, err := auth0mock.NewClient(mockURL)
//	    if err != nil { t.Fatal(err) }
//	    auth0mocktest.Bracket(t, c)
//
//	    reg := auth0mocktest.MustApply(t, c.ExpectGet("/api/v2/users/123").
//	        Respond(200).
//	        JSON(map[string]any{"user_id":"123"}))
//	    reg.Times(1)
//
//	    // ... exercise the system under test ...
//	}
//
// Equivalent to writing:
//
//	auth0mocktest.ResetOnCleanup(t, c)            // pre-test reset + register Reset cleanup
//	t.Cleanup(func() { auth0mocktest.MustVerify(t, c) })  // register Verify cleanup
//
// Cleanup ordering is the whole point of Bracket. Go's t.Cleanup is
// LIFO — the cleanup registered LAST fires FIRST. So we register
// Reset's cleanup first (via ResetOnCleanup) and Verify's cleanup
// last; at test end Verify fires first (reads ledger + server state),
// then Reset fires (wipes both). Without Bracket, a user who orders
// these the other way around silently passes every constraint check
// because Reset drops the local verification ledger before Verify
// can inspect it.
func Bracket(t testing.TB, c *auth0mock.Client) {
	t.Helper()
	ResetOnCleanup(t, c)
	t.Cleanup(func() { MustVerify(t, c) })
}
