package auth0mock_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/sergiught/auth0-mock/pkg/auth0mock"
)

// ExampleNewClient shows the smallest possible round-trip — bind to a
// running mock and clear its state. NewClient returns an error if
// baseURL is empty, unparsable, or missing a scheme or host (e.g.
// "localhost:8080" without "http://"); failing fast here keeps typos
// from looking like cryptic transport errors on the first SDK call.
func ExampleNewClient() {
	c, err := auth0mock.NewClient("http://localhost:8080")
	if err != nil {
		log.Fatal(err)
	}
	if err := c.Reset(context.Background()); err != nil {
		log.Fatal(err)
	}
}

// ExampleClient_ExpectGet shows the recommended fluent registration
// path for the 80% case — stub a single GET, respond with a JSON body.
func ExampleClient_ExpectGet() {
	c, _ := auth0mock.NewClient("http://localhost:8080")

	_, err := c.ExpectGet("/api/v2/users/auth0|alice").
		Respond(200).
		JSON(map[string]any{
			"user_id": "auth0|alice",
			"email":   "alice@example.com",
		}).
		Apply(context.Background())
	if err != nil {
		log.Fatal(err)
	}
}

// ExampleClient_Expect shows the request-matched variant — when one
// operation needs to return different responses based on the request
// body. The catch-all (no WithBodyJSON) covers everything else.
func ExampleClient_Expect() {
	c, _ := auth0mock.NewClient("http://localhost:8080")
	ctx := context.Background()

	// Catch-all stub: any POST to /api/v2/users returns u_default.
	_, _ = c.ExpectPost("/api/v2/users").
		Respond(201).
		JSON(map[string]any{"id": "u_default"}).
		Apply(ctx)

	// Higher-priority stub: only matches when the body has name=alice.
	_, _ = c.ExpectPost("/api/v2/users").
		WithBodyJSON(map[string]any{"name": "alice"}).
		Respond(201).
		JSON(map[string]any{"id": "u_alice"}).
		Apply(ctx)
}

// Example_testIntegration shows the canonical bracket pattern using
// the auth0mocktest subpackage. Drop into every test that touches
// the mock so each test starts from a known-empty state and leaves no
// expectations behind. The auth0mocktest helpers live in the subpackage
// so production binaries that import auth0mock don't transitively
// import the testing package.
//
//	import (
//	    "github.com/sergiught/auth0-mock/pkg/auth0mock"
//	    "github.com/sergiught/auth0-mock/pkg/auth0mock/auth0mocktest"
//	)
//
//	func TestUserLookup(t *testing.T) {
//	    c, _ := auth0mock.NewClient("http://localhost:8080")
//	    auth0mocktest.Bracket(t, c)
//
//	    reg := auth0mocktest.MustApply(t, c.ExpectGet("/api/v2/users/123").
//	        Respond(200).
//	        JSON(map[string]any{"user_id": "123"}))
//	    reg.Times(1)
//
//	    // ... call the system-under-test here ...
//	}
func Example_testIntegration() {
	c, _ := auth0mock.NewClient("http://localhost:8080")
	_ = c
}

// ExampleClient_Claims shows the runtime-claim injection path. Every
// token minted by the mock after this call carries the registered
// claims merged into both id_token and access_token.
func ExampleClient_Claims() {
	c, _ := auth0mock.NewClient("http://localhost:8080")

	if err := c.Claims.Set(context.Background(), map[string]any{
		"https://example.com/tenant": "acme",
		"https://example.com/role":   "admin",
	}); err != nil {
		log.Fatal(err)
	}
}

// ExampleClient_Permissions shows the per-audience permission setup.
// The audience can be any string — Auth0-style URL audiences (with
// slashes) work natively without escaping.
func ExampleClient_Permissions() {
	c, _ := auth0mock.NewClient("http://localhost:8080")
	ctx := context.Background()

	if err := c.Permissions.Set(ctx, "https://api.example.com/", []string{
		"read:users", "write:users",
	}); err != nil {
		log.Fatal(err)
	}

	// Read it back — the slice round-trips verbatim.
	perms, err := c.Permissions.Get(ctx, "https://api.example.com/")
	if err != nil {
		log.Fatal(err)
	}
	_ = perms
}

