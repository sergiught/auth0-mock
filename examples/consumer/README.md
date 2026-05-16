# Consumer example

Proves that **auth0-mock is a drop-in for Auth0** from the perspective of
the official [`go-auth0`](https://github.com/auth0/go-auth0) SDK. You write
your code against `go-auth0` exactly as you would for a real tenant; the
only difference is the base URL you point the SDK at.

## 30-second mental model

`auth0-mock` is a single binary with two distinct layers:

```
┌──────────────────────────────────────────────────────────────────────┐
│                    auth0-mock (a single binary)                      │
├──────────────────────────────┬───────────────────────────────────────┤
│  Auth API  —  REAL           │  Management API  —  STUB-DRIVEN       │
│                              │                                       │
│  /oauth/token                │  /api/v2/*   (400+ endpoints)         │
│  /.well-known/jwks.json      │                                       │
│  /.well-known/openid-        │  Replies with whatever you            │
│    configuration             │  registered via the mock's            │
│                              │  POST /admin0/expectations            │
│  Mints REAL RS256 JWTs       │  control plane                        │
└──────────────┬───────────────┴───────────────┬───────────────────────┘
               ▲                               ▲
               │                               │
      go-auth0/authentication           go-auth0/management
```

| Layer            | Endpoints                         | Behaviour                                          |
|------------------|-----------------------------------|----------------------------------------------------|
| Auth API         | `/oauth/token`, `/.well-known/*`  | Real RS256 signing keys, real OIDC discovery doc   |
| Management API   | `/api/v2/*`                       | Stub-driven — replies with whatever you registered |
| Control plane    | `/admin0/*`                       | **Not part of Auth0** — the mock's own setup API   |

The SDK only ever sees the first two. `/admin0` is how *you* tell the mock
what to return for the second one.

## What this example runs

Three phases, all driven by the unmodified go-auth0 SDK:

| # | Phase                                          | SDK calls                                                                            |
|---|------------------------------------------------|--------------------------------------------------------------------------------------|
| 1 | Mint an access token via `client_credentials`  | `authentication.New` → `OAuth.LoginWithClientCredentials`                            |
| 2 | Verify the token against the mock's JWKS       | `MicahParks/keyfunc` + `golang-jwt/jwt` (the libraries a downstream service would use) |
| 3 | Round-trip a client app and a user             | `Client.Create` / `Client.Read`, `User.Create` / `User.Read`                         |

## Run it

One command, from the repo root:

```
make demo
```

That builds the mock binary, starts it on `:8443` with its TLS cert
persisted to `/tmp/auth0-mock-demo-tls`, runs this example against the
mock with **full TLS validation** (the example loads the same cert
into its `RootCAs` pool), and shuts the mock down on exit.

Manually, in two shells:

```
# shell 1 — start the mock with a persisted TLS cert
TLS_CACHE_DIR=/tmp/auth0-mock-tls go run ./cmd/api

# shell 2 — run the example, pointing at that cert
cd examples/consumer && go run . -cert=/tmp/auth0-mock-tls/tls.crt
```

If you skip `-cert`, the example falls back to `InsecureSkipVerify`,
which still works against any local mock but doesn't verify the cert.
Fine for a quick demo, never for anything real.

## What you'll see

```
[1/3] Minted access token via go-auth0 authentication SDK
      eyJhbGciOiJSUzI1NiIs...
[2/3] Verified token signature against the mock's JWKS
      https://localhost:8443/.well-known/jwks.json
[3/3] Drove the Management API through the management SDK
      created + read back client: demo-client-id
      created + read back user:   auth0|demo

Done. go-auth0 SDK works against auth0-mock unchanged.
```

## Why the two layers matter

- **Auth API is real.** The mock generates a fresh RS256 keypair on boot
  and signs every token with it. A downstream service can pick a token up
  and validate it against the mock's published JWKS using stock OSS
  libraries — and it works for the same reason it works against real
  Auth0: the signature math checks out.
- **Management API is stubbed.** When the SDK calls
  `POST /api/v2/clients`, the mock looks up the `(method, path)` pair in
  its expectations store and returns whatever you registered. No real
  database, no real provisioning. This is what lets you write
  deterministic tests without standing up a real tenant.

## How the expectation dance works

Each Management API call you intend to make needs an expectation
registered up-front. The pattern is always the same:

```go
// 1. Tell the mock what to return for (method, path).
registerExpectation(base, hc, expectation{
    Method:   "GET",
    Path:     "/api/v2/clients/demo-client-id",
    Response: stubResponse{Status: 200, Body: stub},
})

// 2. Make the SDK call. It hits the mock, gets the stub back, and
//    unmarshals it into a typed value — none the wiser.
got, _ := api.Client.Read(ctx, "demo-client-id")
```

The `stub` can be any JSON-serialisable value. In this example it's a
real `*management.Client` / `*management.User` so the Go types do the
schema work for you.

## Why a separate Go module

This example lives in its own `go.mod` so the `go-auth0` SDK and its
dependency tree don't leak into the `auth0-mock` module graph. The mock
itself stays SDK-agnostic.

## TLS

The go-auth0 SDK only speaks HTTPS, so the example always targets the
mock's TLS listener. There are two ways to handle the mock's self-signed
cert:

| Mode                    | Trigger             | Trust model                                     |
|-------------------------|---------------------|-------------------------------------------------|
| **Real TLS validation** | `-cert <path>`      | PEM is loaded into `RootCAs`, full chain check  |
| **Skip verification**   | no `-cert` flag     | `InsecureSkipVerify: true` — debug-only escape  |

The recommended setup is to run the mock with `TLS_CACHE_DIR=<dir>` so
it persists its auto-generated cert at `<dir>/tls.crt`, then point the
example at the same file with `-cert=<dir>/tls.crt`. `make demo` does
this for you.

For real deployments (CI, shared dev clusters), prefer trusting the
cert via the OS trust store — see the
[TLS section of the repo README](../../README.md#-https--tls).

## Flags

```
-mock string   auth0-mock base URL (HTTPS)
               (default "https://localhost:8443")
-cert string   PEM file containing the mock's TLS cert; if empty,
               skip verification
```
