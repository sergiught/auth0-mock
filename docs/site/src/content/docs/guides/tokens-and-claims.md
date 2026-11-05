---
title: Tokens and claims
description: Test code that reads permissions or custom claims, and test token expiry without sleeping.
sidebar:
  order: 2
---

## Test a code path that reads a specific `permissions` claim

Use `/admin0/permissions/{audience}` to register the permissions the test needs, then mint a token for that audience. The mock injects the permissions as the JWT's `permissions` claim.

```bash
curl -X PUT 'http://localhost:8080/admin0/permissions/https://api.example.com/' \
  -H 'Content-Type: application/json' \
  -d '["read:users","write:users"]'

TOKEN=$(curl -s -X POST http://localhost:8080/oauth/token \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'grant_type=client_credentials&client_id=demo&client_secret=x&audience=https://api.example.com/' \
  | jq -r .access_token)

# Decode the JWT payload to verify
echo "$TOKEN" | cut -d. -f2 | base64 -d 2>/dev/null | jq .permissions
# => ["read:users","write:users"]
```

Different audiences get different permission sets. Tokens minted for unregistered audiences omit the `permissions` claim entirely.

## Inject a custom claim into every minted token

Tests that exercise claim-gated behaviour (e.g. "if `claim.role == admin` then ...") can set a process-wide claim map.

```bash
curl -X PUT http://localhost:8080/admin0/claims \
  -H 'Content-Type: application/json' \
  -d '{"role":"admin","org_id":"o-42"}'

TOKEN=$(curl -s -X POST http://localhost:8080/oauth/token \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'grant_type=client_credentials&client_id=demo&client_secret=x&audience=http://api/' \
  | jq -r .access_token)

echo "$TOKEN" | cut -d. -f2 | base64 -d 2>/dev/null | jq '.role, .org_id'
# => "admin"
# => "o-42"
```

**Custom claims overwrite reserved claims** (`gty`, `azp`, `permissions`, etc.) on purpose, so tests can override anything they need.

## Mint per-request claims from token-request parameters

The global claims map stamps **one** value into every token. When the system
under test fetches several tokens with **different** values for the same claim
— and the test has no safe point to swap the global claim in between — map the
request parameter to a claim name instead. `/oauth/token` then copies whatever
value the client sends (form or JSON body, any grant) into the minted token,
overriding the global map for that key.

```bash
curl -X PUT http://localhost:8080/admin0/claims/mappings \
  -H 'Content-Type: application/json' \
  -d '{"resource":"https://example.com/resource"}'

mint() {
  curl -s -X POST http://localhost:8080/oauth/token \
    -H 'Content-Type: application/x-www-form-urlencoded' \
    -d "grant_type=client_credentials&client_id=demo&client_secret=x&audience=http://api/&resource=$1" \
    | jq -r .access_token | cut -d. -f2 | base64 -d 2>/dev/null \
    | jq '."https://example.com/resource"'
}

mint urn:api:orders   # => "urn:api:orders"
mint urn:api:billing  # => "urn:api:billing" — no /admin0/claims race in between
```

Only mapped parameters are projected (the map is an allowlist); requests that
omit the parameter, and mocks with no mapping configured, behave exactly as
before. Works for the `private_key_jwt` client-credentials variant too — its
JSON body is captured the same way.

## Test token expiry without sleeps

Freeze the clock, mint a token, advance past its `exp`, then watch the
bearer middleware reject the same token. Instant and deterministic —
no `sleep`, no flaky CI on slow runners.

```bash
# 1. Freeze the mock's clock at a memorable instant.
curl -X PUT http://localhost:8080/admin0/clock \
  -H 'Content-Type: application/json' \
  -d '{"now":"2030-01-01T00:00:00Z"}'

# 2. Mint a token (defaults to ACCESS_TOKEN_TTL = 24h).
TOKEN=$(curl -s -X POST http://localhost:8080/oauth/token \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'grant_type=client_credentials&client_id=demo&client_secret=x&audience=http://localhost:8080/api/v2/' \
  | jq -r .access_token)

# 3. Stub something on /api/v2 so a 200 is possible.
curl -X POST http://localhost:8080/admin0/expectations \
  -H 'Content-Type: application/json' \
  -d '{"method":"GET","path":"/api/v2/users/x","response":{"status":200,"body":{"user_id":"x"}}}'

# 4. The token works right now.
curl -i http://localhost:8080/api/v2/users/x \
  -H "Authorization: Bearer ${TOKEN}"
# HTTP/1.1 200 OK

# 5. Advance the clock 25h — past the token's exp.
curl -X POST http://localhost:8080/admin0/clock/advance \
  -H 'Content-Type: application/json' \
  -d '{"by":"25h"}'

# 6. Same token, now rejected as expired.
curl -i http://localhost:8080/api/v2/users/x \
  -H "Authorization: Bearer ${TOKEN}"
# HTTP/1.1 401 Unauthorized
# {"errorCode":"invalid_bearer","message":"invalid bearer token", ...}

# 7. Back to wall clock when you're done.
curl -X DELETE http://localhost:8080/admin0/clock
```

The same clock drives the minter (`iat`/`exp` on `/oauth/token`) and
the bearer middleware (`exp`/`nbf` check on `/api/v2/*`), so the round
trip is internally consistent — you can freeze at any point in time and
mint tokens that look exactly like Auth0 would have issued at that
moment.

From Go tests, use `c.Clock.Freeze(ctx, t)` and `c.Clock.Advance(ctx, d)`
instead of `curl`. `auth0mocktest.Bracket(t, c)` automatically resets
the clock on cleanup along with every other admin store.
