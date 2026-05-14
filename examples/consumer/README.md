# Consumer example

A stand-alone Go program that proves `auth0-mock` is a drop-in for Auth0 from an
OIDC consumer's perspective:

1. mint an access token via `POST /oauth/token` (`client_credentials` grant),
2. verify the token's signature against `GET /.well-known/jwks.json` using
   `MicahParks/keyfunc` + `golang-jwt/jwt`, the **same libraries** real
   Auth0-consuming services use,
3. register a mocked Management API response for `GET /api/v2/users/auth0|demo`
   via `POST /admin0/expectations`, then call it with the bearer token and
   confirm a `200 OK`.

## Run

In one shell:

```
go run ./cmd/api
```

In another:

```
go run ./examples/consumer
```

Expected output:

```
minted token eyJhbGciOiJSUzI1NiIsImtpZCI6Ii...
token signature verified against http://localhost:8080/.well-known/jwks.json
registered + retrieved a mocked Mgmt API resource
```

Pass `-mock <url>` to target a different base URL (e.g. the HTTPS port).
