<div align="center">

# 🔐 auth0-mock

**A drop-in mock of Auth0's HTTP API, both the Authentication API and the Management API, that you can point any Auth0-using service at, with no code changes.**

Real RS256 JWTs · 400+ Mgmt API endpoints · Runtime claim & permission injection · MFA flow · PKCE · OIDC discovery · HTTP & HTTPS

[Quick start](#-quick-start) · [What's mocked](#-whats-mocked) · [Recipes](docs/COOKBOOK.md) · [Architecture](docs/ARCHITECTURE.md) · [Contributing](CONTRIBUTING.md)

</div>

---

## ✨ What is this?

A self-contained Go service that **looks and behaves like Auth0** to a calling client:

- 🎫 **Mints real RS256 JWTs** signed with an in-process key, publishes the matching JWKS at `/.well-known/jwks.json`. Consumer SDKs validate signatures normally, no `InsecureSkipVerify`, no fake-token kludges.
- 📦 **Mocks the Management API spec-completely** by embedding Auth0's published OpenAPI 3.1 document (~400 operations) and routing every endpoint to a single generic handler. You stub responses with `<verb> /api/v2/.../match`; the OpenAPI schema validates the stubbed body.
- 🛠 **Shapes runtime state via HTTP**: custom JWT claims, per-audience permissions, and the MFA-required flag are mutable mid-test through `/admin0/*` endpoints. No restart, no config-file juggling.
- 🐳 **Ships as a single static binary** (~13 MB) or a tiny Docker image. Sub-second boot, both HTTP (`:8080`) and HTTPS (`:8443`) by default.

## 🎯 Who is this for?

Anyone whose service talks to Auth0 in tests or local dev:

- **CI pipelines** that need deterministic Auth0 responses without burning rate limit on a real tenant.
- **Local dev loops** where you don't want to share an Auth0 tenant or wait on its latency.
- **Integration test suites** for Auth0 SPA / native / API SDKs (auth0-react, auth0-js, auth0-spa-js, auth0-android, auth0-swift, auth0-react-native, etc.).
- **Resilience tests** for code paths that hit `/api/v2/users`, `/api/v2/clients`, `/api/v2/roles`, etc.
- **Service-to-service** flows using `client_credentials`, with realistic scopes and `permissions` claim shapes.

It's NOT for: production traffic, replacing your IdP, or anything that needs a real RBAC engine.

## 🚀 Quick start

### Local binary

```bash
make build && ./bin/auth0-mock
```

### Live-reload dev loop (`air`)

Sub-second rebuild on every save under `cmd/` or `internal/`, no docker, no bind-mounts, no flakiness:

```bash
make watch     # installs air into ./bin on first run
```

### Docker

```bash
docker compose up -d --build
docker compose logs -f auth0-mock
```

`docker compose` runs the same production-grade image used for releases. After source changes, restart with `--build`.

### Smoke test

```bash
# 1. Mint a real signed access token
TOKEN=$(curl -s -X POST http://localhost:8080/oauth/token \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'grant_type=client_credentials&client_id=demo&client_secret=x&audience=http://localhost:8080/api/v2/' \
  | jq -r .access_token)

# 2. Stub a Mgmt API response
curl -X GET http://localhost:8080/api/v2/users/auth0%7C123/match \
  -H 'Content-Type: application/json' \
  -d '{"status":200,"body":{"user_id":"auth0|123","email":"alice@example.com"}}'

# 3. Call the stubbed endpoint with your bearer
curl http://localhost:8080/api/v2/users/auth0%7C123 \
  -H "Authorization: Bearer ${TOKEN}"
# => {"user_id":"auth0|123","email":"alice@example.com"}
```

That's it. No SDK changes, no monkey-patching, your code calls auth0-mock the same way it calls Auth0.

## Calling the API from Postman or Insomnia

> **Prefer a browser?** Run the mock and open
> [http://localhost:8080/docs](http://localhost:8080/docs) for an interactive
> reference rendered by [Scalar](https://github.com/scalar/scalar) — every
> endpoint clickable, "Try it" pointing at the same instance that served the
> page.

The mock ships a merged OpenAPI 3.1 document that covers every HTTP surface
it exposes:

- The Auth0 **Authentication API** (`/oauth/token`, `/authorize`,
  `/userinfo`, `/v2/logout`, `/dbconnections/*`, `/passwordless/*`).
- The Auth0 **Management API** (everything under `/api/v2`), plus a
  `POST {path}/match` and `POST {path}/reset` sibling for every operation so
  you can programme canned responses from the same collection.
- The **admin0** control plane (`/admin0/*`).
- The **service** endpoints (`/healthz`, `/.well-known/jwks.json`,
  `/openapi.json`, `/openapi.yaml`).

### Importing

- **From the repo:** import `api/auth0-mock.openapi.json` directly.
- **From a running instance:** point your client at
  `http://localhost:8080/openapi.json` (or `/openapi.yaml`).

Both Postman and Insomnia will create a folder per tag (`auth-api`,
`admin0`, `service`, `mock-control`, plus the Mgmt API's existing tags) and
fill in request bodies from the schemas.

### Regenerating the spec

The merged JSON is committed and checked for drift in CI. Re-run
`make openapi` after editing any of:

- `api/mock-control.openapi.yaml`
- `internal/authapi/authapi.openapi.yaml`
- `internal/admin0/admin0.openapi.yaml`
- `internal/router/service.openapi.yaml`
- `api/auth0-management-api.openapi.json`

## 📋 What's mocked

### 🎫 Authentication API (hand-coded, fully functional)

| Endpoint | Method | Notes |
|---|---|---|
| `/oauth/token` | POST | All Auth0 grants (see table below) |
| `/oauth/revoke` | POST | 200 no-op (mock doesn't track refresh state) |
| `/authorize` | GET | 302 with `code` (or implicit token); stashes PKCE challenge if present |
| `/userinfo` | GET | Returns claims from the bearer |
| `/v2/logout` | GET | 302 to `returnTo` |
| `/.well-known/jwks.json` | GET | Real JWKS for the in-process signing key |
| `/.well-known/openid-configuration` | GET | OIDC discovery rooted at the configured issuer |
| `/dbconnections/signup` | POST | Returns `{_id, email, email_verified:false}` |
| `/dbconnections/change_password` | POST | Returns the canned reset-email message |
| `/passwordless/start` | POST | Returns `{_id, email, phone_number}` |
| `/passwordless/verify` | POST | Mints token if `otp=000000` |

### 🔑 OAuth grants supported

| `grant_type` | Notes |
|---|---|
| `client_credentials` | M2M flow, returns `access_token` only |
| `password` | Returns access + id + refresh; gates on the MFA flag |
| `refresh_token` | New `access_token`; no refresh state tracked |
| `authorization_code` | Returns access + id; **enforces PKCE** if challenge was set at `/authorize` |
| `http://auth0.com/oauth/grant-type/password-realm` | Auth0 Native SDKs; same as password + `realm` field threaded into claims |
| `http://auth0.com/oauth/grant-type/passwordless/otp` | Mints if `otp=000000` |
| `http://auth0.com/oauth/grant-type/mfa-otp` | Step 2 of MFA dance; accepts `otp=123456` |
| `http://auth0.com/oauth/grant-type/mfa-oob` | Push/SMS step-up; accepts `binding_code=123456` |
| `http://auth0.com/oauth/grant-type/mfa-recovery-code` | Recovery flow; accepts `recovery_code=ABCDEFGHIJKLMNOP` |

### 📦 Management API (spec-driven, ~400 endpoints)

Every operation in Auth0's published OpenAPI 3.1 spec is mounted. Default response is `404 no_match`. Tests register stubs:

```bash
# Concrete-id stub
curl -X GET http://localhost:8080/api/v2/users/auth0%7C123/match \
  -H 'Content-Type: application/json' \
  -d '{"status":200,"body":{"user_id":"auth0|123","email":"alice@x"}}'

# Template stub (catch-all for any user id)
curl -X GET http://localhost:8080/api/v2/users/{id}/match \
  -H 'Content-Type: application/json' \
  -d '{"status":200,"body":{"user_id":"auth0|*","email":"any@x"}}'
```

> Concrete URL stubs win over template stubs. Schemas are validated at registration time, invalid bodies are rejected with `400 invalid_match`. `/match` siblings mirror the original verb (so for `GET /users/{id}` the sibling is `GET …/match`, yes, GET-with-body).

### 🛠 Admin surface (no auth, JSON-driven)

| Endpoint | Method | Purpose |
|---|---|---|
| `/admin0/reset` | POST | Wipe everything: matches, claims, permissions, MFA flag |
| `/admin0/matches` | GET | List currently registered match stubs |
| `/admin0/claims` | GET / PUT / DELETE | Custom claims merged into every minted JWT |
| `/admin0/permissions` | GET / DELETE | All audiences and their permissions |
| `/admin0/permissions/{audience}` | GET / PUT / DELETE | Per-audience RBAC injection (audience may be a URL, chi wildcard) |
| `/admin0/mfa-required` | GET / PUT | Toggle MFA enforcement at runtime |

### 🩺 Operations

| Endpoint | Notes |
|---|---|
| `/healthz` | Kubernetes-style liveness probe, `200 {"status":"ok"}`, no auth |

## 💡 Common recipes

→ See [`docs/COOKBOOK.md`](docs/COOKBOOK.md) for full recipes. Highlights:

```bash
# Inject a custom claim into every token
curl -X PUT http://localhost:8080/admin0/claims \
  -H 'Content-Type: application/json' \
  -d '{"role":"admin","org_id":"o-42"}'

# Set RBAC for an audience (URL-form audience works thanks to chi wildcard)
curl -X PUT http://localhost:8080/admin0/permissions/https://api.example.com/ \
  -H 'Content-Type: application/json' \
  -d '["read:users","write:users"]'

# Force MFA on the next password grant
curl -X PUT http://localhost:8080/admin0/mfa-required \
  -H 'Content-Type: application/json' \
  -d '{"required":true}'

# Reset everything between tests
curl -X POST http://localhost:8080/admin0/reset
```

## 🛠 Configuration

Environment variables (see [`.env.example`](.env.example) for the full template):

| Variable | Default | Notes |
|---|---|---|
| `HTTP_ADDR` | `127.0.0.1:8080` | Empty disables the HTTP listener. Set to `0.0.0.0:8080` to accept LAN/container traffic (the Dockerfile already does). |
| `HTTPS_ADDR` | `127.0.0.1:8443` | Empty disables the HTTPS listener. Set to `0.0.0.0:8443` to accept LAN/container traffic (the Dockerfile already does). |
| `TLS_CERT_FILE` / `TLS_KEY_FILE` | _empty_ | If both set → load. Else → auto-generate (see TLS section) |
| `TLS_CACHE_DIR` | _empty_ | If set, persist auto-gen cert to `<dir>/tls.{crt,key}` and reuse on restart |
| `TLS_HOSTNAMES` | `localhost,127.0.0.1,::1` | SAN entries on the auto-generated cert |
| `SIGNING_KEY_FILE` | _empty_ | PEM-encoded RSA key. Otherwise a fresh RS256 key is generated each boot |
| `ISSUER_URL` | `https://localhost:8443/` | `iss` claim and OIDC discovery base |
| `DEFAULT_AUDIENCE` | `https://localhost:8443/api/v2/` | Default `aud` if request doesn't supply one |
| `ACCESS_TOKEN_TTL` | `24h` | Minted access token lifetime |
| `ID_TOKEN_TTL` | `24h` | Minted ID token lifetime |
| `SPEC_VALIDATION_STRICT` | `true` | If `false`, runtime response re-check (defence in depth) logs but doesn't fail |
| `LOG_LEVEL` | `info` | zerolog levels |
| `READ_HEADER_TIMEOUT` | `5s` | http.Server's `ReadHeaderTimeout` |
| `SHUTDOWN_TIMEOUT` | `5s` | Graceful-shutdown grace period |

## 🔒 HTTPS / TLS

The auto-generated cert covers `localhost`, `127.0.0.1`, `::1` (override with `TLS_HOSTNAMES`). Identical TLS behaviour on macOS and Linux, but it's self-signed, so clients reject it unless told otherwise. Three options:

> **⚠️ macOS gotcha**: Go on **macOS pulls trust roots from the system Security framework and ignores `SSL_CERT_FILE` / `SSL_CERT_DIR`** (Linux Go honors them). The Linux `SSL_CERT_FILE=./tls.crt go run …` trick simply doesn't work on macOS. On macOS, trust the cert via `mkcert` (option 1 below, easiest), or import it into the keychain (`security add-trusted-cert …`, recipe in [`docs/COOKBOOK.md`](docs/COOKBOOK.md#trusting-the-self-signed-cert)), or build a custom `tls.Config{RootCAs: pool}` in your client code.

### 1. `mkcert` (recommended for local dev)

[`mkcert`](https://github.com/FiloSottile/mkcert) installs a local CA into your platform's trust store and signs certs with it. Browsers, Go, and `curl` accept the result without flags:

```bash
mkcert -install                                                # one-time per workstation
mkcert -cert-file tls.crt -key-file tls.key localhost 127.0.0.1 ::1

docker run -e TLS_CERT_FILE=/certs/tls.crt -e TLS_KEY_FILE=/certs/tls.key \
  -v "$PWD:/certs" auth0-mock
```

### 2. `TLS_CACHE_DIR` (recommended for `docker compose` without mkcert)

Pick a path; the mock writes its auto-generated cert there on first boot and reuses it on subsequent restarts. Trust the cert once and trust persists:

```bash
docker compose run --rm -e TLS_CACHE_DIR=/data/tls \
  -v auth0-mock-tls:/data/tls auth0-mock
```

### 3. Skip verification

Fine for ephemeral tests, ugly for anything else:

```bash
curl -k https://localhost:8443/.well-known/openid-configuration
# Go: &tls.Config{InsecureSkipVerify: true}
```

To install the mock's cert into your OS trust store (after option 2 so it's stable across boots), see [`docs/COOKBOOK.md`](docs/COOKBOOK.md#trusting-the-self-signed-cert).

## 🧪 Testing the mock

```bash
go test -race ./...                        # unit tests
go test -tags=features ./cmd/api/...       # godog acceptance suite (63 scenarios)
```

The godog harness boots the service in-process on a random port and exercises every Auth API path, every admin endpoint, and the spec-driven Mgmt API surface. See [`features/`](features/) for the gherkin and [`features/scenario/`](features/scenario/) for the harness.

## 🏗 Architecture

→ Full deep-dive: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

At-a-glance:

```
chi router
  ├── recovery + request_id + logging       (always-on middleware)
  ├── /healthz                               liveness
  ├── /admin0/{reset, matches, claims, permissions/*, mfa-required}
  │                                          control plane (no auth)
  ├── /.well-known/{jwks.json, openid-configuration}
  ├── /oauth/* /authorize /userinfo /v2/logout
  │   /dbconnections/* /passwordless/*
  │                                          Auth API (hand-coded, functional)
  └── /api/v2/*                              Management API (spec-driven)
        /api/v2/.../{verb}                   ← bearer-enforced; generic handler
        /api/v2/.../{verb}/match             ← stub register (no bearer)
        /api/v2/.../{verb}/reset             ← stub clear (no bearer)
```

Every handler is a struct holding its dependencies as fields, implementing `http.Handler` via `ServeHTTP`. JSON responses go through `go-chi/render`.

## 📂 Example consumer

[`examples/consumer/`](examples/consumer/) is a stand-alone Go program that proves the drop-in compatibility end to end: mints a token, verifies its signature against `/.well-known/jwks.json` using the standard `MicahParks/keyfunc` + `golang-jwt/jwt` libraries (NOT the mock's internals), registers a Mgmt API stub, and calls the stubbed endpoint.

```bash
go run ./cmd/api &
go run ./examples/consumer
```

## 🤝 Contributing

PRs welcome. See [`CONTRIBUTING.md`](CONTRIBUTING.md) for local setup, code style, testing requirements, and how to add a new endpoint.

## 📖 Documentation map

| File | Audience | Purpose |
|---|---|---|
| [`README.md`](README.md) (this file) | Everyone | Overview, quick start, configuration |
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | Contributors / curious users | How the service is structured internally |
| [`docs/COOKBOOK.md`](docs/COOKBOOK.md) | Test authors | Recipes for common test scenarios |
| [`CONTRIBUTING.md`](CONTRIBUTING.md) | Contributors | Dev setup, conventions, PR workflow |
| [`CHANGELOG.md`](CHANGELOG.md) | Everyone | What changed between versions |
| [`examples/consumer/README.md`](examples/consumer/README.md) | Test authors | Worked end-to-end example |

## ⚖️ License

[MIT](LICENSE).
