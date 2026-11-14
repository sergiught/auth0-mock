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
- **Prefix**: use it when an SDK only takes a domain or issuer URL and you can't add a header. Point the SDK at `localhost:8080/ns/<id>` (auth0-spa-js, express-openid-connect and nextjs-auth0 all work this way).

SDKs that build every URL from the host alone, such as go-auth0, can't carry a prefix. Give them a custom HTTP client that adds the header, via `management.WithClient`:

```go
type nsTransport struct{ id string }

func (t nsTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("X-Auth0-Mock-Namespace", t.id)
	return http.DefaultTransport.RoundTrip(r)
}

api, err := management.New("localhost:8080",
	management.WithInsecure(),
	management.WithClient(&http.Client{Transport: nsTransport{"test-42"}}),
	// ...credentials
)
```

<!-- go-sdk -->

## What is shared and what is private

| Private to each namespace | Shared by all |
| --- | --- |
| Stubs and expectations, claims, claim mappings, permissions, the MFA flag, PKCE codes, sessions and refresh tokens, authorize-error rules, the request journal, the event stream, the clock | The signing key, the OpenAPI spec and validator, the environment config |

Because the key is shared, a token minted in one namespace still verifies in another, as long as the `iss` matches (see below).

`/healthz` and `/readyz` are shared too.

## Issuer

With the header, the issuer is unchanged. With the prefix, it becomes `<issuer-base>/ns/<id>/` in the discovery document, in `jwks_uri` and in the `iss` of every token, so an SDK that validates `iss` against the domain you gave it keeps working.

## Seed file

A [seed file](/auth0-mock/guides/seed-file/) is applied when a namespace is created, and a namespace reset honours `keep_seed`. Hot reload of the seed file only re-applies it to the default namespace; other namespaces pick up the change on their next reset.

## Lifecycle

These routes are global. They never create a namespace.

| Route | Effect |
| --- | --- |
| `GET /admin0/namespaces` | List `[{"id","created_at","last_used_at"}]` |
| `DELETE /admin0/namespaces/{id}` | 204, or 404 `namespace_not_found`. Open event streams of that namespace end. |
| `DELETE /admin0/namespaces` | 204, deletes all |

Deleting a namespace does not wait for requests already in flight inside it.

## The cap

`NAMESPACE_MAX` (default `256`) limits how many namespaces can exist. At the cap, a request for a new one gets `429 too_many_namespaces` with `namespace limit reached (NAMESPACE_MAX=<n>); delete unused namespaces with DELETE /admin0/namespaces/{id}`. Existing namespaces keep working, and nothing is evicted for you, so delete namespaces when a test ends. `0` turns namespaces off.

## Walkthrough

```bash
# Different stubs in two namespaces.
curl -X POST http://localhost:8080/ns/a/admin0/expectations \
  -d '{"method":"GET","path":"/api/v2/users/auth0|1","response":{"status":200,"body":{"user_id":"auth0|1","email":"a@x"}}}'
curl -X POST http://localhost:8080/admin0/expectations \
  -H 'X-Auth0-Mock-Namespace: b' \
  -d '{"method":"GET","path":"/api/v2/users/auth0|1","response":{"status":200,"body":{"user_id":"auth0|1","email":"b@x"}}}'

# Each answers from its own stubs.
curl -s -H "Authorization: Bearer $TOKEN_A" http://localhost:8080/ns/a/api/v2/users/auth0%7C1
curl -s -H "Authorization: Bearer $TOKEN_B" -H 'X-Auth0-Mock-Namespace: b' http://localhost:8080/api/v2/users/auth0%7C1

# Reset only one, then clean up.
curl -X POST http://localhost:8080/ns/a/admin0/reset
curl http://localhost:8080/admin0/namespaces
curl -X DELETE http://localhost:8080/admin0/namespaces/a
```
