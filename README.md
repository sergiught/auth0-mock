<div align="center">

<img src="docs/assets/banner.webp" alt="auth0-mock — a drop-in Auth0 HTTP API mock" width="100%">

# auth0-mock

A drop-in mock of Auth0's HTTP API (Authentication + Management) that you can point any Auth0-using service at, with no code changes.

Real RS256 JWTs. 400+ Management API endpoints. Runtime claim and permission injection. MFA, PKCE, OIDC discovery. HTTP and HTTPS.

[Quick start](#-quick-start) · [What's mocked](#-whats-mocked) · [Recipes](docs/COOKBOOK.md) · [Architecture](docs/ARCHITECTURE.md) · [Contributing](CONTRIBUTING.md)

</div>

[![CI](https://github.com/sergiught/auth0-mock/actions/workflows/ci.yml/badge.svg)](https://github.com/sergiught/auth0-mock/actions/workflows/ci.yml)
[![CodeQL](https://github.com/sergiught/auth0-mock/actions/workflows/codeql.yml/badge.svg)](https://github.com/sergiught/auth0-mock/actions/workflows/codeql.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/sergiught/auth0-mock)](https://goreportcard.com/report/github.com/sergiught/auth0-mock)
[![codecov](https://codecov.io/gh/sergiught/auth0-mock/branch/main/graph/badge.svg)](https://codecov.io/gh/sergiught/auth0-mock)
[![Go Reference](https://pkg.go.dev/badge/github.com/sergiught/auth0-mock.svg)](https://pkg.go.dev/github.com/sergiught/auth0-mock)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/sergiught/auth0-mock/badge)](https://securityscorecards.dev/viewer/?uri=github.com/sergiught/auth0-mock)
[![Signed releases](https://img.shields.io/badge/releases-cosign%20signed-0a7bbb)](#%EF%B8%8F-verifying-releases)
[![govulncheck](https://img.shields.io/badge/govulncheck-passing-2ca02c)](https://github.com/sergiught/auth0-mock/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/sergiught/auth0-mock?sort=semver)](https://github.com/sergiught/auth0-mock/releases)
[![Go version](https://img.shields.io/github/go-mod/go-version/sergiught/auth0-mock)](go.mod)
[![GHCR](https://img.shields.io/badge/ghcr.io-auth0--mock-2496ed?logo=docker&logoColor=white)](https://github.com/sergiught/auth0-mock/pkgs/container/auth0-mock)
[![Go module](https://img.shields.io/badge/go%20install-github.com%2Fsergiught%2Fauth0--mock-00ADD8?logo=go&logoColor=white)](https://pkg.go.dev/github.com/sergiught/auth0-mock/pkg/auth0mock)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Conventional Commits](https://img.shields.io/badge/Conventional%20Commits-1.0.0-fa6673.svg)](https://www.conventionalcommits.org)
[![PRs welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)

---

## 📑 Table of contents

- [✨ What is this?](#-what-is-this)
- [🎯 Who is this for?](#-who-is-this-for)
- [🚀 Quick start](#-quick-start)
- [📮 Calling the API from Postman or Insomnia](#-calling-the-api-from-postman-or-insomnia)
- [📋 What's mocked](#-whats-mocked)
- [💡 Common recipes](#-common-recipes)
- [🛠 Configuration](#-configuration)
- [🔒 HTTPS / TLS](#-https--tls)
- [🧪 Testing the mock](#-testing-the-mock)
- [🛡️ Verifying releases](#️-verifying-releases)
- [🏗 Architecture](#-architecture)
- [📂 Example consumer](#-example-consumer)
- [🐹 Go SDK](#-go-sdk)
- [🤝 Contributing](#-contributing)
- [📖 Documentation map](#-documentation-map)
- [⚖️ License](#️-license)
- [⚠️ Disclaimer](#️-disclaimer)

---

## ✨ What is this?

A self-contained Go service that looks and behaves like Auth0 to a calling client:

- 🎫 **Mints real RS256 JWTs** signed with an in-process key, and publishes the matching JWKS at `/.well-known/jwks.json`. Consumer SDKs validate signatures normally — no `InsecureSkipVerify`, no fake-token kludges.
- 📦 **Covers the whole Management API** by embedding a stripped skeleton of Auth0's OpenAPI 3.1 document (~400 operations: paths, methods, and schemas; Auth0's prose removed) and routing every endpoint through one generic handler. You stub responses by POSTing `{method, path, response}` to `/admin0/expectations`, and the OpenAPI schema validates the stubbed body at registration time. An optional `request` matcher lets you register multiple responses per operation; resolution is 4-tier (exact-path beats template-path; within a path, a request-matched expectation beats a catch-all; newest wins within a tier).
- 🛠 **Shapes runtime state over HTTP**: custom JWT claims, per-audience permissions, and the MFA-required flag are mutable mid-test through `/admin0/*` endpoints. No restart, no config-file juggling.
- 🐳 **Ships as a single static binary** (~13 MB) or a small Docker image. Sub-second boot, both HTTP (`:8080`) and HTTPS (`:8443`) by default.

<p align="right"><sub><a href="#-table-of-contents">↑ Back to table of contents</a></sub></p>

## 🎯 Who is this for?

Anyone whose service talks to Auth0 in tests or local dev:

- **CI pipelines** that need deterministic Auth0 responses without burning rate limit on a real tenant.
- **Local dev loops** where you don't want to share an Auth0 tenant or wait on its latency.
- **Integration test suites** for Auth0 SPA / native / API SDKs (auth0-react, auth0-js, auth0-spa-js, auth0-android, auth0-swift, auth0-react-native, etc.).
- **Resilience tests** for code paths that hit `/api/v2/users`, `/api/v2/clients`, `/api/v2/roles`, etc.
- **Service-to-service** flows using `client_credentials`, with realistic scopes and `permissions` claim shapes.

It is not for: production traffic, replacing your IdP, or anything that needs a real RBAC engine.

<p align="right"><sub><a href="#-table-of-contents">↑ Back to table of contents</a></sub></p>

## 🚀 Quick start

### From a release (recommended)

```bash
# latest stable
curl -fsSL https://raw.githubusercontent.com/sergiught/auth0-mock/main/install.sh | bash

# pinned version
curl -fsSL https://raw.githubusercontent.com/sergiught/auth0-mock/main/install.sh | bash -s v0.227.0

# install to a user-writable dir (no sudo)
BIN_DIR="$HOME/.local/bin" bash <(curl -fsSL https://raw.githubusercontent.com/sergiught/auth0-mock/main/install.sh)
```

The script downloads the goreleaser-built archive, verifies its sha256 against
the release's `checksums.txt`, and installs the binary as `auth0-mock`. Source
is at [`install.sh`](install.sh) — review before piping to bash if that bothers you.

### From source

```bash
make build && ./bin/auth0-mock
```

### Via `go install`

```bash
go install github.com/sergiught/auth0-mock/cmd/api@latest
$(go env GOPATH)/bin/api -version    # installs as `api`, not `auth0-mock`
```

`install.sh` and `make build` are still the recommended paths because they
stamp the binary with `version` / `commit` / `date` (visible via
`auth0-mock -version`) and install it as `auth0-mock`. `go install` is here
for Go developers who'd rather rebuild from source every time and don't
mind the cmd-package naming.

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

`docker compose` builds from the local dev `Dockerfile` (Go toolchain + source) for fast `--build` iteration. The release pipeline uses a separate slim `Dockerfile.release` (binary-only, fed by goreleaser) — `ghcr.io/sergiught/auth0-mock:vX.Y.Z` and `docker.io/sergiught/auth0-mock:vX.Y.Z` are what hits Docker Hub.

### Smoke test

```bash
# 1. Mint a real signed access token
TOKEN=$(curl -s -X POST http://localhost:8080/oauth/token \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'grant_type=client_credentials&client_id=demo&client_secret=x&audience=http://localhost:8080/api/v2/' \
  | jq -r .access_token)

# 2. Stub a Management API response
curl -X POST http://localhost:8080/admin0/expectations \
  -H 'Content-Type: application/json' \
  -d '{"method":"GET","path":"/api/v2/users/auth0|123","response":{"status":200,"body":{"user_id":"auth0|123","email":"alice@example.com"}}}'

# 3. Call the stubbed endpoint with your bearer
curl http://localhost:8080/api/v2/users/auth0%7C123 \
  -H "Authorization: Bearer ${TOKEN}"
# => {"user_id":"auth0|123","email":"alice@example.com"}
```

Your code calls auth0-mock the same way it calls Auth0. No SDK shims, no monkey-patching.

<p align="right"><sub><a href="#-table-of-contents">↑ Back to table of contents</a></sub></p>

## 📮 Calling the API from Postman or Insomnia

> [!TIP]
> **Prefer a browser?** Run the mock and open
> [http://localhost:8080/docs](http://localhost:8080/docs) for an interactive
> reference rendered by [Scalar](https://github.com/scalar/scalar). Every
> endpoint is clickable, and "Try it" points at the same instance that served
> the page.

The mock ships a merged OpenAPI 3.1 document that covers every HTTP surface
it exposes:

- The Auth0 **Authentication API** (`/oauth/token`, `/authorize`,
  `/userinfo`, `/v2/logout`, `/dbconnections/*`, `/passwordless/*`).
- The Auth0 **Management API** (everything under `/api/v2`). Canned responses
  are programmed centrally via `POST /admin0/expectations`.
- The **admin0** control plane (`/admin0/*`).
- The **service** endpoints (`/healthz`, `/.well-known/jwks.json`,
  `/openapi.json`, `/openapi.yaml`).

### Importing

- **From the repo:** import `api/auth0-mock.openapi.json` directly.
- **From a running instance:** point your client at
  `http://localhost:8080/openapi.json` (or `/openapi.yaml`).

Both Postman and Insomnia will create a folder per tag (`auth-api`,
`admin0`, `service`, plus the Management API's existing tags) and
fill in request bodies from the schemas.

### Regenerating the spec

The merged JSON is committed and checked for drift in CI. Re-run
`make openapi` after editing any of the auth0-mock-authored fragments:

- `internal/authapi/authapi.openapi.yaml`
- `internal/admin0/admin0.openapi.yaml`
- `internal/router/service.openapi.yaml`

`api/auth0-management-api.openapi.json` is a generated skeleton, not a
hand-edited file. To pull in a newer Auth0 Management API spec, run
`make refresh-spec` (see [CONTRIBUTING.md](CONTRIBUTING.md)).

<p align="right"><sub><a href="#-table-of-contents">↑ Back to table of contents</a></sub></p>

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

> [!IMPORTANT]
> **Audience is echoed, not enforced.** The mock mints tokens with whatever `audience` you ask for (falling back to `DEFAULT_AUDIENCE`) and the bearer middleware verifies signature + expiry + issuer but *not* that the audience matches anything client-side. This is deliberate — tests need to swap audiences freely. Real Auth0 does enforce audience against the client's registered APIs; if your downstream service relies on `aud` checks, you'll need to add your own assertion in test fixtures.

### 📦 Management API (spec-driven, ~400 endpoints)

Every operation in the embedded Auth0 Management API skeleton is mounted. Default response is `404 no_match`. Tests register stubs:

```bash
# Concrete-id stub
curl -X POST http://localhost:8080/admin0/expectations \
  -H 'Content-Type: application/json' \
  -d '{"method":"GET","path":"/api/v2/users/auth0|123","response":{"status":200,"body":{"user_id":"auth0|123","email":"alice@x"}}}'

# Template stub (catch-all for any user id)
curl -X POST http://localhost:8080/admin0/expectations \
  -H 'Content-Type: application/json' \
  -d '{"method":"GET","path":"/api/v2/users/{id}","response":{"status":200,"body":{"user_id":"auth0|*","email":"any@x"}}}'
```

> [!NOTE]
> Concrete-path stubs win over template stubs at request time. The optional `request` matcher (subset-matched `query` + `body`) lets you register multiple responses per operation; resolution is 4-tier (exact-path beats template-path; within a path, a request-matched expectation beats a catch-all; newest wins within a tier). `response.body` is validated against the operation's response schema at registration time. Invalid bodies are rejected with `400 invalid_match`, unknown operations with `400 unknown_operation`, unparseable or incomplete requests with `400 invalid_body`, and invalid `request` matcher fields (unknown fields, mistyped values, unknown query parameters) with `400 invalid_request_match`.

### 📡 Event streams

`GET /api/v2/events` is a real Server-Sent Events endpoint. Tests push events through `POST /admin0/events`; every connected subscriber sees them in real time. The mock keeps a bounded replay buffer (default 100 events, configurable via `EVENTS_REPLAY_BUFFER`) so reconnecting subscribers can resume via `Last-Event-ID`, `?from=<id>`, or `?from_timestamp=<rfc3339>` and the library's native replay path fills in what they missed.

```bash
# In one terminal: subscribe (bearer required).
TOKEN=$(curl -sX POST http://localhost:8080/oauth/token \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'grant_type=client_credentials&client_id=t&client_secret=t&audience=https://localhost:8443/api/v2/' \
  | jq -r .access_token)
curl -N -H "Authorization: Bearer $TOKEN" \
     'http://localhost:8080/api/v2/events?event_type=user.created'

# In another: push.
curl -X POST http://localhost:8080/admin0/events \
  -H 'Content-Type: application/json' \
  -d '{
    "type":"user.created","offset":"0",
    "event":{
      "specversion":"1.0","type":"user.created","source":"https://auth0.local/",
      "id":"evt_aaaaaaaaaaaaaaaa","time":"2026-05-19T00:00:00Z",
      "a0tenant":"my-tenant","a0stream":"est_aaaaaaaaaaaaaaaa",
      "data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}
    }
  }'
```

The subscriber receives `id: evt_aaaaaaaaaaaaaaaa / event: user.created / data: {...}`. Comment frames (`:keep-alive`) arrive every 15s so reverse-proxy idle timeouts don't drop the connection.

Errors are deliberately specific: schema violations → `400 invalid_event` with a one-line `"/json/pointer": reason` list; unknown `?from_timestamp` → `400 invalid_from_timestamp`; aged-out `Last-Event-ID` → `410 event_aged_out` (matches the `410` in Auth0's OpenAPI).

> [!NOTE]
> The mock's `WRITE_TIMEOUT` (default 30s) is automatically bypassed for `/api/v2/events` — long-lived subscribers won't be torn down by the server-side deadline. If a reverse proxy fronts the mock (nginx, Envoy, …), disable response buffering for this endpoint (`proxy_buffering off;` for nginx) so frames reach the client live rather than queuing in the proxy until the connection closes. The mock has no CORS support — browser `EventSource` clients on a different origin will be blocked by the browser's same-origin policy; run the mock on the page's origin or front it with a CORS-enabling proxy.

See [docs/COOKBOOK.md → Drive an event-stream consumer from a test](docs/COOKBOOK.md#drive-an-event-stream-consumer-from-a-test) for the SDK-driven workflow.

### 🛠 Admin surface (no auth, JSON-driven)

| Endpoint | Method | Purpose |
|---|---|---|
| `/admin0/reset` | POST | Wipe everything: expectations, claims, permissions, MFA flag, clock |
| `/admin0/expectations` | POST / GET / DELETE | Register, list, and clear canned Management API responses |
| `/admin0/claims` | GET / PUT / DELETE | Custom claims merged into every minted JWT |
| `/admin0/permissions` | GET / DELETE | All audiences and their permissions |
| `/admin0/permissions/{audience}` | GET / PUT / DELETE | Per-audience RBAC injection (audience may be a URL, chi wildcard) |
| `/admin0/mfa-required` | GET / PUT | Toggle MFA enforcement at runtime |
| `/admin0/clock` | GET / PUT / DELETE | Freeze the clock at an instant (`{"now":"..."}`), set an offset (`{"offset":"25h"}`), or restore real time. Drives JWT `iat`/`exp` and bearer validation. |
| `/admin0/clock/advance` | POST | Step the held clock by a Go duration (`{"by":"25h"}`). Negative durations allowed. |
| `/admin0/events` | POST | Push an Auth0 event-stream envelope onto `GET /api/v2/events` so consumer SDKs see it live. Body is the full envelope `{type, offset, event:{...CloudEvent}}` — validated against the OpenAPI `text/event-stream` schema before fan-out. See [Event streams](#-event-streams). |

> [!WARNING]
> **`/admin0/*` is unauthenticated by design** so test setup needs zero token plumbing. Never expose it to an untrusted network. Bind the mock to `127.0.0.1` (the default), keep it inside your CI runner / dev container, or front it with your own auth if you must reach it across a network boundary.

### 🩺 Operations

| Endpoint | Notes |
|---|---|
| `/healthz` | Kubernetes-style liveness probe — `200 {"status":"ok"}` if the process is up. No auth. |
| `/readyz`  | Kubernetes-style readiness probe — `200 {"status":"ready"}` once the JWKS signing key is materialised. The mock's only init dependency (RSA keygen) is synchronous and runs before the listener accepts, so today this is functionally equivalent to `/healthz`; the endpoint is exposed for orchestrator-convention parity (liveness vs readiness probe separation) and to leave room if future init grows. No auth. |

<p align="right"><sub><a href="#-table-of-contents">↑ Back to table of contents</a></sub></p>

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

# Freeze the clock so token iat/exp are deterministic
curl -X PUT http://localhost:8080/admin0/clock \
  -H 'Content-Type: application/json' \
  -d '{"now":"2030-01-01T00:00:00Z"}'

# Advance 7 days to simulate token expiry without sleeping
curl -X POST http://localhost:8080/admin0/clock/advance \
  -H 'Content-Type: application/json' \
  -d '{"by":"168h"}'

# Reset everything between tests
curl -X POST http://localhost:8080/admin0/reset
```

<p align="right"><sub><a href="#-table-of-contents">↑ Back to table of contents</a></sub></p>

## 🛠 Configuration

Environment variables (see [`.env.example`](.env.example) for the full template):

| Variable | Default | Notes |
|---|---|---|
| `HTTP_ADDR` | `127.0.0.1:8080` | The HTTP listener address. Set to `0.0.0.0:8080` to accept LAN/container traffic (the Dockerfile already does). To run HTTPS-only, set this to `off`. |
| `HTTPS_ADDR` | `127.0.0.1:8443` | The HTTPS listener address. Set to `0.0.0.0:8443` to accept LAN/container traffic (the Dockerfile already does). To run HTTP-only, set this to `off`. |
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
| `DEBUG` | `false` | When `true`, every request and response is logged in full at INFO level: method, path, query, headers (Authorization / Cookie redacted), and body (truncated at 8 KiB). Off by default — turn on only while debugging an SDK trace; adds an allocation and a synchronous log write per request. |
| `READ_HEADER_TIMEOUT` | `5s` | http.Server's `ReadHeaderTimeout` |
| `WRITE_TIMEOUT` | `30s` | http.Server's `WriteTimeout`. Bounds slow-write attacks. **Doesn't apply to `/api/v2/events`** — the SSE handler clears the deadline per-connection so long-lived subscribers aren't torn down. |
| `IDLE_TIMEOUT` | `120s` | http.Server's `IdleTimeout`. Bounds idle keep-alive connections. |
| `MAX_REQUEST_BODY_BYTES` | `1048576` (1 MiB) | Per-request body cap. Anything larger is read up to this point and the handler surfaces a 400. Set to `0` to disable. |
| `EVENTS_REPLAY_BUFFER` | `100` | Cap of the `/api/v2/events` SSE replay ring buffer. Reconnecting subscribers can resume from `Last-Event-ID`, `?from=<id>`, or `?from_timestamp=<rfc3339>` up to this many events back. `<= 0` disables replay (the endpoint still works; resume params become no-ops). |
| `SHUTDOWN_TIMEOUT` | `5s` | Graceful-shutdown grace period |
| `LOGOUT_ALLOWED_URLS` | _empty_ | Comma-separated allow-list of absolute `returnTo` URLs that `/v2/logout` will 302 to. Empty (default) = no enforcement so SDK tests calling `/v2/logout?returnTo=https://…` work out of the box. When set, mirrors Auth0's tenant "Allowed Logout URLs" setting: relative URLs are always allowed, unlisted absolutes get 400, dangerous schemes (`javascript:`, `data:`, …) and backslash bypasses are rejected. Set in production-like fixtures. |
| `AUTHORIZE_ALLOWED_CALLBACKS` | _empty_ | Comma-separated allow-list of absolute `redirect_uri` values that `/authorize` will 302 to. Same threat model as `LOGOUT_ALLOWED_URLS` but on the higher-value endpoint: `/authorize` carries `code` / `access_token` in the URL, so an unvalidated `redirect_uri` leaks them. Empty (default) = no enforcement so test SDKs can register any callback; set in production-like fixtures. Mirrors Auth0's per-application "Allowed Callback URLs" setting. |
| `BEARER_REQUIRE_AUDIENCE` | _empty_ | When set, the Mgmt-API bearer middleware rejects tokens whose `aud` claim doesn't contain this value (mirrors Auth0's tenant-API-audience binding). Empty keeps the "echoed, not enforced" default so tests can swap audiences freely. |

<p align="right"><sub><a href="#-table-of-contents">↑ Back to table of contents</a></sub></p>

## 🔒 HTTPS / TLS

The auto-generated cert covers `localhost`, `127.0.0.1`, `::1` (override with `TLS_HOSTNAMES`). TLS behaviour is the same on macOS and Linux, but the cert is self-signed, so clients reject it unless told otherwise. Three options:

> [!WARNING]
> **macOS gotcha:** Go on macOS pulls trust roots from the system Security framework and ignores `SSL_CERT_FILE` / `SSL_CERT_DIR` (Linux Go honors them). The Linux `SSL_CERT_FILE=./tls.crt go run …` trick simply doesn't work on macOS. On macOS, trust the cert via `mkcert` (option 1 below, easiest), or import it into the keychain (`security add-trusted-cert …`, recipe in [`docs/COOKBOOK.md`](docs/COOKBOOK.md#trusting-the-self-signed-cert)), or build a custom `tls.Config{RootCAs: pool}` in your client code.

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

<p align="right"><sub><a href="#-table-of-contents">↑ Back to table of contents</a></sub></p>

## 🧪 Testing the mock

```bash
go test -race ./...                        # unit tests
go test -tags=features ./cmd/api/...       # godog acceptance suite (every endpoint, end-to-end)
```

The godog harness boots the service in-process on a random port and exercises every Auth API path, every admin endpoint, and the spec-driven Management API surface. See [`features/`](features/) for the gherkin and [`features/scenario/`](features/scenario/) for the harness.

<p align="right"><sub><a href="#-table-of-contents">↑ Back to table of contents</a></sub></p>

## 🛡️ Verifying releases

Every tagged release ships with a Cosign signature on each Docker image and an SPDX-JSON SBOM per release archive. Both are produced by GitHub-hosted CI and uploaded as part of the same workflow that publishes the binaries.

**Verify a Docker image** (keyless signing — no shared secret required). Replace `vX.Y.Z` with the tag you want to verify, e.g. `v0.227.0`:

```bash
cosign verify \
  --certificate-identity-regexp 'https://github.com/sergiught/auth0-mock/\.github/workflows/release\.yml@.+' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/sergiught/auth0-mock:vX.Y.Z
```

> [!NOTE]
> Only the `ghcr.io/...` tag is cosign-signed by the release workflow. The `docker.io/sergiught/...` mirror is a publish-only convenience; verify the equivalent GHCR digest if you need attestation.

A successful verification proves the image was built by *this* repo's release workflow at *that* tag — not a CDN-substituted copy.

**Find an SBOM:** every release on the [GitHub Releases page](https://github.com/sergiught/auth0-mock/releases) carries an `auth0-mock_<version>_<os>_<arch>.tar.gz.spdx.json` alongside each archive. Pass it to your SBOM scanner of choice (Snyk, FOSSA, Black Duck, `grype`, etc.).

<p align="right"><sub><a href="#-table-of-contents">↑ Back to table of contents</a></sub></p>

## 🏗 Architecture

→ Full deep-dive: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

At-a-glance:

```
chi router
  ├── recovery + request_id + logging       (always-on middleware)
  ├── /healthz                               liveness
  ├── /openapi.json /openapi.yaml            merged OpenAPI 3.1 spec
  ├── /docs                                  Scalar-rendered API reference
  ├── /admin0/{reset, expectations, claims, permissions/*, mfa-required}
  │                                          control plane (no auth)
  ├── /.well-known/{jwks.json, openid-configuration}
  ├── /oauth/* /authorize /userinfo /v2/logout
  │   /dbconnections/* /passwordless/*
  │                                          Auth API (hand-coded, functional)
  └── /api/v2/*                              Management API (spec-driven; one generic handler)
        /api/v2/.../{verb}                   ← bearer-enforced; stubs via /admin0/expectations
```

Every handler is a struct holding its dependencies as fields, implementing `http.Handler` via `ServeHTTP`. JSON responses go through `go-chi/render`.

<p align="right"><sub><a href="#-table-of-contents">↑ Back to table of contents</a></sub></p>

## 📂 Example consumer

[`examples/consumer/`](examples/consumer/) is a stand-alone Go program that proves the drop-in compatibility end to end: mints a token, verifies its signature against `/.well-known/jwks.json` using the standard `MicahParks/keyfunc` + `golang-jwt/jwt` libraries (NOT the mock's internals), registers a Management API stub, and calls the stubbed endpoint.

```bash
make demo                # builds the mock, runs the example end-to-end over HTTPS, cleans up
```

Under the hood, `make demo` boots the binary with a persisted self-signed cert, waits for `/healthz`, runs `examples/consumer` against it, and tears the mock down on exit. To run by hand instead:

```bash
go run ./cmd/api &
go run ./examples/consumer
```

<p align="right"><sub><a href="#-table-of-contents">↑ Back to table of contents</a></sub></p>

## 🐹 Go SDK

[`pkg/auth0mock`](pkg/auth0mock) is the typed Go client for the `/admin0/*` control plane. Use it from Go test code to register stubs, inject claims, set per-audience permissions, and toggle MFA — without hand-marshalling JSON.

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
| `Expectations` | `List` | `Add`, fluent `ExpectGet/Post/Put/Patch/Delete(...).Respond(...).JSON(...).Apply(ctx)` | `Clear`, `ClearOp(method, path)` | `Verify(ctx)`; per-stub: `Hits(ctx)`, `Times(n)`, `AtLeast(n)`, `AtMost(n)`, `AnyTimes()` |
| `Claims` | `Get` | `Set` | `Clear` | — |
| `Permissions` | `All`, `Get(audience)` | `Set(audience, perms)` | `Clear`, `Delete(audience)` | — |
| `MFA` | `Get` | `Set` | (use `Set(ctx, false)`) | — |
| `Clock` | `Get` | `Freeze(ctx, t)`, `Offset(ctx, d)`, `Advance(ctx, d)` | `Reset` | — |
| top-level | — | — | `Reset` — wipes every store (including the clock back to real mode) | `auth0mocktest.Bracket(t, c)` — recommended one-liner: pre-test reset + post-test Reset + post-test Verify, all in correct LIFO order |

`Apply(ctx)` and `Expectations.Add(ctx, ...)` return a `*RegisteredExpectation` handle — chain `.Times(n)` / `.AtLeast(n)` / `.AtMost(n)` on it to set hit-count constraints, then `MustVerify` (or `Verify(ctx)` for the error-returning variant) checks every constraint at test end. Discard the handle with `_ = …Apply(ctx)` if you don't need it.

**Which helper?** Use `auth0mocktest.Bracket(t, c)` for every test that wants hit-count assertions — one line wires Reset on entry, Reset on exit, and Verify on exit in the correct LIFO order. Use `auth0mocktest.ResetOnCleanup(t, c)` when you only want isolation (no Times/AtLeast/AtMost constraints anywhere in the test).

**When a stub doesn't match,** the mock returns `{"errorCode":"no_match"}` with HTTP 404 to the SUT. From your test, the quickest debug move is `exps, _ := c.Expectations.List(ctx); fmt.Println(exps)` to see what's actually registered, and `reg.Hits(ctx)` on a specific handle to see if it fired.

What's NOT covered: the Auth0 APIs themselves (`/oauth/*`, `/api/v2/*`) — point your existing Auth0 SDK at the mock for those. The SDK only wraps the test-fixture-shaping surface.

A runnable end-to-end walk-through lives at [`examples/sdk/`](examples/sdk/) — its own Go module (with a local-path `replace` so the example doubles as a copy-and-pin template). The example drives stubs registered through this SDK with the real [`go-auth0`](https://github.com/auth0/go-auth0) SDK end-to-end (token mint → typed Management API call → Verify the stub was hit). Defaults to `https://localhost:8443` because go-auth0 only speaks TLS.

```bash
make demo-sdk                                      # full setup: mock + TLS cert + run + teardown
# or, against an already-running mock:
cd examples/sdk && go run . -cert=/path/to/tls.crt # full chain verification
cd examples/sdk && go run .                        # InsecureSkipVerify fallback (demo only)
```

Full godoc: [pkg.go.dev/github.com/sergiught/auth0-mock/pkg/auth0mock](https://pkg.go.dev/github.com/sergiught/auth0-mock/pkg/auth0mock).

> [!NOTE]
> The SDK's API is **unstable until v1.0.0**. Pin a tagged version (`go get github.com/sergiught/auth0-mock@v0.227.0` or later) and treat any minor bump as potentially breaking.

<p align="right"><sub><a href="#-table-of-contents">↑ Back to table of contents</a></sub></p>

## 🤝 Contributing

PRs welcome. See [`CONTRIBUTING.md`](CONTRIBUTING.md) for local setup, code style, testing requirements, and how to add a new endpoint.

<p align="right"><sub><a href="#-table-of-contents">↑ Back to table of contents</a></sub></p>

## 📖 Documentation map

| File | Audience | Purpose |
|---|---|---|
| [`README.md`](README.md) (this file) | Everyone | Overview, quick start, configuration |
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | Contributors / curious users | How the service is structured internally |
| [`docs/COOKBOOK.md`](docs/COOKBOOK.md) | Test authors | Recipes for common test scenarios |
| [`CONTRIBUTING.md`](CONTRIBUTING.md) | Contributors | Dev setup, conventions, PR workflow |
| [`SECURITY.md`](SECURITY.md) | Everyone | How to report a vulnerability |
| [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md) | Everyone | Community standards and enforcement |
| [`CHANGELOG.md`](CHANGELOG.md) | Everyone | What changed between versions |
| [`examples/consumer/README.md`](examples/consumer/README.md) | Test authors | Worked end-to-end example |

<p align="right"><sub><a href="#-table-of-contents">↑ Back to table of contents</a></sub></p>

## ⚖️ License

[MIT](LICENSE).

<p align="right"><sub><a href="#-table-of-contents">↑ Back to table of contents</a></sub></p>

## ⚠️ Disclaimer

auth0-mock is an independent, community-built testing tool. It is **not
affiliated with, endorsed by, or sponsored by Auth0 or Okta, Inc.** "Auth0" and
"Okta" are trademarks of Okta, Inc.; they are used here only nominatively, to
describe what this project mocks.

To route and validate every Management API endpoint, this repo embeds a
**stripped skeleton** of Auth0's published Management API OpenAPI specification
(sourced from <https://auth0.com/docs/api/management/openapi.json>): paths,
methods, parameters, and JSON-schema shapes only. Every Auth0-authored
`description`, `externalDocs` link, and `x-*` extension is removed before commit
by [`stripUpstreamProse`](cmd/genopenapi/main.go); see the
[refresh procedure](CONTRIBUTING.md#refreshing-the-auth0-management-api-spec).
The raw download is gitignored and never committed; only the skeleton is.

Auth0 does not attach an explicit redistribution license to the published spec.
The deliberate stripping above is what lets us redistribute the structural shape
for interoperability without redistributing Auth0's prose. If the distinction
matters for your compliance review, confirm the terms with Auth0/Okta directly.

<p align="right"><sub><a href="#-table-of-contents">↑ Back to table of contents</a></sub></p>
