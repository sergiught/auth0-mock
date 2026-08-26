# Cookbook

Practical recipes for using auth0-mock in tests. Each recipe is self-contained: copy, paste, adjust.

## 📑 Table of contents

- [Mint a token and call a stubbed Management API endpoint](#mint-a-token-and-call-a-stubbed-management-api-endpoint)
- [Stub multiple users at once](#stub-multiple-users-at-once)
- [Different responses for different requests](#different-responses-for-different-requests)
- [Test a code path that reads a specific `permissions` claim](#test-a-code-path-that-reads-a-specific-permissions-claim)
- [Inject a custom claim into every minted token](#inject-a-custom-claim-into-every-minted-token)
- [Mint per-request claims from token-request parameters](#mint-per-request-claims-from-token-request-parameters)
- [Test a PKCE flow end-to-end](#test-a-pkce-flow-end-to-end)
- [Test an MFA challenge flow](#test-an-mfa-challenge-flow)
- [Test token expiry without sleeps](#test-token-expiry-without-sleeps)
- [Test the password-realm grant](#test-the-password-realm-grant)
- [Stub an error response (400, 429, 500)](#stub-an-error-response-400-429-500)
- [Reset state between tests](#reset-state-between-tests)
- [Inspect what's currently registered](#inspect-whats-currently-registered)
- [Run against HTTPS with a trusted cert](#run-against-https-with-a-trusted-cert)
- [Drive an event-stream consumer from a test](#drive-an-event-stream-consumer-from-a-test)
- [Use a Go test that boots the mock in-process](#use-a-go-test-that-boots-the-mock-in-process)
- [Trust the self-signed cert system-wide](#trusting-the-self-signed-cert)

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

## Test a PKCE flow end-to-end

```bash
# 1. Compute the S256 challenge from a known verifier
VERIFIER="the-quick-brown-fox-jumps-over-the-lazy-dog-43"
CHALLENGE=$(echo -n "$VERIFIER" | openssl dgst -sha256 -binary | base64 | tr '+/' '-_' | tr -d '=')

# 2. Hit /authorize with the challenge, server stashes it against the issued code
LOCATION=$(curl -s -i "http://localhost:8080/authorize?client_id=demo&redirect_uri=https://app/cb&state=s1&response_type=code&code_challenge=${CHALLENGE}&code_challenge_method=S256" \
  | grep -i '^location:' | cut -d' ' -f2 | tr -d '\r')

CODE=$(echo "$LOCATION" | sed -n 's/.*code=\([^&]*\).*/\1/p')

# 3. Exchange the code with the matching verifier
curl -s -X POST http://localhost:8080/oauth/token \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d "grant_type=authorization_code&client_id=demo&code=${CODE}&redirect_uri=https://app/cb&code_verifier=${VERIFIER}" \
  | jq .access_token

# Wrong verifier? 400 invalid_grant
curl -s -X POST http://localhost:8080/oauth/token \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d "grant_type=authorization_code&client_id=demo&code=${CODE}&code_verifier=wrong-verifier" \
  | jq .
# => {"error":"invalid_grant","error_description":"PKCE verification failed: S256 mismatch"}
```

Both `S256` and `plain` are supported. `plain` is the default when `code_challenge_method` is omitted (per RFC 7636).

## Test an MFA challenge flow

Two steps: enable MFA, then perform the full dance.

```bash
# 1. Turn MFA on
curl -X PUT http://localhost:8080/admin0/mfa-required \
  -H 'Content-Type: application/json' \
  -d '{"required":true}'

# 2. Initial password grant returns 403 with an mfa_token
MFA_TOKEN=$(curl -s -X POST http://localhost:8080/oauth/token \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'grant_type=password&client_id=demo&username=alice@x&password=ignored&audience=http://api/' \
  | jq -r .mfa_token)

# 3. Exchange the mfa_token with one of three MFA grants:

# OTP (TOTP / HOTP)
curl -s -X POST http://localhost:8080/oauth/token \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d "grant_type=http://auth0.com/oauth/grant-type/mfa-otp&mfa_token=${MFA_TOKEN}&otp=123456&client_id=demo" \
  | jq .access_token

# OOB (push / SMS)
curl -X POST http://localhost:8080/oauth/token \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d "grant_type=http://auth0.com/oauth/grant-type/mfa-oob&mfa_token=${MFA_TOKEN}&oob_code=push-abc&binding_code=123456&client_id=demo"

# Recovery code
curl -X POST http://localhost:8080/oauth/token \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d "grant_type=http://auth0.com/oauth/grant-type/mfa-recovery-code&mfa_token=${MFA_TOKEN}&recovery_code=ABCDEFGHIJKLMNOP&client_id=demo"
```

The accepted factor values are constants:

| Factor | Accepted value |
|---|---|
| `otp` | `123456` |
| `binding_code` (paired with any `oob_code`) | `123456` |
| `recovery_code` | `ABCDEFGHIJKLMNOP` |

Wrong factors return `403 invalid_grant`. The minted token carries `gty=mfa-otp` (or `mfa-oob` / `mfa-recovery-code`) so downstream services can identify stepped-up sessions.

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

## Test the password-realm grant

Auth0 Native SDKs (auth0-android, auth0-swift, auth0-react-native) use the password-realm grant to target a specific connection.

```bash
curl -X POST http://localhost:8080/oauth/token \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'grant_type=http://auth0.com/oauth/grant-type/password-realm&client_id=demo&username=alice@x&password=ignored&realm=Username-Password-Authentication&audience=http://api/&scope=openid profile email'

# Issued token carries the realm in the connection claim:
# {"connection":"Username-Password-Authentication", "gty":"password-realm", ...}
```

Missing `realm` returns `400 invalid_request`.

## Stub an error response (400, 429, 500)

The registration validator rejects bodies that violate the spec for the chosen status, but valid error shapes are fine.

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

## Run against HTTPS with a trusted cert

> [!WARNING]
> **macOS Go ignores `SSL_CERT_FILE` and `SSL_CERT_DIR`**: those env vars are honored on Linux but not on macOS, where Go reads roots from the system Security framework. So the Linux shortcut (`SSL_CERT_FILE=./tls.crt go run …`) won't work on macOS. Use `mkcert` (which writes its CA into the keychain), the [trust-store recipe](#trusting-the-self-signed-cert) below (`security add-trusted-cert …`), or construct a `tls.Config{RootCAs: pool}` in client code.

For local dev, use [`mkcert`](https://github.com/FiloSottile/mkcert):

```bash
mkcert -install                                                  # one-time
mkcert -cert-file tls.crt -key-file tls.key localhost 127.0.0.1 ::1

docker run -p 8443:8443 \
  -e TLS_CERT_FILE=/certs/tls.crt -e TLS_KEY_FILE=/certs/tls.key \
  -v "$PWD:/certs" auth0-mock

curl https://localhost:8443/.well-known/openid-configuration   # no -k needed
```

For ephemeral CI tests that just need to skip verification, set `InsecureSkipVerify: true` on your client's TLS config (Go) or pass `-k` (curl). Don't do this in production.

## Drive an event-stream consumer from a test

Use this when your service consumes Auth0's event stream and you want
to assert it reacts correctly to specific events. The mock exposes
`GET /api/v2/events` as a real SSE endpoint and `POST /admin0/events`
as the producer side. The `auth0mocktest` helpers handle the SSE
framing so the test stays focused on intent.

```go
import (
    "testing"
    "time"

    "github.com/sergiught/auth0-mock/pkg/auth0mock"
    "github.com/sergiught/auth0-mock/pkg/auth0mock/auth0mocktest"
)

func TestConsumer_ReactsToUserCreated(t *testing.T) {
    c, err := auth0mock.NewClient("http://localhost:8080")
    if err != nil { t.Fatal(err) }
    auth0mocktest.ResetOnCleanup(t, c)

    bearer := mintBearerForYourConsumer(t) // however your test rig gets one

    // Open a subscription with an event-type filter. The helper
    // strips keep-alives and yields one SSEEvent per real frame.
    stream := auth0mocktest.SubscribeEvents(t, c, bearer, "event_type=user.created")

    // Block until the subscription has registered server-side, rather
    // than sleeping a fixed guess. active is eventually-consistent, so
    // this is the deterministic way to know a pushed event will be
    // fanned out to this stream.
    auth0mocktest.WaitForActiveSubscribers(t, c, 1, time.Second)

    // Push an event. The mock validates the envelope against the
    // OpenAPI text/event-stream schema; misshapen bodies surface
    // as APIError("invalid_event") with a one-line reason. Use
    // auth0mock.NewEventID() to get a fresh ID that conforms to the
    // `evt_` + 16-char pattern — the schema rejects literals of the
    // wrong length, and hand-counting is a common paste-and-go trap.
    eventID := auth0mock.NewEventID()
    streamID := auth0mock.NewStreamID()
    const offset = "0" // the resume cursor; surfaces as the SSE id
    auth0mocktest.MustPush(t, c, fmt.Sprintf(`{
        "type":"user.created","offset":%q,
        "event":{
            "specversion":"1.0","type":"user.created","source":"https://auth0.local/",
            "id":%q,"time":"2026-05-19T00:00:00Z",
            "a0tenant":"my-tenant","a0stream":%q,
            "data":{"object":{
                "user_id":"u-1",
                "created_at":"2026-05-19T00:00:00Z",
                "updated_at":"2026-05-19T00:00:00Z",
                "identities":[]
            }}
        }
    }`, offset, eventID, streamID))

    // Block until your consumer (downstream of the SSE stream)
    // observes the event, then assert it reacted as expected. Matching
    // Auth0's Events API, the SSE id is the offset (the resume cursor),
    // not the CloudEvent event.id.
    evt := stream.NextEvent(t, 3*time.Second)
    if evt.ID != offset {
        t.Fatalf("got id=%q want offset %s", evt.ID, offset)
    }
    // ... drive your consumer's assertion here ...
}
```

**Resume from a known offset** by passing `from=<offset>` or
`from_timestamp=<rfc3339>` as the query string — the offset is the value
a prior event delivered in its SSE `id:` line. The mock keeps a bounded
ring buffer (default 100 events, see `EVENTS_REPLAY_BUFFER`) and replays
missed events on reconnect.

**Simulate an in-band stream error** to exercise a consumer's reconnect
or cursor-fallback path by pushing an error frame:

```go
auth0mocktest.MustPush(t, c, `{"type":"error","error":{"code":"cursor_expired","message":"cursor expired"}}`)
```

Matching Auth0, error frames are control signals, not events: every
subscriber receives them regardless of `event_type` filter, they're
never buffered/replayed (no offset), and the stream **closes** right
after — which is what drives your consumer to reconnect.

**Advance the cursor during idle** with an `offset-only` progress marker —
exercise a consumer that persists offsets from non-event messages and
resumes after a quiet period:

```go
auth0mocktest.MustPush(t, c, `{"type":"offset-only","offset":"42"}`)
```

Like error frames, markers reach every subscriber regardless of
`event_type` filter. But they carry an offset, so (unlike errors) they
**are** buffered and `from=42` is a valid resume point.

**Assert a stream closed cleanly** by waiting for the active count to
drain after you (or your consumer-under-test) disconnect:

```go
stream.Close() // or let your consumer shut down
auth0mocktest.WaitForActiveSubscribers(t, c, 0, time.Second)
```

For lifetime counts — e.g. asserting a consumer reconnected — read
`c.Events.Subscribers(ctx)` and check `.Total` (connections since the
last reset; it never decreases within a window).

**Reset between tests** via `auth0mocktest.ResetOnCleanup(t, c)` —
this drains every open subscriber and clears the replay buffer
without permanently breaking the hub, so the next test starts from a
known-empty state.

**Force a cursor to age out** when you need to exercise a consumer's
unrecoverable-cursor-loss path. `ExpireEvents` expires every buffered
cursor, so the next reconnect presenting one gets `410 event_aged_out`;
`ExpireEventsBefore` expires only what is older than the cursor you
name, leaving that cursor and everything after it replayable. Both
report how many cursors they dropped.

Calling `ExpireEventsBefore` twice with the same cursor is safe — the
boundary survives its own expiry, so the second call drops 0. A cursor
the buffer doesn't hold at all is an error (`404 cursor_not_found`), so
a mistyped offset fails at the expire call rather than at the reconnect
several steps later.

```go
auth0mocktest.MustPush(t, c, `{"type":"user.created","offset":"1", ...}`)
auth0mocktest.MustPush(t, c, `{"type":"user.created","offset":"2", ...}`)

// Offset 1 is now unresumable; offset 2 still replays what follows it.
expired, err := c.Events.ExpireEventsBefore(ctx, "2")
require.NoError(t, err)
require.Equal(t, 1, expired)
```

Prefer this over pushing past `EVENTS_REPLAY_BUFFER` to force natural
eviction (slow, and couples the test to the buffer's capacity) or over
a full reset (which drops every other store and disconnects
subscribers). Expiry touches the replay buffer only: subscribers that
are already streaming keep receiving events.

**Common errors** the mock returns:

| Status | `errorCode` | Cause |
|---|---|---|
| 400 | `invalid_event` | Schema violation. The `message` field lists each failed `/json/pointer: reason` on a single line. |
| 400 | `invalid_from_timestamp` | `?from_timestamp` isn't valid RFC 3339, or was supplied empty. |
| 400 | `invalid_from` | `?from` was supplied empty. Omit the parameter to join live — an empty value is refused rather than read as "no cursor", because a client templating `?from=${cursor}` with an unset variable would otherwise get a `200` and silently miss everything buffered since its cursor. |
| 400 | `invalid_event_type` | `?event_type` was supplied empty (on its own or alongside real types). Omit it to receive every event. An empty value used to subscribe the stream to a topic nothing publishes to, so the connection succeeded and then never delivered an event. |
| 400 | `invalid_before` | `POST /admin0/events/expire?before=` was present but empty. Omit the parameter to expire the whole buffer — an empty value is refused rather than treated as expire-everything. |
| 400 | `invalid_query` | The query string wouldn't parse — a stray `%` that isn't a valid escape, or an unencoded `;`, which Go refuses as a query separator. Percent-encode either (`%25`, `%3B`) if a cursor genuinely contains one; the `message` field carries the parser's own reason. On `GET /api/v2/events` one bad pair rejects the whole request, because Go keeps the pairs it could parse and drops the one it couldn't: a `?from` that vanished would join live instead of 410-ing, and a vanished `?event_type` would turn a filtered subscription into a firehose. Also covers a repeated `?from` / `?from_timestamp`, which would silently resolve to the first value and resume the caller from a cursor it didn't ask for. On `POST /admin0/events/expire` it additionally covers a parameter other than `before` and a repeated `before`, since omitting `before` means "expire everything" and a typo can't be allowed to look like omission. |
| 404 | `cursor_not_found` | `POST /admin0/events/expire?before=` named a cursor the replay buffer doesn't hold — mistyped, already evicted, or the mock runs with `EVENTS_REPLAY_BUFFER=0`. Reported rather than folded into a `200` with `expired: 0`, which couldn't say whether nothing was older or the cursor was never there. |
| 410 | `event_aged_out` | `Last-Event-ID` / `?from` references an event the ring buffer no longer holds — through natural eviction or `POST /admin0/events/expire`. A `?from_timestamp` resolves against the buffer instead, so it joins live rather than 410-ing. |
| 401 | _bearer envelope_ | No / invalid bearer on `/api/v2/events`. |

## Use a Go test that boots the mock in-process

For stub registration, claim injection, permission setup, and clock control from Go test code, use [`pkg/auth0mock`](../pkg/auth0mock) — a typed client over `/admin0/*` that removes the JSON-marshalling boilerplate. See the [Go SDK section of the README](../README.md#-go-sdk) for the API surface and [`examples/sdk/`](../examples/sdk/) for a worked end-to-end walk-through.

For booting the mock itself: subprocess from `TestMain` (see [`examples/consumer/main.go`](../examples/consumer/main.go)) or in-process on a random port (see [`features/scenario/context.go`](../features/scenario/context.go), the pattern the godog suite uses).

## Trusting the self-signed cert

After running with `TLS_CACHE_DIR=/data/tls` so the cert is stable across reboots:

```bash
# Export from a running server (or read from $TLS_CACHE_DIR/tls.crt):
openssl s_client -connect localhost:8443 -showcerts </dev/null 2>/dev/null \
  | openssl x509 -outform pem > /tmp/auth0-mock.crt

# macOS
sudo security add-trusted-cert -d -r trustRoot \
  -k /Library/Keychains/System.keychain /tmp/auth0-mock.crt

# Debian / Ubuntu
sudo cp /tmp/auth0-mock.crt /usr/local/share/ca-certificates/auth0-mock.crt
sudo update-ca-certificates

# Arch / Fedora
sudo trust anchor /tmp/auth0-mock.crt
```

After this, `curl https://localhost:8443/...` works without `-k`, browsers stop nagging, and Go clients trust the cert via the system root pool. Combined with `TLS_CACHE_DIR`, trust persists across container restarts.

---

## See also

- [README.md](../README.md): top-level overview
- [docs/ARCHITECTURE.md](ARCHITECTURE.md): internals
- [CONTRIBUTING.md](../CONTRIBUTING.md): adding new functionality
