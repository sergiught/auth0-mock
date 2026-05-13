# Changelog

All notable changes to this project will be documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versioning follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **MFA challenge flow** — initial `password` / `password-realm` grants return `403 mfa_required` + `mfa_token` when MFA enforcement is on. Three new Auth0 grants accept canned factors: `mfa-otp` (`otp=123456`), `mfa-oob` (`oob_code=any` + `binding_code=123456`), `mfa-recovery-code` (`recovery_code=ABCDEFGHIJKLMNOP`). `GET/PUT /admin0/mfa-required` toggles enforcement at runtime. `POST /admin0/reset` now clears MFA state.
- **`password-realm` grant** — Auth0-specific `http://auth0.com/oauth/grant-type/password-realm`; same shape as `password` with an extra `realm` field threaded into the issued token's claims (`connection`, `https://auth0.com/realm`, `gty=password-realm`). Used by Auth0 Native SDKs.
- **PKCE validation on `authorization_code` grant** — `/authorize` stashes any `code_challenge` (S256 or plain) keyed by the issued code; `/oauth/token` verifies the `code_verifier` on exchange. 10-min TTL, single-use. Codes that never came through `/authorize` still mint (backward compat).
- **`/admin0/claims`** (GET / PUT / DELETE) — per-process custom JWT claim map merged into every minted token. Custom claims overwrite reserved keys by design.
- **`/admin0/permissions[/{audience}]`** (GET / PUT / DELETE) — per-audience RBAC injection. Audiences may be URLs (chi wildcard route).
- **`/healthz`** — Kubernetes-style liveness probe.
- **`TLS_CACHE_DIR` env** — persist auto-generated TLS cert across restarts. Trust-store imports now survive container restarts.
- **README, ARCHITECTURE, COOKBOOK, COMPARISON, CONTRIBUTING, CHANGELOG** docs.
- **examples/consumer** — stand-alone Go consumer demonstrating end-to-end drop-in compatibility.

### Changed

- **`POST /admin0/reset`** now clears matches + claims + permissions + MFA state in one shot.
- Migrated router from `julienschmidt/httprouter` to `go-chi/chi v5`; every handler is now a struct holding its dependencies as fields, implementing `ServeHTTP`.
- JSON responses go through `go-chi/render` everywhere.
- Bumped Go directive to 1.26.

## Earlier work (pre-release, unversioned)

Foundations landed in a series of milestones before the first tagged release:

- **M0 — Foundation:** project scaffolding, `internal/{config,logger,httperr,middleware,matches,admin0,server,router}`, Dockerfile, docker-compose, `.env.example`.
- **M1 — TLS + JWKS + bearer:** `internal/{tlscert,jwks,bearer}`; HTTPS listener; `/.well-known/jwks.json`.
- **M2 — Spec-driven Mgmt API:** embedded Auth0 OpenAPI 3.1 spec, `internal/spec`, `internal/mgmtapi` with one generic handler covering ~400 operations; OpenAPI-validated registered stubs.
- **M3 — Auth API core:** `internal/authapi` with `/oauth/token` (`client_credentials`, `password`, `refresh_token`, `authorization_code`), `/authorize`, `/userinfo`, `/v2/logout`, `/oauth/revoke`, `/.well-known/openid-configuration`.
- **M4 — Auth API extensions:** `/dbconnections/{signup,change_password}`, `/passwordless/{start,verify}`.
- **M5 — Acceptance tests + docs:** godog harness, initial 33 scenarios; examples/consumer; production Dockerfile.
