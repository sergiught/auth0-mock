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

### Repeated query keys

A query value in `request.query` is either a string or a list of strings. A string matches when any of the request's values for that key equals it, so `"fields":"email"` matches `?fields=name&fields=email`. A list matches when the request carries every listed value, in any order, with extra values allowed; a repeated value must appear that many times. A one-element list is the same as the string. An empty list or a non-string item is rejected with `400 invalid_request_match`.

```bash
# Matches ?fields=name&fields=email, ?fields=email&fields=x&fields=name, ...
curl -X POST http://localhost:8080/admin0/expectations \
  -H 'Content-Type: application/json' \
  -d '{"method":"GET","path":"/api/v2/users",
       "request":{"query":{"fields":["name","email"]}},
       "response":{"status":200,"body":[]}}'
```

In Go, use `WithQuery(key, value)` for the string form and `WithQueryAll(key, values...)` for a list.

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

## Dynamic responses

A stub returns the same body on every call. For `POST /api/v2/users` that means every user gets the same `user_id`. Add `"template": true` to a response and the mock fills in placeholders each time the stub is served. From release 0.234.0.

Templating is off by default, and you turn it on per response. Auth0's email templates and Universal Login templates contain Liquid text like `{{ user.email }}`, and a stub that returns them must come back unchanged.

```bash
curl -X POST http://localhost:8080/admin0/expectations \
  -H 'Content-Type: application/json' \
  -d '{"method":"POST","path":"/api/v2/users","response":{"status":201,"template":true,"headers":{"Location":"/api/v2/users/auth0|{{uuid}}"},"body":{"user_id":"auth0|{{uuid}}","email":"{{body.email}}","email_verified":"{{body.email_verified}}","created_at":"{{now}}","identities":[{"provider":"auth0","user_id":"{{uuid}}","connection":"{{body.connection}}","isSocial":false}]}}}'

curl -s -X POST http://localhost:8080/api/v2/users \
  -H "Authorization: Bearer ${TOKEN}" -H 'Content-Type: application/json' \
  -d '{"email":"jane@example.com","email_verified":true,"connection":"Username-Password-Authentication","password":"S3cretPass!"}'
```

Each call gets a new `user_id`, the `email` you posted, and the current time in `created_at`.

The Go SDK equivalent:

```go
_, err := c.ExpectPost("/api/v2/users").
    Respond(201).Template().
    Header("Location", "/api/v2/users/auth0|{{uuid}}").
    JSON(map[string]any{
        "user_id":        "auth0|{{uuid}}",
        "email":          "{{body.email}}",
        "email_verified": "{{body.email_verified}}",
        "created_at":     "{{now}}",
    }).
    Apply(ctx)
```

`Template()` flags only the entry you are building. After `Then(...)`, call it again for that entry.

### Placeholders

| Placeholder | Renders to |
|---|---|
| `{{uuid}}` | A random lowercase UUID v4 |
| `{{now}}` | The mock's clock as UTC time with milliseconds, like `2026-11-07T12:34:56.789Z` |
| `{{path.NAME}}` | The decoded path parameter `NAME` (`/api/v2/users/{id}` gives `{{path.id}}`) |
| `{{body.PATH}}` | A field of the JSON request body, by [gjson path](https://github.com/tidwall/gjson/blob/master/SYNTAX.md) such as `email` or `app_metadata.plan`; a multipath like `{email,user_id}` works too |

Spaces inside the braces are fine: `{{ uuid }}`.

Placeholders work in JSON string values of the body and in header values. Object keys, numbers, booleans and nulls are never changed.

- **One `uuid` and one `now` per response.** Every `{{uuid}}` in a response, headers included, gets the same value, so `user_id`, `identities[0].user_id` and `Location` agree. The next call gets a new one.
- **`{{now}}` follows the clock.** It reads `/admin0/clock`. Freeze the clock and `created_at` is the frozen time.
- **Whole-string `{{body.PATH}}` keeps its type.** If a string is only `{{body.PATH}}`, the field replaces the whole string with its own type. `"email_verified": "{{body.email_verified}}"` gives `true`, and an object field gives the object.
- **Embedded `{{body.PATH}}` is text.** In `"User {{body.email}}"`, a string goes in as is and any other value goes in as its JSON text.
- **A missing field in a whole-string `{{body.PATH}}` is left out.** The key is removed from its object, or the element from its array, the way Auth0 leaves out fields you didn't send. If the placeholder is the entire body, it renders `""`. This also applies to a request with no JSON body.
- **A missing field in an embedded `{{body.PATH}}` renders `""`.** `"User {{body.email}}"` gives `"User "`.
- **Literal braces.** Write `\{{` (in JSON, `"\\{{"`) to get a literal `{{`.

The mock checks the template when you register the stub. An unknown placeholder, a bad path parameter name or an unclosed `{{` gives a 400 with the code `invalid_template`. The body is also checked against the response schema. A whole-string `{{body.PATH}}` is skipped, since its type is only known when served. Any other templated string must be a string in the schema, so `"email_verified": "x{{body.email_verified}}"` fails. Its value is checked too, against an enum or a format for example, unless the text depends on the request body or on a path parameter the stub leaves as `{name}`; those values are checked when served. A typo in a field name fails only where the schema forbids extra fields, which `POST /api/v2/users` does not. A header that renders with a line break or another control character gives a 500 `invalid_template` when served.

The rendered body is checked against the schema on every call, as for any stub. If a placeholder renders the wrong type or value, strict mode returns a 500 `invalid_match`.

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
