---
title: Stubbing the Management API
description: Stub Management API responses, errors and retries, and reset or inspect what is registered.
sidebar:
  order: 1
---

## Mint a token and call a stubbed Management API endpoint

The hello-world of auth0-mock.

```bash
# 1. Stub the response (no auth needed for /admin0/expectations)
curl -X POST http://localhost:8080/admin0/expectations \
  -H 'Content-Type: application/json' \
  -d '{"method":"GET","path":"/api/v2/users/auth0|123","response":{"status":200,"body":{"user_id":"auth0|123","email":"alice@x"}}}'

# 2. Mint a bearer
TOKEN=$(curl -s -X POST http://localhost:8080/oauth/token \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'grant_type=client_credentials&client_id=demo&client_secret=x&audience=http://localhost:8080/api/v2/' \
  | jq -r .access_token)

# 3. Call the stubbed endpoint
curl http://localhost:8080/api/v2/users/auth0%7C123 \
  -H "Authorization: Bearer ${TOKEN}"
# => {"user_id":"auth0|123","email":"alice@x"}
```

Note `%7C` is URL-encoded `|`, required because `|` is reserved in URLs.

## Stub multiple users at once

Concrete URLs stub one entity; template URLs (containing `{id}`) stub a fallback for the whole endpoint pattern. Concrete wins over template.

```bash
# Template fallback: any user lookup returns this
curl -X POST http://localhost:8080/admin0/expectations \
  -H 'Content-Type: application/json' \
  -d '{"method":"GET","path":"/api/v2/users/{id}","response":{"status":200,"body":{"user_id":"auth0|*","email":"anyone@x"}}}'

# Concrete override for alice
curl -X POST http://localhost:8080/admin0/expectations \
  -H 'Content-Type: application/json' \
  -d '{"method":"GET","path":"/api/v2/users/auth0|alice","response":{"status":200,"body":{"user_id":"auth0|alice","email":"alice@x"}}}'

# alice returns her own data; everyone else gets the template fallback
curl -H "Authorization: Bearer ${TOKEN}" http://localhost:8080/api/v2/users/auth0%7Calice  # → alice@x
curl -H "Authorization: Bearer ${TOKEN}" http://localhost:8080/api/v2/users/auth0%7Cbob    # → anyone@x
```

## Different responses for different requests

Multiple expectations can be registered for the same operation and conditioned on the incoming request body or query parameters. The mock applies a 4-tier precedence: an exact-path expectation beats a template-path one, and within a path level a request-matched expectation beats a catch-all. Newest wins within a tier.

```bash
# Register two expectations on the same operation, matched by request body.
# Precedence: exact-path+matcher > exact-path+catch-all > template+matcher > template+catch-all.
# Newest-registered wins within each tier.
curl -X POST http://localhost:8080/admin0/expectations \
  -H 'Content-Type: application/json' \
  -d '{"method":"POST","path":"/api/v2/users",
       "request":{"body":{"email":"a@example.com"}},
       "response":{"status":201,"body":{"user_id":"auth0|a"}}}'

curl -X POST http://localhost:8080/admin0/expectations \
  -H 'Content-Type: application/json' \
  -d '{"method":"POST","path":"/api/v2/users",
       "request":{"body":{"email":"b@example.com"}},
       "response":{"status":201,"body":{"user_id":"auth0|b"}}}'
```

A `POST /api/v2/users` request carrying `{"email":"a@example.com", ...}` returns `{"user_id":"auth0|a"}`; one carrying `{"email":"b@example.com", ...}` returns `{"user_id":"auth0|b"}`. Omit `request` entirely (or send `{}`) for a catch-all that fires when no more-specific matcher applies.

To clear all expectations for an operation at once (catch-all + every request-matched one):

```bash
curl -X DELETE http://localhost:8080/admin0/expectations \
  -H 'Content-Type: application/json' \
  -d '{"method":"POST","path":"/api/v2/users"}'
```

## Stub an error response (400, 429, 500)

The status must be one the spec declares for the operation. Auth0's spec gives error statuses no body schema, so an error body is checked against Auth0's error envelope instead: `statusCode` (equal to the stub's status), `error` and `message` are required, `errorCode` is optional, and any other field is rejected so a typo like `mesage` fails at registration. An empty body is also accepted.

```bash
# Force a 429 rate-limit on the next call to GET /api/v2/users/auth0|x
curl -X POST http://localhost:8080/admin0/expectations \
  -H 'Content-Type: application/json' \
  -d '{"method":"GET","path":"/api/v2/users/auth0|x","response":{"status":429,"headers":{"X-RateLimit-Limit":"50","Retry-After":"60"},"body":{"statusCode":429,"error":"Too Many Requests","message":"Rate limit exceeded"}}}'

curl -i -H "Authorization: Bearer ${TOKEN}" http://localhost:8080/api/v2/users/auth0%7Cx
# HTTP/1.1 429
# Retry-After: 60
# X-RateLimit-Limit: 50
# ...
```

Registered headers come through on the response, so you can test client-side rate-limit handling realistically.

## Test retries: 429, then 200

Add an optional `then` list to answer successive calls differently: call 1 gets `response`, call 2 gets `then[0]`, and so on, and the last entry repeats once the list runs out. `Hits` counts every call, and re-registering the stub restarts the sequence. Each entry is validated at registration like `response`; an error names the entry, for example `then[0]`.

```bash
# First call is rate-limited, every call after that succeeds
curl -X POST http://localhost:8080/admin0/expectations \
  -H 'Content-Type: application/json' \
  -d '{"method":"GET","path":"/api/v2/users/auth0|x","response":{"status":429,"headers":{"Retry-After":"0"},"body":{"statusCode":429,"error":"Too Many Requests","message":"Rate limit exceeded"}},"then":[{"status":200,"body":{"user_id":"auth0|x","email":"x@example.com"}}]}'
```

The Go SDK equivalent:

```go
_, err := c.ExpectGet("/api/v2/users/auth0|x").
    Respond(429).Header("Retry-After", "0").JSON(map[string]any{"statusCode": 429, "error": "Too Many Requests", "message": "Rate limit exceeded"}).
    Then(200).JSON(map[string]any{"user_id": "auth0|x", "email": "x@example.com"}).
    Apply(ctx)
```

go-auth0 retries a 429 up to twice and waits at least 1s; `Retry-After: 0` keeps each wait at that floor.

## Reset state between tests

The cheapest possible teardown: one POST wipes everything:

```bash
curl -X POST http://localhost:8080/admin0/reset
```

Or use the more targeted resets:

```bash
# Clear one Management API stub
curl -X DELETE http://localhost:8080/admin0/expectations \
  -H 'Content-Type: application/json' \
  -d '{"method":"GET","path":"/api/v2/users/auth0|x"}'

# Clear just the custom-claim map
curl -X DELETE http://localhost:8080/admin0/claims

# Clear permissions for one audience
curl -X DELETE 'http://localhost:8080/admin0/permissions/https://api.example.com/'

# Clear all audiences' permissions
curl -X DELETE http://localhost:8080/admin0/permissions

# Turn MFA off
curl -X PUT http://localhost:8080/admin0/mfa-required \
  -H 'Content-Type: application/json' -d '{"required":false}'
```

## Inspect what's currently registered

When a test isn't behaving as expected, list the live state:

```bash
curl http://localhost:8080/admin0/expectations | jq .
curl http://localhost:8080/admin0/claims | jq .
curl http://localhost:8080/admin0/permissions | jq .
curl http://localhost:8080/admin0/mfa-required | jq .
```