// ExampleClient_MFA shows the process-wide MFA toggle. Affects the
// password and password-realm grants only; other grants are unaffected.
func ExampleClient_MFA() {
	c, _ := auth0mock.NewClient("http://localhost:8080")
	ctx := context.Background()

	// Force MFA challenge on the next password grant.
	if err := c.MFA.Set(ctx, true); err != nil {
		log.Fatal(err)
	}

	required, err := c.MFA.Get(ctx)
	if err != nil {
		log.Fatal(err)
	}
	_ = required
}

// ExampleAPIError shows how to extract the typed error from a server-
// side validation failure. The mock returns the same Auth0-shaped
// envelope on every /admin0 error, so errors.As gives you all four
// fields without casting through interface{}.
func ExampleAPIError() {
	c, _ := auth0mock.NewClient("http://localhost:8080")

	_, err := c.Expectations.Add(context.Background(), auth0mock.Expectation{
		Method:   "GET",
		Path:     "/api/v2/users",
		Response: auth0mock.ResponseDef{}, // Missing Status → server rejects.
	})

	var apiErr *auth0mock.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest {
		fmt.Println("validation failed:", apiErr.ErrorCode, apiErr.Message)
	}
}

// ExampleExpectationsClient_Verify shows the canonical verification
// flow: register stubs, chain hit-count constraints onto the handles
// they return, run the test, and check every constraint at the end.
//
// In real test code prefer auth0mocktest.MustVerify(t, c), which
// t.Fatals on the error so the cleanup site reads as one line.
func ExampleExpectationsClient_Verify() {
	c, _ := auth0mock.NewClient("http://localhost:8080")
	ctx := context.Background()

	// "exactly once" — typical for one-shot operations.
	reg1, _ := c.ExpectGet("/api/v2/users/auth0|alice").
		Respond(200).
		JSON(map[string]any{"user_id": "auth0|alice"}).
		Apply(ctx)
	reg1.Times(1)

	// "at least once" — useful when the SUT may retry.
	reg2, _ := c.ExpectPost("/api/v2/users").
		Respond(201).
		JSON(map[string]any{"id": "new"}).
		Apply(ctx)
	reg2.AtLeast(1)

	// "never hit" — guards against the SUT accidentally calling a
	// fallback path you didn't expect.
	reg3, _ := c.ExpectDelete("/api/v2/users/auth0|alice").
		Respond(204).
		Apply(ctx)
	reg3.Times(0) // Equivalent to .AtMost(0).

	// ... Exercise the system-under-test here ...

	// Verify joins every violation into one error. Returns nil when
	// every constraint is satisfied or no constraint was set.
	if err := c.Expectations.Verify(ctx); err != nil {
		log.Fatal(err)
	}
}

// ExampleClient_Expect_resolutionRules shows the four-tier resolution
// rule end-to-end: a concrete-path stub beats a template stub, and
// within a path a request-matched stub beats a catch-all.
func ExampleClient_Expect_resolutionRules() {
	c, _ := auth0mock.NewClient("http://localhost:8080")
	ctx := context.Background()

	// Tier 1 — template catch-all. Wins for any user id NOT named below.
	_, _ = c.ExpectGet("/api/v2/users/{id}").
		Respond(200).
		JSON(map[string]any{"user_id": "any"}).
		Apply(ctx)

	// Tier 2 — concrete-path stub beats the template for this id.
	_, _ = c.ExpectGet("/api/v2/users/auth0|alice").
		Respond(200).
		JSON(map[string]any{"user_id": "auth0|alice"}).
		Apply(ctx)

	// Tier 3 — request-matched stub beats the catch-all on POST /users.
	_, _ = c.ExpectPost("/api/v2/users").
		Respond(201).
		JSON(map[string]any{"id": "default"}).
		Apply(ctx)
	_, _ = c.ExpectPost("/api/v2/users").
		WithBodyJSON(map[string]any{"name": "alice"}).
		Respond(201).
		JSON(map[string]any{"id": "u_alice"}).
		Apply(ctx)
}
