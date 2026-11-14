---
title: Parallel tests
description: Share one running mock between many tests, each with its own private state, using namespaces.
sidebar:
  order: 2.5
---

## Share one mock between parallel tests

`/admin0/reset` wipes everything, so two tests sharing a mock step on each other's stubs, claims and clock. A **namespace** gives each test its own private copy of that state on the same running mock.

Pick a namespace per request in one of two ways:

- the header `X-Auth0-Mock-Namespace: <id>`
- a path prefix: `/ns/<id>/…`, for example `/ns/test-42/oauth/token`

An id is 1 to 64 characters from `A-Z a-z 0-9 _ -`. A namespace is created the first time it is used. A request with neither header nor prefix uses the default namespace, which behaves exactly as the mock always has. If the header and the prefix are both present and differ, the answer is `400 invalid_namespace`.

## Header or prefix

- **Header**: use it when you control the HTTP client. The issuer stays the same.
- **Prefix**: use it when an SDK only takes a domain or issuer URL and you can't add a header. Point the SDK at `localhost:8443/ns/<id>` (auth0-spa-js, express-openid-connect and nextjs-auth0 all work this way).

SDKs that build every URL from the host alone, such as go-auth0, can't carry a prefix. Send the header instead, with `option.WithHTTPHeader` (or a custom HTTP client):

```go
import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"

	client "github.com/auth0/go-auth0/v3/management/client"
	"github.com/auth0/go-auth0/v3/management/option"
)

// Trust the mock's self-signed certificate (.auth0-mock/tls.crt).
pem, err := os.ReadFile(".auth0-mock/tls.crt")
if err != nil {
	return err
}
pool := x509.NewCertPool()
pool.AppendCertsFromPEM(pem)
httpClient := &http.Client{Transport: &http.Transport{
	TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
}}

mgmt, err := client.New("localhost:8443",
	option.WithClientCredentials(ctx, "demo", "x"),
	option.WithHTTPClient(httpClient),
	option.WithHTTPHeader(http.Header{"X-Auth0-Mock-Namespace": []string{"test-42"}}),
)
```

The token request may go to the default namespace. That is fine in header mode, because the issuer is the same.

## Go: `auth0mocktest.Isolate`

In Go tests, `auth0mocktest.Isolate` gives each test its own namespace. It checks your stubs when the test ends, then deletes the namespace. Stub through `ns`, and hand `ns.Transport(base)` to the code under test: it adds the header to every request.

```go
func TestCreateUser(t *testing.T) {
	t.Parallel()
	ns := auth0mocktest.Isolate(t, mock) // mock is the shared *auth0mock.Client

	auth0mocktest.MustApply(t, ns.ExpectPost("/api/v2/users").
		Respond(201).
		JSON(map[string]any{"user_id": "auth0|1"}))

	// base trusts the mock's certificate, as in the example above.
	httpClient := &http.Client{Transport: ns.Transport(base)}
	mgmt, err := client.New("localhost:8443",
		option.WithClientCredentials(ctx, "demo", "x"),
		option.WithHTTPClient(httpClient),
	)
	// ... call mgmt from the code under test ...
}
```

The imports are `github.com/auth0/go-auth0/v3/management/client` and `github.com/auth0/go-auth0/v3/management/option`.

## What is shared and what is private

| Private to each namespace | Shared by all |
| --- | --- |
| Stubs and expectations, claims, claim mappings, permissions, the MFA flag, PKCE codes, sessions and refresh tokens, authorize-error rules, the request journal, the event stream, the clock | The signing key, the OpenAPI spec and validator, the environment config |

The signing key is shared, but a token only verifies where its issuer matches. With the header, the issuer is the same in every namespace, so a token minted in one verifies in another. With the prefix, the issuer includes `/ns/<id>/`, so a token from the default namespace or from another prefix is rejected under `/ns/<id>/`: mint the token under the same prefix you call. A token minted while a namespace's clock is moved carries that clock's `iat` and `exp`.

