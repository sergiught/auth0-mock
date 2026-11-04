---
title: Go SDK
description: Use the typed Go client and test helpers to drive auth0-mock from Go tests.
sidebar:
  order: 8
---

The typed Go client for the control plane, the example consumer, and booting the mock from a Go test.

## Go SDK

[`pkg/auth0mock`](https://github.com/sergiught/auth0-mock/blob/main/pkg/auth0mock) is the typed Go client for the `/admin0/*` control plane. Use it from Go test code to register stubs, inject claims, set per-audience permissions, and toggle MFA — without hand-marshalling JSON.

```go
import (
    "testing"

    "github.com/sergiught/auth0-mock/pkg/auth0mock"
    "github.com/sergiught/auth0-mock/pkg/auth0mock/auth0mocktest"
)

func TestUserLookup(t *testing.T) {
    // /admin0 listens on HTTP (8080) by default — no TLS dance needed.
    // For HTTPS (8443) pair with `auth0mock.WithHTTPClient(tlsClient)`,
    // see examples/sdk for the pattern.
    c, err := auth0mock.NewClient("http://localhost:8080")
    if err != nil {
        t.Fatal(err)
    }
    // Reset on entry + exit, Verify all constraints at exit, in the
    // correct LIFO order. One line replaces two and removes the
    // "constraint silently dropped" footgun that comes from getting
    // the cleanup order wrong.
    auth0mocktest.Bracket(t, c)

    // Register a stub and demand it's hit exactly once.
    reg := auth0mocktest.MustApply(t, c.ExpectGet("/api/v2/users/auth0|alice").
        Respond(200).
        JSON(map[string]any{"user_id": "auth0|alice", "email": "alice@example.com"}))
    reg.Times(1)

    // ... your code under test calls the mock the same way it calls Auth0 ...
}
```

What's covered:

| Resource | Read | Write | Wipe | Verify |
|---|---|---|---|---|
| `Expectations` | `List` | `Add`, fluent `ExpectGet/Post/Put/Patch/Delete(...).Respond(...).JSON(...).Then(...).Apply(ctx)` | `Clear`, `ClearOp(method, path)` | `Verify(ctx)`; per-stub: `Hits(ctx)`, `Times(n)`, `AtLeast(n)`, `AtMost(n)`, `AnyTimes()` |
| `Claims` | `Get` | `Set` | `Clear` | — |
| `Permissions` | `All`, `Get(audience)` | `Set(audience, perms)` | `Clear`, `Delete(audience)` | — |
| `MFA` | `Get` | `Set` | (use `Set(ctx, false)`) | — |
| `AuthorizeErrors` | `List` | `Add` | `Clear`, `Delete(id)` | — |
| `Clock` | `Get` | `Freeze(ctx, t)`, `Offset(ctx, d)`, `Advance(ctx, d)` | `Reset` | — |
| `Events` | `Subscribers` (live + lifetime counts; `auth0mocktest.WaitForActiveSubscribers(t, c, want, timeout)` polls until it settles) | `Push(ctx, payload)` | `ExpireEvents` / `ExpireEventsBefore(ctx, cursor)` age out replay cursors so a resume gets `410 event_aged_out`; top-level `Reset` drains subscribers + replay buffer | — |
| `Journal` | `List(ctx, JournalFilter)`, `Unmatched(ctx, JournalFilter)`; `auth0mocktest.MustFindCall(t, c, method, path)` returns the newest matching call | — | `Clear` | — |
| top-level | — | — | `Reset` — wipes every store (including the clock back to real mode) | `auth0mocktest.Bracket(t, c)` — recommended one-liner: pre-test reset + post-test Reset + post-test Verify, all in correct LIFO order |

`Apply(ctx)` and `Expectations.Add(ctx, ...)` return a `*RegisteredExpectation` handle — chain `.Times(n)` / `.AtLeast(n)` / `.AtMost(n)` on it to set hit-count constraints, then `MustVerify` (or `Verify(ctx)` for the error-returning variant) checks every constraint at test end. Discard the handle with `_ = …Apply(ctx)` if you don't need it.

**Which helper?** Use `auth0mocktest.Bracket(t, c)` for every test that wants hit-count assertions — one line wires Reset on entry, Reset on exit, and Verify on exit in the correct LIFO order. Use `auth0mocktest.ResetOnCleanup(t, c)` when you only want isolation (no Times/AtLeast/AtMost constraints anywhere in the test).

**When a stub doesn't match,** the mock returns `{"errorCode":"no_match"}` with HTTP 404 to the SUT, and the 404 `message` names the fields where the closest stub differed. `c.Journal.Unmatched(ctx, auth0mock.JournalFilter{})` gives the full near misses (each stub's differing fields with wanted and received values), and `Verify` prints them automatically under any stub that was hit too few times. To assert on what the SUT actually sent, use `auth0mocktest.MustFindCall(t, c, "PATCH", "/api/v2/users/123")`; if nothing matches, it lists the recent calls of that method.

