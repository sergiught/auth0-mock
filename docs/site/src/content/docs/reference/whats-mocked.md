---
title: "What's mocked"
description: "What the mock implements: the Authentication API, supported OAuth grants, the Management API, event streams, the admin surface and operations endpoints."
sidebar:
  order: 1
---

## Authentication API (hand-coded, fully functional)

Every route answers CORS preflights and echoes the caller's `Origin` (see `CORS_ALLOWED_ORIGINS`).

| Endpoint | Method | Notes |
|---|---|---|
| `/oauth/token` | POST | All Auth0 grants (see table below) |
| `/oauth/revoke` | POST | 200 no-op; revoked refresh tokens keep working |
| `/authorize` | GET | 302 to `redirect_uri` (or 200 auto-submitting form for `response_mode=form_post`) with `code`, implicit tokens (`access_token`, `id_token`) or both for any `response_type` combination of `code` / `token` / `id_token` (fragment unless plain `code`), or an OAuth `error` (bad `code_challenge`, `unsupported_response_type`, `invalid_request` for a missing `client_id` on token types or a missing `nonce` with `id_token`, or an `/admin0/authorize-errors` rule); `prompt=none` answers from the `auth0` session cookie (or `login_required`), `response_mode=web_message` returns a 200 postMessage page; sets the session cookie on success; stashes PKCE challenge if present |
| `/userinfo` | GET | Returns claims from the bearer (a JWT, or an opaque access token until it expires or `/admin0/reset`), plus the logged-in user's profile (from their last login; token claims win) |
| `/v2/logout` | GET | 302 to `returnTo`; ends the login session and expires the `auth0` cookie |
| `/oidc/logout` | GET, POST | OIDC logout (also ends the login session and expires the `auth0` cookie): checks `id_token_hint` (expired is fine; access tokens and other clients' tokens get 400), 302 to `post_logout_redirect_uri` with `state` (needs `client_id` or `id_token_hint`), or a 200 "logged out" page; discovery's `end_session_endpoint`; tokens from before a restart fail the signature check unless `SIGNING_KEY_FILE` is set |
| `/.well-known/jwks.json` | GET | Real JWKS for the in-process signing key |
| `/.well-known/openid-configuration` | GET | OIDC discovery rooted at the configured issuer; lists only the grants, claims and auth methods the mock serves |
| `/dbconnections/signup` | POST | Returns `{_id, email, email_verified:false}` |
| `/dbconnections/change_password` | POST | Returns the canned reset-email message |
| `/passwordless/start` | POST | Returns `{_id, email, phone_number}` |
| `/passwordless/verify` | POST | Same as the passwordless grant on `/oauth/token` |

## OAuth grants supported

| `grant_type` | Notes |
|---|---|
| `client_credentials` | M2M flow, returns `access_token` only; `scope` is the requested one, else the audience's `/admin0/permissions` joined by spaces (omitted when none); 403 `access_denied` when there is no `audience` and `DEFAULT_AUDIENCE` is blank |
| `password` | Returns access + id + refresh; gates on the MFA flag |
| `refresh_token` | A refresh token the mock issued returns the same user: new `access_token` and `id_token` (same profile and `auth_time`) plus a new `refresh_token`; the old one keeps working. `audience`/`scope` default to the login's. Unknown tokens still work and get just a new `access_token` (subject `<client_id>@refresh`) |
| `authorization_code` | Returns access + id; **enforces PKCE** if challenge was set at `/authorize`; the `audience` and `scope` sent to `/authorize` win over the token request's; the `nonce` sent to `/authorize` is echoed in the id_token, and id_tokens carry `auth_time` and `azp`; the user is the `login_hint` from `/authorize` or `auth0\|mock-user`, and id_tokens carry a profile derived from it (`email`, `name`, `nickname`, `picture`) |
| `http://auth0.com/oauth/grant-type/password-realm` | Auth0 Native SDKs; same as password + `realm` field threaded into claims |
| `http://auth0.com/oauth/grant-type/passwordless/otp` | Needs `client_id`, `username`, `otp=000000` and `realm` (`email` or `sms`); returns access + id + refresh for an Auth0-style user (`email\|…` / `sms\|…`, stable per username) with an email or phone profile; gates on the MFA flag |
| `http://auth0.com/oauth/grant-type/mfa-otp` | Step 2 of MFA dance; accepts `otp=123456` |
| `http://auth0.com/oauth/grant-type/mfa-oob` | Push/SMS step-up; accepts `binding_code=123456` |
| `http://auth0.com/oauth/grant-type/mfa-recovery-code` | Recovery flow; accepts `recovery_code=ABCDEFGHIJKLMNOP` |

Client credentials may also be sent via HTTP Basic (`client_secret_basic`); if both are sent, body values win.

:::caution
**Audience is echoed, not enforced.** The mock mints tokens with whatever `audience` you ask for (falling back to `DEFAULT_AUDIENCE`) and the bearer middleware verifies signature + expiry + issuer but *not* that the audience matches anything client-side. This is deliberate — tests need to swap audiences freely. Real Auth0 does enforce audience against the client's registered APIs; if your downstream service relies on `aud` checks, you'll need to add your own assertion in test fixtures.
:::

## Management API (spec-driven, ~480 endpoints)

Every operation in the embedded Auth0 Management API skeleton is mounted. Default response is `404 no_match`. Tests register stubs:

```bash
# Concrete-id stub
curl -X POST http://localhost:8080/admin0/expectations \
  -H 'Content-Type: application/json' \
  -d '{"method":"GET","path":"/api/v2/users/auth0|123","response":{"status":200,"body":{"user_id":"auth0|123","email":"alice@x"}}}'

# Template stub (catch-all for any user id)
curl -X POST http://localhost:8080/admin0/expectations \
  -H 'Content-Type: application/json' \
  -d '{"method":"GET","path":"/api/v2/users/{id}","response":{"status":200,"body":{"user_id":"auth0|*","email":"any@x"}}}'
```

:::note
Concrete-path stubs win over template stubs at request time. The optional `request` matcher (subset-matched `query` + `body`) lets you register multiple responses per operation; resolution is 4-tier (exact-path beats template-path; within a path, a request-matched expectation beats a catch-all; newest wins within a tier). `response.body` is validated against the operation's response schema at registration time. Invalid bodies are rejected with `400 invalid_match`, unknown operations with `400 unknown_operation`, unparseable or incomplete requests with `400 invalid_body`, and invalid `request` matcher fields (unknown fields, mistyped values, unknown query parameters) with `400 invalid_request_match`.
:::

## Event streams

`GET /api/v2/events` is a real Server-Sent Events endpoint. Tests push events through `POST /admin0/events`; every connected subscriber sees them in real time. The mock keeps a bounded replay buffer (default 100 events, configurable via `EVENTS_REPLAY_BUFFER`) so reconnecting subscribers can resume via `Last-Event-ID`, `?from=<offset>`, or `?from_timestamp=<rfc3339>`, and the buffer fills in what they missed.

```bash
# In one terminal: subscribe (bearer required).
TOKEN=$(curl -sX POST http://localhost:8080/oauth/token \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'grant_type=client_credentials&client_id=t&client_secret=t&audience=https://localhost:8443/api/v2/' \
  | jq -r .access_token)
curl -N -H "Authorization: Bearer $TOKEN" \
     'http://localhost:8080/api/v2/events?event_type=user.created'

# In another: push.
curl -X POST http://localhost:8080/admin0/events \
  -H 'Content-Type: application/json' \
  -d '{
    "type":"user.created","offset":"0",
    "event":{
      "specversion":"1.0","type":"user.created","source":"https://auth0.local/",
      "id":"evt_aaaaaaaaaaaaaaaa000000","time":"2026-05-19T00:00:00Z",
      "a0tenant":"my-tenant","a0stream":"est_aaaaaaaaaaaaaaaa000000",
      "data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}
    }
  }'
```

On connect the stream opens with a `:connected` readiness comment and a `retry:` reconnect hint (`3000` ms by default, configurable via `EVENTS_RECONNECT_HINT`) — matching Auth0's Events API. The subscriber then receives `id: 0 / event: user.created / data: {...}` — the SSE `id:` is the event's `offset` (the resume cursor you echo back via `Last-Event-ID` / `?from`), matching Auth0's Events API, not the CloudEvent `event.id`. Comment frames (`:keep-alive`) arrive every 15s so reverse-proxy idle timeouts don't drop the connection.

Push an **error frame** (`{"type":"error","error":{"code":"cursor_expired",...}}`) to simulate an in-band stream error — useful for testing a consumer's reconnect / cursor-fallback paths. Matching Auth0, error frames are control signals: delivered to **every** subscriber regardless of `event_type` filter, never stored in the replay buffer (they carry no offset id, so they're never replayed), and the stream **closes** right after, which triggers the consumer's reconnect.

