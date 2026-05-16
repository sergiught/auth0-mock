# Architecture

A walkthrough of how auth0-mock is put together, written for contributors and curious users. If you want a quick mental model rather than the deep dive, the [README](../README.md#-architecture) has a 10-line diagram.

## Big picture

```
┌──────────────────────────────────────────────────────────────────────────┐
│  Servers                                                                  │
│   ├── HTTP   :8080                                                        │
│   └── HTTPS  :8443  (auto-gen self-signed cert; mountable; persistable)  │
└─────────────────────────┬────────────────────────────────────────────────┘
                          │
                          ▼
┌──────────────────────────────────────────────────────────────────────────┐
│  chi router (go-chi/chi v5)                                              │
│                                                                          │
│   middleware (in order, applied to every route):                         │
│     1. RequestID    (UUID per request, returned as X-Request-Id header)  │
│     2. Recovery     (panic → 500)                                        │
│     3. Logging      (zerolog: method, path, status, latency, request_id)│
│                                                                          │
│   /healthz                                  liveness                     │
│   /readyz                                   readiness (same as /healthz) │
│                                                                          │
│   /admin0/{reset, expectations, claims,      control plane (no bearer)    │
│            permissions[/{audience}],        - wipes / inspects / shapes  │
│            mfa-required}                    runtime state                │
│                                                                          │
│   /.well-known/jwks.json                    JWKS publication             │
│   /.well-known/openid-configuration         OIDC discovery               │
│                                                                          │
│   /oauth/* /authorize /userinfo             Auth API (hand-coded)        │
│   /v2/logout                                - mints real RS256 JWTs      │
│   /dbconnections/{signup,change_password}                                │
│   /passwordless/{start,verify}                                           │
│                                                                          │
│   /api/v2/*                                 Management API (spec-driven)       │
│       <verb> <path>             ← bearer-enforced + spec-validated +    │
│                                    consults stub store (404 by default)  │
│                                    stubs registered via /admin0/expectations│
└─────────────────────────┬────────────────────────────────────────────────┘
                          │
                          ▼
┌──────────────────────────────────────────────────────────────────────────┐
│  Shared in-process state (no persistence)                                │
│   ├── matches.Store        Management API stub registrations                   │
│   ├── claims.Store         per-process custom JWT claims                 │
│   ├── permissions.Store    per-audience permission slice                 │
│   ├── pkce.Store           authorize_code → code_challenge map           │
│   ├── mfa.Store            mfa_token map + global "required" flag       │
│   ├── jwks.KeySet          RS256 signing key + JWKS publisher           │
│   └── spec.{Spec,Validator} embedded OpenAPI 3.1 + kin-openapi          │
└──────────────────────────────────────────────────────────────────────────┘
```

The two halves work very differently. The Auth API is **hand-coded** because the mock must *do something*: sign JWTs, redirect with real codes, return discovery docs. The Management API is **spec-driven** because all ~400 operations behave the same way (return a registered stub or 404), so one generic handler serves them all.

## Two halves, two strategies

### Authentication API: hand-coded, functional

Every Auth API endpoint is a struct in `internal/authapi/` implementing `http.Handler`:

```go
type TokenHandler struct {
    Keys            *jwks.KeySet
    Issuer          string
    DefaultAudience string
    Log             zerolog.Logger
    Claims          *claims.Store
    Permissions     *permissions.Store
    PKCE            *pkce.Store
    MFA             *mfa.Store
}

func (h *TokenHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) { ... }
```

The handlers actually *do* the OAuth/OIDC dance: parse form or JSON, dispatch on `grant_type`, sign tokens with `jwks.KeySet.Mint`, handle redirects, build the OIDC discovery doc. Tokens minted here are valid RS256 JWTs, signed with an in-process 2048-bit RSA key, with the matching public key published at `/.well-known/jwks.json`.

The full list of grants and their dispatch lives in [`internal/authapi/token.go`](../internal/authapi/token.go).

### Management API: spec-driven, generic

`mgmtapi.Mount` walks the embedded Auth0 Management API skeleton at boot:

```go
for op := range opts.Spec.Operations() {
    base := op.Template                    // "/api/v2/users/{id}"

    r.Method(op.Method, base, bearer.Middleware(opts.Keys)(&GenericHandler{Op: op, ...}))
}
```

One handler per operation, all in `internal/mgmtapi/`:

- **`GenericHandler`**: buffers the incoming request body, validates the request against the operation's OpenAPI schema, looks up the best-matching stub from `matches.Store` using 4-tier precedence (exact-path + request matcher, exact-path + catch-all, template-path + request matcher, template-path + catch-all — newest-wins within each tier), defensively re-validates the stored response against the spec, writes it. Returns `404 no_match` if nothing was registered.

Canned responses are registered out-of-band via `POST /admin0/expectations` (handler in `internal/admin0/expectations.go`), which decodes a `{method, path, request?, response}` payload. `response.body` is validated against the operation's response schema for `response.status`. The optional `request` matcher is validated against the operation's request schema with `required` relaxed (a matcher is partial by design), so unknown and mistyped fields are rejected up front. The validated expectation is appended to an ordered list keyed by `(method, path)` in `matches.Store`. This separation keeps the Management API surface bearer-protected while stub registration requires no token.

### Why use a chi wildcard for paths?

chi uses `{id}` natively (same as OpenAPI), so we pass `op.Template` straight to `r.Method` without translation. For one tricky case, `/admin0/permissions/{audience}` where audiences are often URLs containing slashes, we use chi's `/*` wildcard with `chi.URLParam(r, "*")`.

## OpenAPI export

The mock publishes one merged OpenAPI 3.1 document describing every HTTP surface it exposes — served at `GET /openapi.json` and `GET /openapi.yaml`, and rendered as an interactive reference at `GET /docs`. All three are unauthenticated (`router.MountOpenAPI`).

### The bundler

`cmd/genopenapi` builds the merged document at `make openapi` time from two kinds of input:

- **The Auth0 skeleton** (`api/auth0-management-api.openapi.json`) — a stripped copy of Auth0's Management API spec: paths, methods, parameters, and schema shapes only, with every Auth0-authored `description`, `externalDocs`, and `x-*` extension removed (`stripUpstreamProse`). It is re-vendored from a manually-downloaded raw spec via `make refresh-spec`; the raw download is gitignored and never committed (see CONTRIBUTING.md for the why and how).
- **Per-surface fragments** — hand-written partial OpenAPI docs for the surfaces the mock implements itself: `internal/authapi/authapi.openapi.yaml`, `internal/admin0/admin0.openapi.yaml`, `internal/router/service.openapi.yaml`. Each package `//go:embed`s its own fragment.

The pipeline (`bundle` in `cmd/genopenapi/main.go`):

1. Load the skeleton, prefix its paths with `/api/v2`.
2. `stripUpstreamProse` again: an idempotent safety net in case a non-stripped spec was ever committed.
3. Merge each fragment's paths, schemas, security schemes, and tags (`mergeFragment`), erroring on any path+method or name collision.
4. Rewrite `info` and `externalDocs` to auth0-mock's own identity (`rewriteInfo`).
5. Add the `x-tagGroups` extension (`applyTagGroups`) so the rendered sidebar collapses into four sections — Authentication API, Management API, admin0, Service — instead of ~50 flat tags.

The output, `api/auth0-mock.openapi.json`, is committed, `//go:embed`ed as `api.MockOpenAPIJSON`, and drift-checked in CI (`make openapi` then `git diff --exit-code`).

### Serving it

`/openapi.json` writes the embedded bytes directly. `/openapi.yaml` converts once, lazily, and caches the result. `/docs` is a small static HTML page that loads [Scalar](https://github.com/scalar/scalar) — pinned to an exact version and SRI-guarded — and points it at `/openapi.json`; on load it mints a `client_credentials` token and preloads it into Scalar's auth so the "Try it" panel works against the same instance with no manual setup.

## State stores (all in-memory)

| Store | Owns | Mutated by | Consulted by |
|---|---|---|---|
| [`internal/matches/`](../internal/matches/) | Ordered list of `Expectation` records per `(method, path)` key; each `Expectation` is an optional `RequestMatcher` (subset-matched `query` + `body`) plus a `ResponseDef` (`status`, `headers`, `body`). `Find` applies a 4-tier precedence: exact-path + matcher, exact-path + catch-all, template-path + matcher, template-path + catch-all — newest-wins within each tier. | `POST /admin0/expectations`, `DELETE /admin0/expectations`, `POST /admin0/reset` | `GenericHandler` |
| [`internal/claims/`](../internal/claims/) | Per-process map of custom JWT claims | `PUT/DELETE /admin0/claims`, `POST /admin0/reset` | `TokenHandler.augmentExtra`, `PasswordlessVerifyHandler` |
| [`internal/permissions/`](../internal/permissions/) | `map[audience] → []permission` for RBAC claim injection | `PUT/DELETE /admin0/permissions[/{audience}]`, `POST /admin0/reset` | `TokenHandler.augmentExtra` (looks up by audience), `PasswordlessVerifyHandler` |
| [`internal/pkce/`](../internal/pkce/) | `code → {challenge, method, ...}` with 10-min TTL, single-use | `AuthorizeHandler` (writes), `TokenHandler.respondAuthorizationCode` (reads + consumes) | (none) |
| [`internal/mfa/`](../internal/mfa/) | `mfa_token → Context` with 10-min TTL + atomic "required" flag | `PUT /admin0/mfa-required`, `POST /admin0/reset`, `TokenHandler.requireMFA` | `respondPassword`, `respondPasswordRealm`, `respondMFA*` |

Every store is mutex-protected and snapshot-isolating on reads (so the caller can mutate the returned slice/map without corrupting the store).

There is **no persistence** anywhere. Each process restart is a clean slate. That's a deliberate design choice: tests get isolation for free; you don't need a teardown step beyond `POST /admin0/reset` (which is also called automatically between godog scenarios via the scenario harness).

There are also **no per-store size caps**. The expectations store, MFA token store, PKCE store, and per-audience permissions map all grow without bound as you register more entries. The MFA and PKCE stores sweep their own expired entries on every write (TTL-bounded), but the expectations and permissions stores grow until the process restarts or `/admin0/reset` is called. This is fine for a test harness where each scenario starts clean; it's a footgun if you point the mock at long-lived synthetic load. Treat `/admin0/reset` as your teardown contract.

## Token claim composition

When `/oauth/token` mints a token, the payload is built in this order, **last write wins**:

1. **Reserved claims** built by the grant handler:
   - `iss` (issuer from config)
   - `sub` (depends on grant: `<client_id>@clients` for client_credentials, the username for password, etc.)
   - `aud` (from request, falls back to `DefaultAudience`)
   - `iat`, `exp` (now + `ACCESS_TOKEN_TTL`)
   - `scope` (echoed from request)
   - `azp` (client_id)
   - `gty` (grant type, e.g. `client-credentials`, `password-realm`, `mfa-otp`)
2. **`permissions` claim**: added if `permissions.Store.Get(audience)` returns a non-empty slice. Skipped silently otherwise.
3. **Custom claims** from `claims.Store`: merged in. **These overwrite everything above.** Tests can override `gty`, `azp`, `permissions`, anything.

All of this is encapsulated in `TokenHandler.augmentExtra`:

```go
func (h *TokenHandler) augmentExtra(extra map[string]any, audience string) map[string]any {
    if extra == nil {
        extra = make(map[string]any)
    }
    if h.Permissions != nil {
        if perms := h.Permissions.Get(audience); len(perms) > 0 {
            extra["permissions"] = perms
        }
    }
    if h.Claims != nil {
        h.Claims.MergeInto(extra)
    }
    return extra
}
```

## MFA challenge dance

When `mfa.Store.IsRequired()` is true, `password` and `password-realm` don't mint directly. Instead:

1. **Step 1: challenge issued.** The handler builds an `mfa.Context` snapshot of `{client_id, audience, scope, subject, realm}`, calls `mfa.Store.Issue(ctx)` to get back a UUID `mfa_token`, and responds:

   ```json
   {
     "error": "mfa_required",
     "error_description": "Multifactor authentication required",
     "mfa_token": "<uuid>"
   }
   ```

   HTTP status `403`. The token is good for 10 minutes and is single-use.

2. **Step 2: exchange.** The client re-calls `/oauth/token` with one of the three Auth0 MFA grants plus the canned factor:

   | grant_type | Factor field(s) | Accepted value |
   |---|---|---|
   | `http://auth0.com/oauth/grant-type/mfa-otp` | `otp` | `123456` |
   | `http://auth0.com/oauth/grant-type/mfa-oob` | `oob_code` (any) + `binding_code` | `123456` |
   | `http://auth0.com/oauth/grant-type/mfa-recovery-code` | `recovery_code` | `ABCDEFGHIJKLMNOP` |

   The handler `Consume`s the mfa_token (which expires/removes it), validates the factor against the canned constant, and mints normally using the stored `Context`. The minted token carries `gty=mfa-otp` (or `mfa-oob` / `mfa-recovery-code`) so downstream services can tell step-up tokens apart.

This mirrors Auth0's actual MFA flow shape end-to-end. The canned factors are the only "mocky" part; real Auth0 stores a per-user secret and checks TOTP.

## PKCE flow

When `/authorize` is called with a `code_challenge` (and optionally `code_challenge_method=S256|plain`, defaulting to `plain` per RFC 7636), `AuthorizeHandler` stashes the challenge keyed by the generated `code`:

```go
h.PKCE.Put(issuedCode, pkce.Entry{
    Challenge: challenge,
    Method:    pkce.Method(method),
    ClientID:  clientID,
    Redirect:  redirect,
})
```

Later, when `/oauth/token` receives `grant_type=authorization_code` with that code, `TokenHandler.respondAuthorizationCode` consults the PKCE store:

```go
if entry, ok := h.PKCE.Consume(req.Code); ok {
    if err := entry.Verify(req.CodeVerifier); err != nil {
        // 400 invalid_grant
    }
}
```

`Consume` removes the entry (single-use). `Verify` runs the spec algorithm: `S256` hashes the verifier with SHA-256 and base64url-encodes, comparing to the stored challenge; `plain` compares the verifier to the challenge byte-for-byte.

**Codes that never came through `/authorize`** (or arrived without a `code_challenge`) still mint, backward-compatible with tests that don't care about PKCE.

## Error response shapes

Two distinct shapes, matching real Auth0:

**Management API** (`/api/v2/*`, `/admin0/*`):
```json
{ "statusCode": 400, "error": "Bad Request", "message": "...", "errorCode": "invalid_body" }
```

**Authentication API** (`/oauth/*`, `/authorize`, `/userinfo`, `/dbconnections/*`, `/passwordless/*`):
```json
{ "error": "invalid_request", "error_description": "..." }
```

Selected by the caller via `httperr.WriteMgmt(...)` or `httperr.WriteAuth(...)`.

## TLS lifecycle

`internal/tlscert/Load` has three modes, checked in order:

1. If `TLS_CERT_FILE` AND `TLS_KEY_FILE` are both set → load from those paths.
2. Else if `TLS_CACHE_DIR` is set → load `<dir>/tls.{crt,key}` if they exist, otherwise generate a fresh self-signed cert + persist it there for next boot.
3. Else → generate a fresh self-signed cert in-memory only (regenerated on every boot).

The auto-generated cert is ECDSA P-256, valid for 365 days, with SAN entries split between `DNSNames` and `IPAddresses` per `net.ParseIP`. Default SAN list: `localhost`, `127.0.0.1`, `::1`. Override with `TLS_HOSTNAMES`.

## Why these choices

- **chi over http.ServeMux**: clean middleware composition, native `{id}` path params, wildcard support for the URL-form audiences case.
- **go-chi/render over hand-rolled JSON**: uniform `render.JSON(w, r, body)` + `render.Status(r, code)` everywhere; no scattered `json.NewEncoder(w).Encode(...)` calls with their own content-type handling.
- **kin-openapi v0.x for OpenAPI**: only Go OpenAPI library that handles 3.1 well enough for Auth0's spec. We had to add `DisableSchemaPatternValidation` + `DisableSchemaDefaultsValidation` to its legacy router to cope with Auth0's regex lookahead patterns (Go's regexp doesn't support them).
- **golang-jwt/jwt v5**: RS256 mint/verify with proper signing-method validation, native `WithIssuer` + `WithExpirationRequired`. We chose RS256 only on purpose: it's Auth0's default; HS256 is legacy; PS256 is Enterprise-tier; ES256/EdDSA aren't Auth0 features.
- **Per-process in-memory stores, no persistence**: tests get isolation for free; `POST /admin0/reset` is a single round-trip cleanup; no schema migrations, no garbage in `/tmp`.
- **Embedded OpenAPI skeleton via `//go:embed`**: a stripped skeleton of Auth0's spec (paths, methods, schemas — Auth0's prose removed) is the source of truth for the Management API surface. Refresh it with `make refresh-spec`, which strips a manually-downloaded raw spec into the committed skeleton; see CONTRIBUTING.md.

## Project layout (recap)

```
auth0-mock/
├── cmd/api/main.go                 entrypoint (wires config + stores + router)
├── api/                            //go:embed Auth0 API skeleton + merged spec
├── internal/                       private packages (see CONTRIBUTING.md)
├── features/                       godog acceptance tests
└── examples/consumer/              end-to-end Go consumer
```

Full file-level breakdown is in [`CONTRIBUTING.md`](../CONTRIBUTING.md#-project-layout).