What's NOT covered: the Auth0 APIs themselves (`/oauth/*`, `/api/v2/*`) — point your existing Auth0 SDK at the mock for those. The SDK only wraps the test-fixture-shaping surface.

A runnable end-to-end walk-through lives at [`examples/sdk/`](https://github.com/sergiught/auth0-mock/blob/main/examples/sdk/) — its own Go module (with a local-path `replace` so the example doubles as a copy-and-pin template). The example drives stubs registered through this SDK with the real [`go-auth0`](https://github.com/auth0/go-auth0) SDK end-to-end (token mint → typed Management API call → Verify the stub was hit). Defaults to `https://localhost:8443` because go-auth0 only speaks TLS.

```bash
make demo-sdk                                      # full setup: mock + TLS cert + run + teardown
# or, against an already-running mock:
cd examples/sdk && go run . -cert=/path/to/tls.crt # full chain verification
cd examples/sdk && go run .                        # InsecureSkipVerify fallback (demo only)
```

Full godoc: [pkg.go.dev/github.com/sergiught/auth0-mock/pkg/auth0mock](https://pkg.go.dev/github.com/sergiught/auth0-mock/pkg/auth0mock).

:::note
The SDK's API is **unstable until v1.0.0**. Pin a tagged version (`go get github.com/sergiught/auth0-mock@v0.227.0` or later) and treat any minor bump as potentially breaking.
:::

## Example consumer

[`examples/consumer/`](https://github.com/sergiught/auth0-mock/blob/main/examples/consumer/) is a stand-alone Go program that proves the drop-in compatibility end to end: mints a token, verifies its signature against `/.well-known/jwks.json` using the standard `MicahParks/keyfunc` + `golang-jwt/jwt` libraries (NOT the mock's internals), registers a Management API stub, and calls the stubbed endpoint.

```bash
make demo                # builds the mock, runs the example end-to-end over HTTPS, cleans up
```

Under the hood, `make demo` boots the binary with a persisted self-signed cert, waits for `/healthz`, runs `examples/consumer` against it, and tears the mock down on exit. To run by hand instead:

```bash
go run ./cmd/api &
go run ./examples/consumer
```

## Use a Go test that boots the mock in-process

For stub registration, claim injection, permission setup, and clock control from Go test code, use [`pkg/auth0mock`](https://github.com/sergiught/auth0-mock/blob/main/pkg/auth0mock) — a typed client over `/admin0/*` that removes the JSON-marshalling boilerplate. See the [Go SDK section](/auth0-mock/guides/go-sdk/#go-sdk) for the API surface and [`examples/sdk/`](https://github.com/sergiught/auth0-mock/blob/main/examples/sdk/) for a worked end-to-end walk-through.

For booting the mock itself: subprocess from `TestMain` (see [`examples/consumer/main.go`](https://github.com/sergiught/auth0-mock/blob/main/examples/consumer/main.go)) or in-process on a random port (see [`features/scenario/context.go`](https://github.com/sergiught/auth0-mock/blob/main/features/scenario/context.go), the pattern the godog suite uses).
