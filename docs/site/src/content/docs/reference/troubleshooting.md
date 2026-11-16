---
title: "Troubleshooting"
description: "Fixes for the errors people hit most often: refused connections, issuer and audience mismatches, 401 and 403 answers, stubs that do not match, and certificate errors."
sidebar:
  order: 5
---

Each section starts with what you see, then the cause and the fix.

## The container exits at start: "can be reached from other machines"

The image listens on `0.0.0.0` so that Docker's port mapping can reach it. The mock refuses to start on a non-loopback address unless you accept that `/admin0` has no authentication. The log says:

```
HTTP_ADDR=0.0.0.0:8080 and HTTPS_ADDR=0.0.0.0:8443 can be reached from other machines, but /admin0 is unauthenticated and can mint tokens; bind to 127.0.0.1, or set ADMIN_ALLOW_REMOTE=true to accept the risk (Docker needs this)
```

Set `ADMIN_ALLOW_REMOTE=true`, and publish the port on loopback only:

```bash
docker run --rm -p 127.0.0.1:8080:8080 -e ADMIN_ALLOW_REMOTE=true ghcr.io/sergiught/auth0-mock:latest
```

See [`ADMIN_ALLOW_REMOTE`](/auth0-mock/reference/configuration/) in the configuration reference.

## Connection refused from a container to a mock running on the host

Outside Docker the mock listens on `127.0.0.1:8080`. A container cannot reach the host's loopback, so it gets "connection refused". Bind to all interfaces and allow it:

```bash
HTTP_ADDR=0.0.0.0:8080 ADMIN_ALLOW_REMOTE=true auth0-mock
```

Then point the container at the host's address. Anyone who can reach the port can use `/admin0`, so keep it off shared networks.

## My API rejects the token: invalid issuer

The `iss` claim is the `ISSUER_URL` string, exactly as you set it. The default is `https://localhost:8443/`. Your API compares that string to the issuer it expects, character for character, so a missing trailing slash fails. The mock does not add or remove the slash.

Set `ISSUER_URL` to the exact value your API expects. If the address changes (a random container port, or `localhost` from the host and a service name from another container), use `auto`:

```bash
ISSUER_URL=auto
```

With `auto` the issuer is built from each request, for example `http://localhost:18111/` or `http://127.0.0.1:18111/`. A token verifies only at the address that minted it, so the mock itself answers `401 invalid bearer token` when you mint at `localhost` and call at `127.0.0.1`. Mint and call through the same address.

