# auth0-mock vs the alternatives

A pragmatic comparison against the other tools you might reach for when you need to mock Auth0 (or any OIDC provider). For the long-form research notes that backed this summary, see the per-tool reports in `docs/design/` on your workstation; this page is the public-facing distillation.

## TL;DR

| Tool | Auth0-aware? | Mgmt API? | Runtime claim/RBAC shaping? | Stack | License |
|---|---|---|---|---|---|
| **auth0-mock** (this) | ✅ Yes | ✅ ~400 ops, spec-driven | ✅ via `/admin0/*` | Go + chi | MIT |
| [primait/localauth0](https://github.com/primait/localauth0) | ✅ Yes (Auth API only) | ❌ | ✅ via dedicated endpoints | Rust + actix-web | MIT |
| [navikt/mock-oauth2-server](https://github.com/navikt/mock-oauth2-server) | ❌ | ❌ | ✅ via library API | Kotlin / Spring | MIT |
| [axa-group/oauth2-mock-server](https://github.com/axa-group/oauth2-mock-server) | ❌ | ❌ | ✅ via EventEmitter | Node.js | MIT |
| [panva/node-oidc-provider](https://github.com/panva/node-oidc-provider) | ❌ | ❌ | ⚠️ config-closure only | Node.js / Koa | MIT |
| [Keycloak](https://www.keycloak.org/) (real OIDC server, often via Testcontainers) | ❌ | ❌ (own admin API, different shape) | ✅ realms + UI + REST | Java / Quarkus | Apache-2.0 |
| [WireMock](https://wiremock.org/) (generic HTTP mock + JWT extension) | ❌ | ❌ (hand-author every endpoint) | ⚠️ via Handlebars helpers | Java | Apache-2.0 |

**The one thing only auth0-mock has: spec-driven mocking of Auth0's `/api/v2/*` Management API.** Every other tool covers the OIDC half only (or in WireMock's case, nothing without hand-authored stubs).

## How they actually differ

### localauth0

The project that inspired this one. Rust/actix-web container, narrow but well-shaped: it mints functional JWTs, publishes JWKS, supports `client_credentials` and `authorization_code`, and shines at **runtime mutation of token contents** via dedicated endpoints (`/permissions`, `/oauth/token/custom_claims`, `/oauth/token/user_info`). Configured via a TOML file or `LOCALAUTH0_CONFIG` env var. Has 44 releases.

**Pick localauth0 over auth0-mock when:** you only need Auth0's Authentication API and you specifically want the TOML-config workflow. The two grants it covers (client_credentials + authorization_code) are the common case.

**Pick auth0-mock over localauth0 when:** you need any `/api/v2/*` Management API path, the other two Auth0 grants (`password`, `refresh_token`), OIDC discovery (`/.well-known/openid-configuration`), Auth0's `/v2/logout` + `/oauth/revoke`, the password-realm grant, or the MFA challenge dance. localauth0 has none of these.

### navikt/mock-oauth2-server

Kotlin/Spring Boot OIDC test mock, published to Maven Central as both a library (embed inside a JUnit test, sub-second startup, no Docker) and a Docker image. Has more standard OIDC grant coverage than us in one area: it supports **JWT-Bearer** (RFC 7523) and **Token Exchange** (RFC 8693) — neither of which Auth0 itself exposes.

**Pick mock-oauth2-server over auth0-mock when:** you're in a JVM-only codebase doing pure OIDC tests and the Maven embed pattern is non-negotiable. The library-mode startup is a real ergonomic win for JUnit suites.

**Pick auth0-mock over mock-oauth2-server when:** any of your code talks to Auth0-specific paths (`/api/v2/*`, `/dbconnections/*`, `/passwordless/*`, `/v2/logout`, the Auth0-flavoured `password-realm` or `mfa-*` grants).

### axa-group/oauth2-mock-server

Node.js npm library — wire it into a Node test process and configure JWT-claim mutation via an EventEmitter. Strong on algorithm breadth (RS\*/PS\*/ES256/384/512/EdDSA) and full RFC 7636 PKCE.

**Pick oauth2-mock-server over auth0-mock when:** you're in a Node-only codebase and want the in-process library pattern; you specifically need non-RS256 signing algorithms (which Auth0 itself doesn't support, but your test scenario might).

**Pick auth0-mock over oauth2-mock-server when:** Auth0 specifics matter, you need an HTTP admin surface (so non-Node test runners can also poke at it), or you want the runtime store to be inspectable via HTTP rather than only in-process.

### panva/node-oidc-provider

A *production-grade*, OpenID-Certified Node.js OIDC implementation that people often press into service as a mock. Spec-compliant (Basic, Hybrid, FAPI 1.0, FAPI 2.0 profiles certified), highly configurable, but config is **closure-only**: no HTTP admin endpoints to vary state mid-test.

**Pick node-oidc-provider over auth0-mock when:** you're testing OIDC *protocol compliance* (FAPI conformance, edge-case spec handling) and you need real production-shaped behaviour rather than a mock. Or you want certification stamps on your test report.

**Pick auth0-mock over node-oidc-provider when:** you need Auth0-specific paths or grants, an HTTP admin surface for runtime mutation, or anything that touches `/api/v2/*`.

### Keycloak (often via Testcontainers)

A real production IAM platform. Heavy (450–600 MB image, 10–15 s boot with cached image, 750 MB+ RAM), but a real OIDC server with real realms, real users, MFA, federation, social login, an admin UI for visual inspection. The Testcontainers Keycloak module is the canonical "use a real OIDC server in tests" pattern.

**Pick Keycloak over auth0-mock when:** you specifically want to integration-test against a *real* OIDC provider (e.g. to validate that your client correctly handles real edge cases), you want realm + user state to persist across container restarts, or you need MFA enrollment / social federation / SAML.

**Pick auth0-mock over Keycloak when:** anything Auth0-specific (and almost everything is), you want sub-second boot for CI parallelism, you want to assert behaviour rather than configure a real IdP, or you want lightweight HTTP-driven stubbing (`/match`) instead of realm + client + user setup. Keycloak's admin API is **architecturally incompatible** with Auth0's — same concepts, different paths/shapes, so it can't be a drop-in for Auth0 Mgmt API consumers.

### WireMock

A generic HTTP mock with optional JWT extension. Can sign tokens at request time using Handlebars helpers (`{{jwt}}`, `{{jwks}}`), publish JWKS, and reply with whatever you author. But: **none** of the OIDC dance is built in. Every Auth0 endpoint requires a hand-authored stub-mapping JSON file.

**Pick WireMock over auth0-mock when:** you need its strengths — request matching DSL, latency / fault injection, record-and-replay against a real upstream, generic stub mappings for **non-Auth0** HTTP dependencies in the same test. WireMock is a different category of tool: a generic mock framework, not an Auth0 mock.

**Pick auth0-mock over WireMock when:** you want Auth0-shaped behaviour out of the box without authoring tens of stub mappings for each endpoint and orchestrating the OIDC dance yourself.

## Where auth0-mock falls short (honest)

We don't pretend to be everything. Things we deliberately skip:

- **OpenID-Certified protocol compliance.** We approximate; we're not FAPI-certified.
- **`/oauth/introspect` (RFC 7662).** Auth0 itself doesn't have this — we follow Auth0's stance of "validate JWTs locally via JWKS".
- **JWT-Bearer (RFC 7523) and Token Exchange (RFC 8693) grants.** Not in Auth0's documented grant list, so not in ours.
- **Non-RS256 signing algorithms.** Auth0 supports RS256 (default) and HS256 (legacy); we only do RS256 to keep the surface small. PR welcome if someone needs HS256.
- **Stateful Mgmt API CRUD.** The mock is a stub registrar, not a state machine. If you POST `/api/v2/users` we don't remember the user; you have to register what the subsequent GET should return. This is a feature.
- **A Go library embedding mode.** Today the mock runs as a separate process. A public Go API is on the roadmap but not stable yet.

## Capability matrix

A condensed feature-by-feature view. ✓ = first-class support, ⚠️ = partial / requires work, ✗ = not supported.

| Capability | auth0-mock | localauth0 | navikt | axa | panva | Keycloak | WireMock |
|---|---|---|---|---|---|---|---|
| `POST /oauth/token` (client_credentials) | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ⚠️ stub |
| `POST /oauth/token` (password) | ✓ | ✗ | ✓ | ⚠️ | ✓ | ✓ | ⚠️ stub |
| `POST /oauth/token` (refresh_token) | ✓ | ✗ | ✓ | ✓ | ✓ | ✓ | ⚠️ stub |
| `POST /oauth/token` (authorization_code) | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ⚠️ stub |
| `POST /oauth/token` (password-realm) | ✓ | ✗ | ✗ | ✗ | ✗ | ✗ | ⚠️ stub |
| `POST /oauth/token` (passwordless/otp) | ✓ | ✗ | ✗ | ✗ | ✗ | ⚠️ | ⚠️ stub |
| `POST /oauth/token` (mfa-otp / mfa-oob / mfa-recovery-code) | ✓ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ |
| PKCE on authorization_code | ✓ | ⚠️ | ✓ | ✓ | ✓ | ✓ | ✗ |
| `GET /.well-known/openid-configuration` | ✓ | ✗ | ✓ | ✓ | ✓ | ✓ | ⚠️ stub |
| `GET /.well-known/jwks.json` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ⚠️ extension |
| `GET /userinfo` (spec path) | ✓ | ⚠️ different path | ✓ | ✓ | ✓ | ✓ | ⚠️ stub |
| `POST /oauth/revoke` | ✓ | ⚠️ different semantics | ✓ | ✓ | ✓ | ✓ | ⚠️ stub |
| `GET /v2/logout` | ✓ | ✗ | ✓ | ✗ | ✓ | ✓ | ⚠️ stub |
| `POST /dbconnections/{signup,change_password}` | ✓ | ✗ | ✗ | ✗ | ✗ | ✗ | ⚠️ stub |
| `POST /passwordless/{start,verify}` | ✓ | ✗ | ✗ | ✗ | ✗ | ⚠️ | ⚠️ stub |
| Auth0 Management API (`/api/v2/*`) | ✓ ~400 ops | ✗ | ✗ | ✗ | ✗ | ⚠️ different API | ⚠️ stub |
| OpenAPI schema validation on registered stubs | ✓ | ✗ | n/a | n/a | n/a | n/a | ✗ |
| Runtime custom JWT claim injection via HTTP | ✓ | ✓ | ⚠️ library | ⚠️ EventEmitter | ⚠️ closure | ✓ via Admin REST | ⚠️ Handlebars |
| Per-audience permission injection | ✓ | ✓ | ⚠️ library | ✗ | ⚠️ closure | ✓ | ✗ |
| MFA challenge enforcement toggle | ✓ HTTP flag | ✗ | ✗ | ✗ | ⚠️ closure | ✓ realm policy | ✗ |
| Drop-in for Auth0 SDKs (no code change) | ✓ | ⚠️ Auth API only | ✗ | ✗ | ✗ | ✗ | ✗ |
| Container image (sub-100 MB) | ✓ ~13 MB | ✓ | ⚠️ JVM | ✓ Node | ✓ Node | ✗ 450+ MB | ✗ |
| Library embed for in-process tests | ✗ (planned) | ✗ | ✓ Maven | ✓ npm | ✓ npm | ✗ | ⚠️ JVM |
| Healthcheck endpoint | ✓ `/healthz` | ✓ `/healthcheck` | ⚠️ | ⚠️ | ⚠️ | ✓ | ⚠️ |

## When you really shouldn't use auth0-mock

- You're not testing — you're running production. Use real Auth0 (or actual Keycloak).
- Your test exercises a real OIDC certification edge case (FAPI conformance, etc.). Use node-oidc-provider.
- You need to test against a real IdP with real users, federation, MFA enrollment. Use Keycloak via Testcontainers, or actual Auth0.
- Your service speaks to many HTTP dependencies, not just Auth0, and you want one tool to mock them all. Use WireMock as the orchestrator and auth0-mock alongside it for the Auth0-shaped calls.

## See also

- [README.md](../README.md) — overview
- [docs/ARCHITECTURE.md](ARCHITECTURE.md) — how auth0-mock is built
- [docs/COOKBOOK.md](COOKBOOK.md) — practical recipes
