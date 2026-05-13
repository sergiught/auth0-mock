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
| `TLS_CACHE_DIR`           | _empty_ → fresh cert per boot; if set, persist auto-gen cert to `<dir>/tls.{crt,key}` and reuse on restart |
| `TLS_HOSTNAMES`           | `localhost,127.0.0.1,::1`                   |
| `SIGNING_KEY_FILE`        | _empty_ → fresh RS256 key per boot          |
| `ISSUER_URL`              | `https://localhost:8443/`                   |
| `DEFAULT_AUDIENCE`        | `https://localhost:8443/api/v2/`            |
| `ACCESS_TOKEN_TTL`        | `24h`                                       |
| `ID_TOKEN_TTL`            | `24h`                                       |
| `SPEC_VALIDATION_STRICT`  | `true`                                      |
| `LOG_LEVEL`               | `info`                                      |

## HTTPS / TLS trust

The auto-generated cert has SAN entries for `localhost`, `127.0.0.1`, and `::1`
by default (override with `TLS_HOSTNAMES`). It works identically on macOS and
Linux at the TLS layer, but it is self-signed, so clients will reject it unless
you tell them otherwise. Three options, in order of recommendation:

**1. `mkcert` (recommended for local dev).** [`mkcert`](https://github.com/FiloSottile/mkcert)
installs a local CA into your platform's trust store and issues certs signed by
it — browsers, Go, and `curl` accept the result without flags:

```bash
mkcert -install                                   # one-time per workstation
mkcert -cert-file tls.crt -key-file tls.key localhost 127.0.0.1 ::1

docker run -e TLS_CERT_FILE=/certs/tls.crt -e TLS_KEY_FILE=/certs/tls.key \
  -v "$PWD:/certs" auth0-mock
```

**2. `TLS_CACHE_DIR` (recommended for `docker compose` without mkcert).** Pick
a path and the mock will write its auto-generated cert there on first boot, then
reuse the same files on subsequent restarts. Trust the cert once (see option 3)
and trust persists across boots:

```bash
docker compose run --rm -e TLS_CACHE_DIR=/data/tls \
  -v auth0-mock-tls:/data/tls auth0-mock
```

**3. Skip verification.** Fine for ephemeral tests, not for anything else:

```bash
curl -k https://localhost:8443/.well-known/openid-configuration
# Go: &tls.Config{InsecureSkipVerify: true}
```

To install the mock's generated cert into the OS trust store (after option 2 so
it stays stable across boots):

```bash
# Export from a running server (or read from $TLS_CACHE_DIR/tls.crt):
openssl s_client -connect localhost:8443 -showcerts </dev/null 2>/dev/null \
  | openssl x509 -outform pem > /tmp/auth0-mock.crt

# macOS
sudo security add-trusted-cert -d -r trustRoot \
  -k /Library/Keychains/System.keychain /tmp/auth0-mock.crt

# Debian/Ubuntu
sudo cp /tmp/auth0-mock.crt /usr/local/share/ca-certificates/auth0-mock.crt
sudo update-ca-certificates

# Arch/Fedora
sudo trust anchor /tmp/auth0-mock.crt
```

## Example consumer

See [`examples/consumer/`](examples/consumer/) for a stand-alone Go program
that mints a token, verifies its signature against the published JWKS using
`MicahParks/keyfunc` + `golang-jwt/jwt`, registers a Mgmt API response, and
calls it back successfully — proving the drop-in compatibility end to end.

## Design

