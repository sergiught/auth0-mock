# Changelog

All notable changes to this project will be documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versioning follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Request-matcher validation at registration**: `request.body` is validated against the target operation's request schema with `required` relaxed (a matcher is partial by design), so unknown and mistyped fields are rejected up front (`invalid_request_match`); `request.query` keys are checked against the operation's declared query parameters.
- **MFA challenge flow**: initial `password` / `password-realm` grants return `403 mfa_required` + `mfa_token` when MFA enforcement is on. Three new Auth0 grants accept canned factors: `mfa-otp` (`otp=123456`), `mfa-oob` (`oob_code=any` + `binding_code=123456`), `mfa-recovery-code` (`recovery_code=ABCDEFGHIJKLMNOP`). `GET/PUT /admin0/mfa-required` toggles enforcement at runtime. `POST /admin0/reset` now clears MFA state.
- **`password-realm` grant**: Auth0-specific `http://auth0.com/oauth/grant-type/password-realm`; same shape as `password` with an extra `realm` field threaded into the issued token's claims (`connection`, `https://auth0.com/realm`, `gty=password-realm`). Used by Auth0 Native SDKs.
- **PKCE validation on `authorization_code` grant**: `/authorize` stashes any `code_challenge` (S256 or plain) keyed by the issued code; `/oauth/token` verifies the `code_verifier` on exchange. 10-min TTL, single-use. Codes that never came through `/authorize` still mint (backward compat).
- **`/admin0/claims`** (GET / PUT / DELETE): per-process custom JWT claim map merged into every minted token. Custom claims overwrite reserved keys by design.
- **`/admin0/permissions[/{audience}]`** (GET / PUT / DELETE): per-audience RBAC injection. Audiences may be URLs (chi wildcard route).
- **`/healthz`**: Kubernetes-style liveness probe.
- **`TLS_CACHE_DIR` env**: persist auto-generated TLS cert across restarts. Trust-store imports now survive container restarts.
- **README, ARCHITECTURE, COOKBOOK, COMPARISON, CONTRIBUTING, CHANGELOG** docs.
- **examples/consumer**: stand-alone Go consumer demonstrating end-to-end drop-in compatibility.
- **OpenAPI export**: a merged OpenAPI 3.1 document covering every HTTP surface — Auth API, Management API (canned Management API responses are registered centrally via POST/GET/DELETE `/admin0/expectations`), `admin0`, and service endpoints — served at `GET /openapi.json` and `GET /openapi.yaml`, with a Scalar-rendered reference at `GET /docs` (preloaded bearer for "Try it", OS-themed, navigable `x-tagGroups` sidebar). The `cmd/genopenapi` bundler stitches it from the Auth0 skeleton + per-surface fragments; `make openapi` regenerates `api/auth0-mock.openapi.json` and CI drift-checks it.
- **Non-affiliation disclaimer** in the README: auth0-mock is an independent tool, not affiliated with or endorsed by Auth0 / Okta.

### Changed

- **`/admin0/expectations` payload is now nested** — `{method, path, request?, response}`. An optional `request` matcher (subset-matched `query` + `body`) conditions an expectation on the incoming request, so multiple expectations can be registered per operation; the most specific match wins, ties broken by newest-registered. `DELETE {method, path}` clears the whole operation's list. The `matches` store changed from one entry per `(method, path)` to an ordered list per key.
- **`POST /admin0/reset`** now clears expectations + claims + permissions + MFA state in one shot.
- **Auth0 Management API spec is now a stripped skeleton**, not Auth0's verbatim document. `stripUpstreamProse` removes every Auth0-authored description, `externalDocs` link, and `x-*` extension (~1000 `x-description-*` fields, 97 doc links), leaving only the paths/methods/parameters/schemas the mock needs to route and validate. `make refresh-spec` re-vendors the skeleton from a manually-downloaded raw spec (gitignored, never committed); see CONTRIBUTING.md.
- **HTTP/HTTPS listeners default to `127.0.0.1`** (was `0.0.0.0`), so a bare-metal run is not reachable off-host without an explicit `HTTP_ADDR` / `HTTPS_ADDR` opt-in. The Docker image sets them back to `0.0.0.0` so `docker run -p` and compose still work.
- Migrated router from `julienschmidt/httprouter` to `go-chi/chi v5`; every handler is now a struct holding its dependencies as fields, implementing `ServeHTTP`.
- JSON responses go through `go-chi/render` everywhere.
- Bumped Go directive to 1.26.
- Consolidated to a **single root-level `Dockerfile`** (was: separate `infrastructure/dockerfiles/{development,production}/Dockerfile`). Same production-grade image is used for local `docker compose up` and Docker Hub publishing. `docker-compose.yaml` no longer mounts source, code changes require a `docker compose up --build`. Image now uses `tini` as PID 1 for clean SIGTERM, runs as `nobody`, and exposes a built-in healthcheck via `/healthz`.

### Dev experience

- **`make watch`**: sub-second hot-reload via [`air`](https://github.com/air-verse/air). Installs air into `./bin` on first run; watches `cmd/`, `internal/`, `api/`; rebuilds + restarts the binary on every save. `.air.toml` lives at the repo root. Native filesystem events, no docker, no bind-mount.
- **`make test-features`**: run the godog acceptance suite (was: `go test -tags=features -count=1 ./cmd/api/...`).
- **`make lint`**: runs `golangci-lint` v2.5.0 with the project's `.golangci.yaml` (errcheck, gocritic, gocyclo, godot, gosec, revive, staticcheck, unconvert, unused, whitespace). Auto-installed into `./bin` on first invocation.
- **`make lint-commits`**: runs `commitlint` v0.10.1 against the conventional-commit `commitlint.yaml` profile.
- **`make vuln`**: runs `govulncheck` against the module graph to surface known Go vulnerabilities.
- **`make pre-commit`**: installs the `pre-commit` framework hooks (`.pre-commit-config.yaml`) so commitlint / gofmt / golangci-lint / govulncheck run automatically on every commit.
- **GitHub Actions CI** (`.github/workflows/ci.yml`): five parallel jobs (`lint`, `test`, `test-features`, `vuln`, `commitlint` (PR-only)). Go 1.26.

## Earlier work (pre-release, unversioned)

Foundations landed in a series of milestones before the first tagged release:

- **M0 Foundation:** project scaffolding, `internal/{config,logger,httperr,middleware,matches,admin0,server,router}`, Dockerfile, docker-compose, `.env.example`.
- **M1 TLS + JWKS + bearer:** `internal/{tlscert,jwks,bearer}`; HTTPS listener; `/.well-known/jwks.json`.
- **M2 Spec-driven Management API:** embedded Auth0 OpenAPI 3.1 spec, `internal/spec`, `internal/mgmtapi` with one generic handler covering ~400 operations; OpenAPI-validated registered stubs.
- **M3 Auth API core:** `internal/authapi` with `/oauth/token` (`client_credentials`, `password`, `refresh_token`, `authorization_code`), `/authorize`, `/userinfo`, `/v2/logout`, `/oauth/revoke`, `/.well-known/openid-configuration`.
- **M4 Auth API extensions:** `/dbconnections/{signup,change_password}`, `/passwordless/{start,verify}`.
- **M5 Acceptance tests + docs:** godog harness, initial 33 scenarios; examples/consumer; production Dockerfile.
