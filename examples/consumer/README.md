# Consumer example

A stand-alone Go program that proves `auth0-mock` is a drop-in for Auth0 when
driven by the **official [`go-auth0`](https://github.com/auth0/go-auth0) SDK**:

1. mint an access token with the SDK's **authentication** client
   (`client_credentials` grant against `POST /oauth/token`),
2. verify the token's signature against `GET /.well-known/jwks.json` using
   `MicahParks/keyfunc` + `golang-jwt/jwt`, the **same libraries** real
   Auth0-consuming services use,
3. use the SDK's **management** client to create + read back a client
   application and a user, with the Management API responses stubbed via
   `POST /admin0/expectations`.

It lives in its **own Go module** (`examples/consumer/go.mod`) so the
`go-auth0` SDK never leaks into the `auth0-mock` module graph.

## Run

The `go-auth0` SDK only speaks HTTPS, so the example targets the mock's TLS
listener (`:8443`). The mock's auto-generated cert is self-signed, so the
example uses an HTTP client with `InsecureSkipVerify` — see the
[TLS section of the repo README](../../README.md#-https--tls) for how a real
deployment would trust it properly.

In one shell, from the repo root:

```
go run ./cmd/api
```

In another, from this directory:

```
cd examples/consumer
go run .
```

Expected output:

```
minted token via go-auth0 authentication SDK: eyJhbGciOiJSUzI1NiIsImtpZCI6Ii...
token signature verified against https://localhost:8443/.well-known/jwks.json
created + read back a client application: demo-client-id
created + read back a user: auth0|demo
```

Pass `-mock <url>` to target a different HTTPS base URL.