Push an **offset-only marker** (`{"type":"offset-only","offset":"42"}`) to advance the cursor during idle without delivering an event. Also a control frame (reaches every subscriber regardless of filter), but unlike an error it carries an offset, so it **is** buffered and `from=42` is a valid resume point.

Errors are deliberately specific: schema violations → `400 invalid_event` with a one-line `"/json/pointer": reason` list; unknown `?from_timestamp` → `400 invalid_from_timestamp`; a query string that won't unescape → `400 invalid_query` rather than a `200` that quietly dropped the parameter it choked on; an empty `?from` / `?event_type` / `Last-Event-ID` → `400 invalid_from` / `400 invalid_event_type` / `400 invalid_last_event_id` rather than a `200` that silently joins live or subscribes to nothing; aged-out `Last-Event-ID` → `410 event_aged_out` (matches the `410` in Auth0's OpenAPI), which is also the answer when replay is disabled entirely, since there is no buffer that could hold the cursor.

To test how a consumer handles **unrecoverable cursor loss**, `POST /admin0/events/expire` ages out the replay buffer on demand, so the next reconnect presenting an expired cursor gets that `410 event_aged_out` — no need to push past `EVENTS_REPLAY_BUFFER` to force natural eviction. Pass `?before=<offset>` to expire only what is older than that cursor, leaving it and everything after it replayable, which models partial loss. It replies `{"expired":N}` with the number of cursors dropped; repeating a `?before` call is safe, since the boundary cursor survives its own expiry and the second call drops `0`. A cursor the buffer doesn't hold is `404 cursor_not_found` rather than a `200` that expired nothing, so a mistyped offset fails at the expire call instead of at the reconnect that then came back `200` where you asserted `410`. Unlike `/admin0/reset` it is scoped to the replay buffer: subscribers that are already streaming keep receiving events, and no other mock state is touched.