`/healthz` and `/readyz` are shared too.

## Issuer

With the header, the issuer is unchanged. With the prefix, it becomes `<issuer-base>/ns/<id>/` in the discovery document, in `jwks_uri` and in the `iss` of every token, so an SDK that validates `iss` against the domain you gave it keeps working.

## Seed file

A [seed file](/auth0-mock/guides/seed-file/) is applied when a namespace is created, and a namespace reset honours `keep_seed`. Hot reload of the seed file only re-applies it to the default namespace; other namespaces pick up the change on their next reset.

## Lifecycle

These routes are global. They never create a namespace and ignore the header and prefix.

| Route | Effect |
| --- | --- |
| `GET /admin0/namespaces` | List `[{"id","created_at","last_used_at"}]` |
| `DELETE /admin0/namespaces/{id}` | 204, or 404 `namespace_not_found`. Open event streams of that namespace end. |
| `DELETE /admin0/namespaces` | 204, deletes all. Refused with 400 `invalid_namespace` if the request names a namespace by header or prefix. |

Deleting a namespace does not wait for requests already in flight inside it.

## The cap

`NAMESPACE_MAX` (default `256`) limits how many namespaces can exist. At the cap, a request for a new one gets `429 too_many_namespaces` with `namespace limit reached (NAMESPACE_MAX=<n>); delete unused namespaces with DELETE /admin0/namespaces/{id}`. Existing namespaces keep working, and nothing is evicted for you, so delete namespaces when a test ends.

Each namespace keeps its own request journal: up to `REQUEST_JOURNAL_SIZE` entries (default 1000), each storing up to 64 KiB of request body. A namespace with small requests costs well under 1 MB, but one that keeps receiving large bodies can reach about 62.5 MiB, or about 16 GiB across 256 namespaces. If your tests send large bodies, lower `REQUEST_JOURNAL_SIZE` or `NAMESPACE_MAX`. Delete namespaces you no longer need.

`NAMESPACE_MAX=0` turns namespaces off.

## Walkthrough

```bash
# Different stubs in two namespaces.
curl -X POST http://localhost:8080/ns/a/admin0/expectations \
  -H 'Content-Type: application/json' \
  -d '{"method":"GET","path":"/api/v2/users/auth0|1","response":{"status":200,"body":{"user_id":"auth0|1","email":"a@x"}}}'
curl -X POST http://localhost:8080/admin0/expectations \
  -H 'X-Auth0-Mock-Namespace: b' \
  -H 'Content-Type: application/json' \
  -d '{"method":"GET","path":"/api/v2/users/auth0|1","response":{"status":200,"body":{"user_id":"auth0|1","email":"b@x"}}}'

# Mint a token per namespace (under the prefix, or with the header).
TOKEN_A=$(curl -s -X POST http://localhost:8080/ns/a/oauth/token \
  -d 'grant_type=client_credentials&client_id=demo&client_secret=x&audience=http://localhost:8080/api/v2/' \
  | jq -r .access_token)
TOKEN_B=$(curl -s -X POST http://localhost:8080/oauth/token \
  -H 'X-Auth0-Mock-Namespace: b' \
  -d 'grant_type=client_credentials&client_id=demo&client_secret=x&audience=http://localhost:8080/api/v2/' \
  | jq -r .access_token)

# Each answers from its own stubs.
curl -s -H "Authorization: Bearer $TOKEN_A" http://localhost:8080/ns/a/api/v2/users/auth0%7C1
curl -s -H "Authorization: Bearer $TOKEN_B" -H 'X-Auth0-Mock-Namespace: b' http://localhost:8080/api/v2/users/auth0%7C1

# Reset only one, then clean up.
curl -X POST http://localhost:8080/ns/a/admin0/reset
curl http://localhost:8080/admin0/namespaces
curl -X DELETE http://localhost:8080/admin0/namespaces/a
```
