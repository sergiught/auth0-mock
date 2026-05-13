# auth0-mock

A self-contained Go service that mocks Auth0's HTTP API surface so applications
configured to talk to Auth0 can be pointed at this mock with no code change.

- **Authentication API** — fully functional. Mints real RS256-signed JWTs and
  publishes the matching JWKS, so consumer services validate signatures
  normally.
- **Management API** — driven by Auth0's published OpenAPI 3.1 spec
  (~400 operations). Each endpoint returns 404 by default; tests register
  the response they expect via `<verb> <path>/match` siblings and clear it
  via `<verb> <path>/reset`. The same OpenAPI spec validates incoming
  requests, registered match payloads, and outgoing responses.

## Stack

Go 1.26, [go-chi/chi v5](https://github.com/go-chi/chi),
[go-chi/render](https://github.com/go-chi/render),
[getkin/kin-openapi](https://github.com/getkin/kin-openapi),
[golang-jwt/jwt v5](https://github.com/golang-jwt/jwt),
[rs/zerolog](https://github.com/rs/zerolog),
[caarlos0/env](https://github.com/caarlos0/env).

## Quick start

Local binary:

```bash
make build && ./bin/auth0-mock
```

Docker (development):

```bash
docker compose up -d --build
```

Both expose `:8080` (HTTP) and `:8443` (HTTPS, auto-generated self-signed cert
covering `localhost`, `127.0.0.1`, `::1`).

## Mocking a Management API call

```bash
# 1. Register a response (no bearer required for /match)
curl -X GET http://localhost:8080/api/v2/users/auth0%7C123/match \
  -H 'Content-Type: application/json' \
  -d '{"status":200,"body":{"user_id":"auth0|123","email":"a@x"}}'

# 2. Mint a bearer
TOKEN=$(curl -s -X POST http://localhost:8080/oauth/token \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'grant_type=client_credentials&client_id=x&client_secret=x&audience=http://localhost:8080/api/v2/' \
  | jq -r .access_token)

# 3. Call the mocked endpoint
curl http://localhost:8080/api/v2/users/auth0%7C123 \
  -H "Authorization: Bearer ${TOKEN}"
# => 200 with {"user_id":"auth0|123","email":"a@x"}
```

`/match` siblings mirror the original verb (e.g. for `GET /api/v2/users/{id}`
the sibling is `GET …/match`). The same `<verb> …/match` URL also supports
template registration (literal `{id}` in the path) for catch-all responses.

## Reset

| Endpoint                       | Scope                                    |
|--------------------------------|------------------------------------------|
| `<verb> /api/v2/…/reset`       | Clears that endpoint's matches.          |
| `POST /admin0/reset`           | Wipes ALL registered matches.            |
| `GET /admin0/matches`          | Lists every currently registered match.  |

## Configuration

Environment variables (see `.env.example`):

| Var                       | Default                                     |
|---------------------------|---------------------------------------------|
| `HTTP_ADDR`               | `0.0.0.0:8080` (empty disables HTTP)        |
| `HTTPS_ADDR`              | `0.0.0.0:8443` (empty disables HTTPS)       |
| `TLS_CERT_FILE`           | _empty_ → auto-generate self-signed         |
| `TLS_KEY_FILE`            | _empty_                                     |
| `TLS_HOSTNAMES`           | `localhost,127.0.0.1,::1`                   |
| `SIGNING_KEY_FILE`        | _empty_ → fresh RS256 key per boot          |
| `ISSUER_URL`              | `https://localhost:8443/`                   |
| `DEFAULT_AUDIENCE`        | `https://localhost:8443/api/v2/`            |
| `ACCESS_TOKEN_TTL`        | `24h`                                       |
| `ID_TOKEN_TTL`            | `24h`                                       |
| `SPEC_VALIDATION_STRICT`  | `true`                                      |
| `LOG_LEVEL`               | `info`                                      |

## Example consumer

See [`examples/consumer/`](examples/consumer/) for a stand-alone Go program
that mints a token, verifies its signature against the published JWKS using
`MicahParks/keyfunc` + `golang-jwt/jwt`, registers a Mgmt API response, and
calls it back successfully — proving the drop-in compatibility end to end.

## Design