```bash
# Everything buffered so far is now unresumable.
curl -X POST http://localhost:8080/admin0/events/expire
# Or: keep offset 42 and newer, age out the rest.
curl -X POST "http://localhost:8080/admin0/events/expire?before=42"
```

To set expectations and verify connection lifecycle, `GET /admin0/events/subscribers` reports `{"active":N,"total":M}` — `active` is how many subscribers are connected right now, `total` how many have connected since the last `/admin0/reset` (handy for asserting reconnection behaviour). `active` is eventually-consistent: the mock removes a subscriber only when it observes the connection close, which can lag a client's disconnect by a few milliseconds. So to assert "my stream closed cleanly", poll until `active` settles rather than reading it immediately:

```bash
# After closing the consumer, wait for the count to drain to 0.
until [ "$(curl -s http://localhost:8080/admin0/events/subscribers | jq .active)" = 0 ]; do sleep 0.05; done
```

From Go tests, skip the hand-rolled loop: `c.Events.Subscribers(ctx)` returns the typed `{Active, Total}` counts, and `auth0mocktest.WaitForActiveSubscribers(t, c, want, timeout)` polls until `active` settles (or fatals on timeout).

:::note
The mock's `WRITE_TIMEOUT` (default 30s) is automatically bypassed for `/api/v2/events` — long-lived subscribers won't be torn down by the server-side deadline. If a reverse proxy fronts the mock (nginx, Envoy, …), disable response buffering for this endpoint (`proxy_buffering off;` for nginx) so frames reach the client live rather than queuing in the proxy until the connection closes. `/api/v2/events` sends CORS headers like every other route, but the browser's native `EventSource` cannot send an `Authorization` header and the stream needs a bearer, so use a fetch-based SSE client from a browser.
:::