If you use a `/ns/<id>/` prefix, the issuer includes the prefix too. See [Issuer](/auth0-mock/guides/parallel-tests/#issuer).

## My access token is not a JWT

By default every token request gets an audience, because `DEFAULT_AUDIENCE` is `https://localhost:8443/api/v2/`. If you set `DEFAULT_AUDIENCE` to an empty value and the request has no `audience`, the access token is a random opaque string with no dots, and only `/userinfo` accepts it. Your API cannot verify it.

Send an `audience`, or set `DEFAULT_AUDIENCE` to your API's identifier:

```bash
curl -s -X POST http://localhost:8080/oauth/token \
  -d 'grant_type=client_credentials&client_id=a&client_secret=b&audience=https://api.example.com/'
```

With the default left blank, a `client_credentials` request without an audience fails:

```json
{"error":"access_denied","error_description":"No audience parameter was provided, and no default audience has been configured"}
```

See [Configuration](/auth0-mock/reference/configuration/).

## The Management API answers 403: Insufficient scope

The token does not carry any scope the operation accepts. The body is:

```json
{"statusCode":403,"error":"Forbidden","message":"Insufficient scope, expected any of: create:users","errorCode":"insufficient_scope"}
```

Only the token's `scope` claim counts. What lands there depends on the grant:

- **`client_credentials`:** if you ask for no scope, the token gets the permissions registered for the audience. With nothing registered, a Management API audience (`https://<host>/api/v2/`) gets every Management scope. Registering permissions for that audience replaces this all-scopes default, so a short list can cause the 403.
- **Password, authorization code and the other login grants:** the token carries only the scope you request. Registered permissions go into the `permissions` claim, which this check ignores. A refresh keeps the scope of the original login unless you ask for a new one.

So, put the scope in the token request:

```bash
curl -s -X POST http://localhost:8080/oauth/token \
  -d 'grant_type=client_credentials&client_id=a&client_secret=b&audience=https://localhost:8443/api/v2/&scope=create:users'
```

For a login grant, add it to the `scope` parameter of `/authorize` or the password request.

Set `BEARER_REQUIRE_SCOPES=false` to turn the check off. See [Tokens and claims](/auth0-mock/guides/tokens-and-claims/) and [Configuration](/auth0-mock/reference/configuration/).

## Tokens expire in tests that move the clock

After you move `/admin0/clock` past a token's lifetime, the token is rejected at once with `401 invalid_bearer`. Tokens expire exactly on time unless you set a leeway. Mint a new token after moving the clock, or give the check some room:

```bash
TOKEN_LEEWAY=60s
```

With the default `0s`, a token that is 30 seconds past its `exp` gets a 401. With `60s` it is still accepted. Put the clock back with `DELETE /admin0/clock`. See [Test token expiry without sleeps](/auth0-mock/guides/tokens-and-claims/#test-token-expiry-without-sleeps).

## curl or my client rejects the HTTPS certificate

The mock serves HTTPS with a self-signed certificate. curl reports `SSL certificate OpenSSL verify result: self-signed certificate (18)`. Trust the certificate, use `mkcert`, or skip verification in throwaway tests (`curl -k`).

If your client reaches the mock by another name, such as a service name in Docker Compose, the name must be on the certificate. How you add it depends on where the certificate comes from:

- **Generated by the mock:** add the name to `TLS_HOSTNAMES`.

  ```bash
  TLS_HOSTNAMES=localhost,127.0.0.1,::1,auth0-mock
  ```

- **Cached with `TLS_CACHE_DIR`:** the mock reuses the cached certificate and ignores `TLS_HOSTNAMES`. Delete `tls.crt` and `tls.key` from that directory (or the volume), restart with the new `TLS_HOSTNAMES`, then trust the new certificate again.
- **Your own (`TLS_CERT_FILE` and `TLS_KEY_FILE`, for example from `mkcert`):** reissue it with the extra name, such as `mkcert localhost 127.0.0.1 auth0-mock`.

A browser that cannot trust the certificate shows "Failed to fetch", which looks like a CORS error. See [Trusting the self-signed cert](/auth0-mock/guides/https-tls/#trusting-the-self-signed-cert).

## Stubs from one test show up in another

Stubs, claims, permissions and the clock live in one shared state. Wipe it between tests:

```bash
curl -X POST http://localhost:8080/admin0/reset
```

That also wipes what any parallel test registered. For parallel tests, give each test its own namespace instead, with the `X-Auth0-Mock-Namespace` header or a `/ns/<id>/` path prefix, and reset only that one with `POST /ns/<id>/admin0/reset`. See [Parallel tests](/auth0-mock/guides/parallel-tests/) and [Reset state between tests](/auth0-mock/guides/stubbing/#reset-state-between-tests).

## My stub never matches: 404 no_match

The request did not match any stub. If a stub exists for the operation, the error names the field that differed:

```json
{"statusCode":404,"error":"Not Found","message":"no registered match for GET /api/v2/users/auth0|x; closest stub differed on query.fields","errorCode":"no_match"}
```

With no stub for the operation, it says `no stubs registered for GET /api/v2/users/{id}`. For the full difference, read the calls no stub matched:

```bash
curl -s http://localhost:8080/admin0/journal/unmatched | jq '.entries[].nearMisses'
```

Each near miss lists the `field`, what the stub wanted (`want`) and what the request had (`got`). Namespaces keep separate journals, so ask the namespace the test uses. The journal holds `REQUEST_JOURNAL_SIZE` entries (default 1000). See [Different responses for different requests](/auth0-mock/guides/stubbing/#different-responses-for-different-requests).

## Code or refresh token rejected: invalid_grant

With `TOKEN_ENDPOINT_STRICT=true` the mock behaves like Auth0. A code works once, and a refresh token works once because every refresh rotates it. Reusing either gives a 403:

```json
{"error":"invalid_grant","error_description":"Invalid authorization code"}
{"error":"invalid_grant","error_description":"Unknown or invalid refresh token."}
```

Using an old refresh token again also revokes the newest one. Always keep the `refresh_token` from the latest response.

No refresh token in the response at all? In strict mode one is issued only when the scope includes `offline_access`. Add it to the `/authorize` scope. Without strict mode, any code works and a refresh token is always issued. See [Test strict token handling](/auth0-mock/guides/login-flows/#test-strict-token-handling).

## /u/login or /activate returns 404

Both pages exist only when `LOGIN_PAGE=true`. By default `/authorize` signs in at once and the response is `404 page not found`.

```bash
docker run --rm -p 127.0.0.1:8080:8080 -e ADMIN_ALLOW_REMOTE=true -e LOGIN_PAGE=true ghcr.io/sergiught/auth0-mock:latest
```

See [Test the login page](/auth0-mock/guides/login-flows/#test-the-login-page).

## Password login returns 403 mfa_required

The answer is:

```json
{"error":"mfa_required","error_description":"Multifactor authentication required","mfa_token":"..."}
```

MFA is switched on, either globally or for that user. The global switch stays on until you turn it off or call `/admin0/reset`. A seed file with `mfa_required: true` or an `mfa_users` entry turns it on again after each reset. Check and turn it off:

```bash
curl http://localhost:8080/admin0/mfa-required
curl -X PUT http://localhost:8080/admin0/mfa-required -d '{"required":false}'
```

A per-user setting beats the global switch, so also check `GET /admin0/mfa/users/{user}`. To finish the challenge instead, trade the `mfa_token` with one of the `mfa-*` grants. See [Test an MFA challenge flow](/auth0-mock/guides/login-flows/#test-an-mfa-challenge-flow).

## A browser app gets a CORS error

By default the mock echoes the caller's `Origin`, so a CORS error usually has another cause: an untrusted HTTPS certificate (see above), or a wrong address. If `CORS_ALLOWED_ORIGINS` is set and your app's origin is not on the list, the preflight gets a `403` and no CORS headers. Add the origin, or unset the variable:

```bash
CORS_ALLOWED_ORIGINS=http://localhost:3000
```

See [Call the mock from a browser app](/auth0-mock/guides/browser-apps/).
