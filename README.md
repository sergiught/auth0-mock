<div align="center">

<img src="docs/assets/banner.webp" alt="auth0-mock — a drop-in Auth0 HTTP API mock" width="100%">

# auth0-mock

A drop-in mock of Auth0's HTTP API (Authentication + Management) that you can point any Auth0-using service at, with no code changes.

Real RS256 JWTs. 400+ Management API endpoints. Runtime claim and permission injection. MFA, PKCE, OIDC discovery. HTTP and HTTPS.

[Quick start](#-quick-start) · [What's mocked](https://sergiu.dev/auth0-mock/reference/whats-mocked/) · [Recipes](https://sergiu.dev/auth0-mock/guides/stubbing/) · [Architecture](https://sergiu.dev/auth0-mock/reference/architecture/) · [Contributing](CONTRIBUTING.md)

</div>

[![CI](https://github.com/sergiught/auth0-mock/actions/workflows/ci.yml/badge.svg)](https://github.com/sergiught/auth0-mock/actions/workflows/ci.yml)
[![CodeQL](https://github.com/sergiught/auth0-mock/actions/workflows/codeql.yml/badge.svg)](https://github.com/sergiught/auth0-mock/actions/workflows/codeql.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/sergiught/auth0-mock)](https://goreportcard.com/report/github.com/sergiught/auth0-mock)
[![codecov](https://codecov.io/gh/sergiught/auth0-mock/branch/main/graph/badge.svg)](https://codecov.io/gh/sergiught/auth0-mock)
[![Go Reference](https://pkg.go.dev/badge/github.com/sergiught/auth0-mock.svg)](https://pkg.go.dev/github.com/sergiught/auth0-mock)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/sergiught/auth0-mock/badge)](https://securityscorecards.dev/viewer/?uri=github.com/sergiught/auth0-mock)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/12966/badge)](https://www.bestpractices.dev/projects/12966)
[![Signed releases](https://img.shields.io/badge/releases-cosign%20signed-0a7bbb)](https://sergiu.dev/auth0-mock/reference/verifying-releases/)
[![govulncheck](https://img.shields.io/badge/govulncheck-passing-2ca02c)](https://github.com/sergiught/auth0-mock/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/sergiught/auth0-mock?sort=semver)](https://github.com/sergiught/auth0-mock/releases)
[![Go version](https://img.shields.io/github/go-mod/go-version/sergiught/auth0-mock)](go.mod)
[![GHCR](https://img.shields.io/badge/ghcr.io-auth0--mock-2496ed?logo=docker&logoColor=white)](https://github.com/sergiught/auth0-mock/pkgs/container/auth0-mock)
[![Go module](https://img.shields.io/badge/go%20install-github.com%2Fsergiught%2Fauth0--mock-00ADD8?logo=go&logoColor=white)](https://pkg.go.dev/github.com/sergiught/auth0-mock/pkg/auth0mock)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Conventional Commits](https://img.shields.io/badge/Conventional%20Commits-1.0.0-fa6673.svg)](https://www.conventionalcommits.org)
[![PRs welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)

📖 **Docs:** https://sergiu.dev/auth0-mock/

## ✨ What is this?

A self-contained Go service that looks and behaves like Auth0 to a calling client:

- 🎫 **Mints real RS256 JWTs** signed with an in-process key, and publishes the matching JWKS at `/.well-known/jwks.json`. Consumer SDKs validate signatures normally — no `InsecureSkipVerify`, no fake-token kludges.
- 📦 **Covers the whole Management API** by embedding a stripped skeleton of Auth0's OpenAPI 3.1 document (~480 operations: paths, methods, and schemas; Auth0's prose removed) and routing every endpoint through one generic handler. You stub responses by POSTing `{method, path, response}` to `/admin0/expectations`, and the OpenAPI schema validates the stubbed body at registration time. An optional `request` matcher lets you register multiple responses per operation; resolution is 4-tier (exact-path beats template-path; within a path, a request-matched expectation beats a catch-all; newest wins within a tier).
- 🛠 **Shapes runtime state over HTTP**: custom JWT claims, per-audience permissions, and the MFA-required flag are mutable mid-test through `/admin0/*` endpoints. No restart, no config-file juggling.
- 🐳 **Ships as a single static binary** (~13 MB) or a small Docker image. Sub-second boot, both HTTP (`:8080`) and HTTPS (`:8443`) by default.

## 🎯 Who is this for?

Anyone whose service talks to Auth0 in tests or local dev:

- **CI pipelines** that need deterministic Auth0 responses without burning rate limit on a real tenant.
- **Local dev loops** where you don't want to share an Auth0 tenant or wait on its latency.
- **Integration test suites** for Auth0 SPA / native / API SDKs (auth0-react, auth0-js, auth0-spa-js, auth0-android, auth0-swift, auth0-react-native, etc.).
- **Resilience tests** for code paths that hit `/api/v2/users`, `/api/v2/clients`, `/api/v2/roles`, etc.
- **Service-to-service** flows using `client_credentials`, with realistic scopes and `permissions` claim shapes.

It is not for: production traffic, replacing your IdP, or anything that needs a real RBAC engine.

## 🚀 Quick start

```bash
docker run --rm -p 127.0.0.1:8080:8080 -e ADMIN_ALLOW_REMOTE=true ghcr.io/sergiught/auth0-mock:latest
```

Then, in another terminal:

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

## 📖 Documentation

Full docs live at <https://sergiu.dev/auth0-mock/>.

| Page | What it covers |
|---|---|
| [Installation](https://sergiu.dev/auth0-mock/getting-started/installation/) | Install and run auth0-mock from a release, from source, with go install, in a live-reload loop or in Docker. |
| [First request](https://sergiu.dev/auth0-mock/getting-started/first-request/) | Mint a token, stub a Management API response and call it, using only curl. |
| [curl quickstart](https://sergiu.dev/auth0-mock/quickstarts/http/) | Call the mock with plain curl, from starting it to calling a stubbed endpoint. |
| [Node.js quickstart](https://sergiu.dev/auth0-mock/quickstarts/node/) | Test a Node API and a Management API client against the mock with express-oauth2-jwt-bearer and the auth0 SDK. |
| [Python quickstart](https://sergiu.dev/auth0-mock/quickstarts/python/) | Test a Python API and a Management API client against the mock with PyJWT and the auth0-python SDK. |
| [Stubbing the Management API](https://sergiu.dev/auth0-mock/guides/stubbing/) | Stub Management API responses, errors and retries, and reset or inspect what is registered. |
| [Tokens and claims](https://sergiu.dev/auth0-mock/guides/tokens-and-claims/) | Test code that reads permissions or custom claims, and test token expiry without sleeping. |
| [Login flows](https://sergiu.dev/auth0-mock/guides/login-flows/) | Test PKCE, login errors, silent login, MFA challenges and the password-realm grant. |
| [Event streams](https://sergiu.dev/auth0-mock/guides/event-streams/) | Drive an event-stream consumer from a test. |
| [HTTPS / TLS](https://sergiu.dev/auth0-mock/guides/https-tls/) | Run auth0-mock over HTTPS and make clients trust its self-signed certificate. |
| [Calling the API from Postman or Insomnia](https://sergiu.dev/auth0-mock/guides/postman/) | Import the merged OpenAPI document into Postman or Insomnia, and regenerate it after spec changes. |
| [Browser apps](https://sergiu.dev/auth0-mock/guides/browser-apps/) | Call the mock from a browser app. |
| [Go SDK](https://sergiu.dev/auth0-mock/guides/go-sdk/) | Use the typed Go client and test helpers to drive auth0-mock from Go tests. |
| [Common recipes](https://sergiu.dev/auth0-mock/guides/common-recipes/) | Short curl recipes for claims, permissions, MFA and the clock. |
| [What's mocked](https://sergiu.dev/auth0-mock/reference/whats-mocked/) | What the mock implements: the Authentication API, supported OAuth grants, the Management API, event streams, the admin surface and operations endpoints. |
| [Configuration](https://sergiu.dev/auth0-mock/reference/configuration/) | Every environment variable and flag that configures auth0-mock. |
| [Verifying releases](https://sergiu.dev/auth0-mock/reference/verifying-releases/) | Verify the Cosign signature on Docker images and find the SBOM for each release. |
| [Architecture](https://sergiu.dev/auth0-mock/reference/architecture/) | A walkthrough of how auth0-mock is put together, for contributors and curious users. |

## 🤝 Contributing

PRs welcome. See [`CONTRIBUTING.md`](CONTRIBUTING.md) for local setup, code style, testing requirements, and how to add a new endpoint. Also see [`SECURITY.md`](SECURITY.md), [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md) and [`CHANGELOG.md`](CHANGELOG.md).

## ⚖️ License

[MIT](LICENSE).

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