See [Event streams guide → Drive an event-stream consumer from a test](/auth0-mock/guides/event-streams/#drive-an-event-stream-consumer-from-a-test) for the SDK-driven workflow.

## Admin surface (no auth, JSON-driven)

| Endpoint | Method | Purpose |
|---|---|---|
| `/admin0/reset` | POST | Wipe everything: expectations, claims, claim mappings, authorize error rules, permissions, MFA flag, remembered users and refresh tokens, clock, SSE events hub, request journal |
| `/admin0/reset?keep_seed=false` | POST | From release 0.234.0. Also drop the [seed file](/auth0-mock/guides/seed-file/) state; a plain reset re-applies the seed. Other values get a 400 |
| `/admin0/export` | GET | From release 0.234.0. Dump expectations, claims, claim mappings, permissions and the MFA flag as a seed file. `?format=yaml` returns YAML; `?detail=true` adds `id` and `hits`. See [Seed file](/auth0-mock/guides/seed-file/) |
| `/admin0/expectations` | POST / GET / DELETE | Register, list, and clear canned Management API responses; an optional `then` list answers successive calls in sequence ([cookbook](/auth0-mock/guides/stubbing/#test-retries-429-then-200)) |
| `/admin0/journal` | GET / DELETE | List (`?method=`, `?path=`; a `{template}` path matches by operation) or clear the journal of `/api/v2/*` calls the mock received: method, path, redacted headers, body, status, `matched` / `no_match` / `rejected` outcome. Independent store: `DELETE /admin0/expectations` doesn't touch it; `/admin0/reset` clears it |
| `/admin0/journal/unmatched` | GET | Only the calls no stub matched, each with up to 3 `nearMisses` showing which fields differed |
| `/admin0/claims` | GET / PUT / DELETE | Custom claims merged into every minted JWT |
| `/admin0/claims/mappings` | GET / PUT / DELETE | Request-parameter → claim projection: map a token-request body parameter (form or JSON) to a claim name and `/oauth/token` stamps that parameter's value into the minted access token — per request, overriding `/admin0/claims` for that key. Allowlist semantics; empty map (default) = off. Independent store: `DELETE /admin0/claims` doesn't touch it |
| `/admin0/authorize-errors` | POST / GET / DELETE | Make /authorize fail on purpose: rules with an optional match on /authorize query params return an OAuth error to redirect_uri; newest match wins; DELETE /admin0/authorize-errors/{id} removes one |
| `/admin0/permissions` | GET / DELETE | All audiences and their permissions |
| `/admin0/permissions/{audience}` | GET / PUT / DELETE | Per-audience RBAC injection (audience may be a URL, chi wildcard) |
| `/admin0/mfa-required` | GET / PUT | Toggle MFA enforcement at runtime |
| `/admin0/clock` | GET / PUT / DELETE | Freeze the clock at an instant (`{"now":"..."}`), set an offset (`{"offset":"25h"}`), or restore real time. Drives JWT `iat`/`exp` and bearer validation. |
| `/admin0/clock/advance` | POST | Step the held clock by a Go duration (`{"by":"25h"}`). Negative durations allowed. |
| `/admin0/events` | POST | Push an Auth0 event-stream envelope onto `GET /api/v2/events` so consumer SDKs see it live. Body is the full envelope `{type, offset, event:{...CloudEvent}}` — validated against the OpenAPI `text/event-stream` schema before fan-out. See [Event streams](#event-streams). |
| `/admin0/events/subscribers` | GET | Observe the SSE endpoint: `{"active":N,"total":M}` — subscribers connected right now, and total connected since the last `/admin0/reset`. See [Event streams](#event-streams). |
| `/admin0/events/expire` | POST | Age out replay cursors so a reconnect gets `410 event_aged_out`. `?before=<offset>` expires only what is older than that cursor. Leaves live subscribers and every other store alone. See [Event streams](#event-streams). |

:::caution
**`/admin0/*` is unauthenticated by design** so test setup needs zero token plumbing. Never expose it to an untrusted network. Bind the mock to `127.0.0.1` (the default), keep it inside your CI runner / dev container, or front it with your own auth if you must reach it across a network boundary. The mock won't start on an address other machines can reach unless `ADMIN_ALLOW_REMOTE=true`, and logs a warning when it does.
:::

## Operations

| Endpoint | Notes |
|---|---|
| `/healthz` | Kubernetes-style liveness probe — `200 {"status":"ok"}` if the process is up. No auth. |
| `/readyz`  | Kubernetes-style readiness probe — `200 {"status":"ready"}` once the JWKS signing key is materialised. The mock's only init dependency (RSA keygen) is synchronous and runs before the listener accepts, so today this is functionally equivalent to `/healthz`; the endpoint is exposed for orchestrator-convention parity (liveness vs readiness probe separation) and to leave room if future init grows. No auth. |
