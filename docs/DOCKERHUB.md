# auth0-mock

A drop-in mock of [Auth0](https://auth0.com)'s Authentication and Management
APIs for tests, local development, and CI. Same HTTP shapes and response
formats: point your SDK at the mock instead of `*.auth0.com` and your code
keeps running unchanged.

## What's in the box

- **Real RS256 JWTs.** The mock generates a fresh keypair on boot and signs
  every token with it. Downstream services validate signatures against
  `/.well-known/jwks.json` with stock OSS libraries.
- **400+ Management API endpoints** mounted from Auth0's published OpenAPI
  spec. Stubbed responses are registered per `(method, path)` and validated
  against the spec schema at registration time.
- **OIDC discovery** at `/.well-known/openid-configuration` for clients that
  bootstrap from a single base URL.
- **Auth flows:** client credentials, authorization code, PKCE, password
  realm, passwordless, and the full MFA challenge dance.
- **Runtime claim & permission injection** so a single mock can serve many
  audiences with different scopes.
- **Single static binary** (~13 MB) with both HTTP (`:8080`) and HTTPS
  (`:8443`) listeners. Sub-second boot.

## Quick start

```bash
docker run --rm -p 8080:8080 -p 8443:8443 sergiught/auth0-mock:latest
```

Mint a token, register a stubbed Management API response, and call it:

```bash
TOKEN=$(curl -s -X POST http://localhost:8080/oauth/token \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'grant_type=client_credentials&client_id=demo&client_secret=x&audience=http://localhost:8080/api/v2/' \
  | jq -r .access_token)

curl -X POST http://localhost:8080/admin0/expectations \
  -H 'Content-Type: application/json' \
  -d '{"method":"GET","path":"/api/v2/users/auth0|123","response":{"status":200,"body":{"user_id":"auth0|123","email":"alice@example.com"}}}'

curl -H "Authorization: Bearer ${TOKEN}" \
  'http://localhost:8080/api/v2/users/auth0%7C123'
# => {"user_id":"auth0|123","email":"alice@example.com"}
```

## Tags

| Tag             | Points to                              |
|-----------------|----------------------------------------|
| `latest`        | latest stable release                  |
| `vX.Y.Z`        | a specific release (immutable)         |

All tags are multi-arch manifests covering `linux/amd64` and `linux/arm64`.

### Provenance

Every `ghcr.io/sergiught/auth0-mock:<tag>` image is signed with [Cosign](https://github.com/sigstore/cosign) keylessly from the GitHub Actions release workflow (no shared secret). The Docker Hub mirror (`sergiught/auth0-mock:<tag>`) is a publish-only convenience: pull the equivalent GHCR digest to verify provenance. Replace `<tag>` below with the version you want, e.g. `v0.1.0`:

```bash
cosign verify \
  --certificate-identity-regexp 'https://github.com/sergiught/auth0-mock/\.github/workflows/release\.yml@.+' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/sergiught/auth0-mock:<tag>
```

SPDX-JSON SBOMs for each release archive live alongside the binaries on the [GitHub Releases page](https://github.com/sergiught/auth0-mock/releases).

## Configuration

The container is configured via environment variables. The most common ones:

| Variable        | Default                                | Purpose                              |
|-----------------|----------------------------------------|--------------------------------------|
| `HTTP_ADDR`     | `0.0.0.0:8080`                         | HTTP listener; empty to disable      |
| `HTTPS_ADDR`    | `0.0.0.0:8443`                         | HTTPS listener; empty to disable     |
| `TLS_CERT_FILE` / `TLS_KEY_FILE` | _empty_               | If both set → load; else auto-gen    |
| `TLS_CACHE_DIR` | _empty_                                | Persist auto-gen cert across restarts |
| `ISSUER_URL`    | `https://localhost:8443/`              | `iss` claim and OIDC discovery base  |
| `DEFAULT_AUDIENCE` | `https://localhost:8443/api/v2/`    | Default `aud` if request omits one   |
| `LOG_LEVEL`     | `info`                                 | `debug` / `info` / `warn` / `error`  |
| `WRITE_TIMEOUT` | `30s`                                  | `http.Server.WriteTimeout` — slow-write defence |
| `IDLE_TIMEOUT`  | `120s`                                 | `http.Server.IdleTimeout` — keep-alive cap      |
| `MAX_REQUEST_BODY_BYTES` | `1048576` (1 MiB)             | Per-request body cap; oversize requests get a 400 |

The full list lives in the [repository README](https://github.com/sergiught/auth0-mock#configuration).

## Disclaimer

auth0-mock is an independent, community-built testing tool. It is **not
affiliated with, endorsed by, or sponsored by Auth0 or Okta, Inc.** "Auth0"
and "Okta" are trademarks of Okta, Inc.; they are used here only nominatively,
to describe what this project mocks.

## More

- **Source & issues:** <https://github.com/sergiught/auth0-mock>
- **Architecture deep-dive:** <https://github.com/sergiught/auth0-mock/blob/main/docs/ARCHITECTURE.md>
- **Recipes (claim injection, RBAC, MFA, TLS trusting, SDK integration):** <https://github.com/sergiught/auth0-mock/blob/main/docs/COOKBOOK.md>
- **Working SDK example:** <https://github.com/sergiught/auth0-mock/tree/main/examples/consumer>
- **License:** MIT (see [`LICENSE`](https://github.com/sergiught/auth0-mock/blob/main/LICENSE) and [`NOTICE`](https://github.com/sergiught/auth0-mock/blob/main/NOTICE))
