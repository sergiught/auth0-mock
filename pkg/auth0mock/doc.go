// Package auth0mock is the Go SDK for the auth0-mock control plane.
//
// It wraps the /admin0/* HTTP surface of a running auth0-mock instance
// with a typed Go API, so test code can register stubs, inject custom
// JWT claims, set per-audience permissions, toggle MFA, and freeze /
// advance the mock's clock without hand-marshalling JSON.
//
// This SDK is NOT for calling the mocked Auth0 APIs (/oauth/*, /api/v2/*) —
// point your existing Auth0 SDK (auth0-go, auth0-js) at the mock's
// base URL for that. The SDK is purely for shaping the mock's fixture
// state from Go code.
//
// # Running a mock
//
// The SDK assumes an auth0-mock instance is already reachable. Start
// one with Docker or the binary before any SDK call:
//
//	docker run --rm -p 8080:8080 ghcr.io/sergiught/auth0-mock:latest
//	# or, from a source checkout: make watch
//
// # Quick start
//
//	import (
//	    "github.com/sergiught/auth0-mock/pkg/auth0mock"
//	    "github.com/sergiught/auth0-mock/pkg/auth0mock/auth0mocktest"
//	)
//
//	func TestUserLookup(t *testing.T) {
//	    ctx := context.Background()
//	    c, err := auth0mock.NewClient("http://localhost:8080")
//	    if err != nil {
//	        t.Fatal(err)
//	    }
//
//	    // Reset on entry + exit, Verify all constraints at exit, in
//	    // the correct LIFO order. The recommended one-liner setup.
//	    auth0mocktest.Bracket(t, c)
//
//	    // Stub a Management API response via the fluent builder.
//	    // Apply returns a handle (*RegisteredExpectation) the rest of
//	    // the test can chain hit-count constraints onto — discard with
//	    // _ if you don't need it.
//	    reg, err := c.ExpectGet("/api/v2/users/auth0|alice").
//	        Respond(200).
//	        JSON(map[string]any{"user_id": "auth0|alice", "email": "alice@example.com"}).
//	        Apply(ctx)
//	    if err != nil {
//	        t.Fatal(err)
//	    }
//	    reg.Times(1) // Bracket's cleanup-side Verify will fail if not hit exactly once.
//
//	    // Hand-build form if you already have an Expectation structured:
//	    _, err = c.Expectations.Add(ctx, auth0mock.Expectation{
//	        Method: "GET", Path: "/api/v2/users/{id}",
//	        Response: auth0mock.ResponseDef{
//	            Status: 200,
//	            Body:   auth0mock.MustJSON(map[string]any{"user_id": "any"}),
//	        },
//	    })
//	    if err != nil {
//	        t.Fatal(err)
//	    }
//
//	    // ... call the system-under-test here ...
//	}
//
// See [examples/sdk] in the repo for a runnable walk-through of every
// resource (expectations, claims, permissions, MFA), and
// [examples/consumer] for the worked-flow example with the real
// go-auth0 SDK calling through the mock.
//
// # Stability
//
// This package's API is unstable until the auth0-mock module reaches
// v1.0.0. Pin a tagged version (e.g. v0.226.0) and treat any minor
// bump as potentially breaking. The package doc will call out the
// stability promise when v1 is approaching.
//
// # No authentication
//
// /admin0/* is unauthenticated by design — there is no token to pass.
// Bind the mock to localhost or keep it inside your CI container;
// never expose it to an untrusted network.
//
// [examples/sdk]: https://github.com/sergiught/auth0-mock/tree/main/examples/sdk
// [examples/consumer]: https://github.com/sergiught/auth0-mock/tree/main/examples/consumer
package auth0mock
