---
title: Login flows
description: Test PKCE, login errors, silent login, MFA challenges and the password-realm grant.
sidebar:
  order: 3
---

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

## Test login errors

Make `/authorize` send the user back to your app with an OAuth error, so you can test cancelled logins and failed silent auth.

```bash
curl -s -X POST http://localhost:8080/admin0/authorize-errors \
  -H 'Content-Type: application/json' \
  -d '{"match":{"client_id":"my-app"},"error":"access_denied","error_description":"User cancelled login"}'
```

Matching `/authorize` requests now redirect to:

```
https://app/cb?error=access_denied&error_description=User+cancelled+login&state=...
```

The error is delivered per `response_mode`: in the query by default for `response_type=code`, in the fragment for every other `response_type` (anything but plain `code`), as an auto-submitting form for `form_post`, and as a postMessage page for `web_message`. Problems with `redirect_uri` itself still return a 400 JSON error with no redirect, since there is nowhere safe to redirect.

From Go:

```go
rule, err := c.AuthorizeErrors.Add(ctx, auth0mock.AuthorizeError{
	Match:            map[string]string{"client_id": "my-app"},
	Error:            "access_denied",
	ErrorDescription: "User cancelled login",
})
if err != nil {
	t.Fatal(err)
}
t.Cleanup(func() { _ = c.AuthorizeErrors.Delete(context.Background(), rule.ID) })
```

To force a failure that a real session would not produce, match `prompt=none` with `login_required` or `consent_required`. A rule wins over a live session. `DELETE /admin0/authorize-errors` (or `/admin0/reset`) clears all rules.

## Organizations

Pass `organization` to `/authorize` and the tokens carry `org_id` (and `org_name`). No organizations are stored; the claims come from the request.

```bash
# A name: org_name is "acme", org_id is derived from it
curl -s -o /dev/null -D - 'http://localhost:8080/authorize?client_id=my-app&redirect_uri=https://app/cb&response_type=code&audience=https://api.example.com&organization=acme'
```

- **Id or name.** A value starting with `org_` is an id and is used as is: you get `org_id` only. Any other value is a name, lowercased: you get `org_name` and an `org_id` derived from the name (`acme` becomes `org_822b33ad87c148a0`). The same name always gives the same id, but it will not match the id of an organization in a real tenant. Real Auth0 only accepts names when the "Allow Organization Names in Authentication API" tenant setting is on; the mock always accepts them. A blank value adds nothing.
- **Which flows.** Code, implicit, hybrid and `prompt=none` on `/authorize`, and `client_credentials` (send `organization` in the form or JSON body). The claims go on the access token, and on the ID token when the flow returns one. Opaque access tokens (no `audience`) carry no claims.
- **Code exchange and refresh keep the org.** The organization is fixed at `/authorize`. An `organization` sent on the `/oauth/token` code request or on a refresh request is ignored, and every refreshed token keeps the original org, with or without `TOKEN_ENDPOINT_STRICT`.
- **Password-style grants ignore it.** `password`, password-realm, passwordless and the `mfa-*` grants add no org claims and return no error, since Auth0 does not support Organizations there. Use the [token exchange](#token-exchange) grant when you need an organization on an automated login.
- **`invitation`** is accepted and ignored. The login completes as if the invitation was accepted.
- **Org claims are overridable.** Global claims and claim mappings can override `org_id` and `org_name` on access tokens, like any other claim. See [Tokens and claims](/auth0-mock/guides/tokens-and-claims/).
- **Management API routes** for organizations are still stubs; creating an organization there does not register anything for logins.

To test "this user is not a member of the organization", add a [login error](#test-login-errors) rule. Rules match any subset of the `/authorize` query parameters:

```bash
curl -s -X POST http://localhost:8080/admin0/authorize-errors \
  -H 'Content-Type: application/json' \
  -d '{"match":{"organization":"org_abc123","login_hint":"bob@example.com"},"error":"access_denied","error_description":"user bob@example.com is not a member of org_abc123"}'
```

Now `/authorize?...&organization=org_abc123&login_hint=bob@example.com` redirects with `error=access_denied`, while the same request for `alice@example.com` logs in. Auth0 does not document the exact error text, so use whatever your app expects.

## Token exchange

The token-exchange grant (`urn:ietf:params:oauth:grant-type:token-exchange`) mints tokens for a user you name, with no password or browser. It is the mock's version of Auth0's Custom Token Exchange. Send a form or JSON body to `/oauth/token`:

- `subject_token` (required): names the user.
- `subject_token_type` (required): a `https://` or `urn:` URI of your choosing, compared case-insensitively. URIs in the reserved `auth0.com`, `okta.com` (`http://` or `https://`), `urn:ietf`, `urn:auth0` and `urn:okta` namespaces are rejected.
- `client_id`, `audience`, `scope`, `organization`: as for the other grants. `actor_token` and `requested_token_type` are ignored.

**The subject token is not verified.** If it is a JWT (three non-empty dot-separated parts, header a JSON object), the mock reads the payload and never checks the signature, so any caller can name any user. `sub` is required and becomes the user id; `email`, `email_verified`, `name`, `nickname`, `picture`, `phone_number` and `phone_number_verified` go to the ID token. A JWT whose `exp` is in the past is rejected. Any other string is used whole as the user id, with the profile derived as for the other grants. Org claims inside the subject token are ignored.

```bash
curl -s -X POST http://localhost:8080/oauth/token \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'grant_type=urn:ietf:params:oauth:grant-type:token-exchange&client_id=demo&subject_token=auth0|jane&subject_token_type=https://example.com/token-types/legacy&audience=https://api.example.com&scope=openid email offline_access'
# {"access_token":"...","id_token":"...","refresh_token":"...",
#  "issued_token_type":"urn:ietf:params:oauth:token-type:access_token", ...}
```

The response has the same shape as a password grant plus `issued_token_type`. The access token carries `gty=token-exchange`. There is no MFA step and no `act` claim. A `refresh_token` follows the usual `offline_access` rules (in [strict mode](#test-strict-token-handling) it needs `offline_access`), and refreshing keeps the organization.

Failures return `400 invalid_request`:

```json
{"error":"invalid_request","error_description":"Invalid subject token"}
```

Other descriptions are `Missing required parameter: subject_token`, `Missing required parameter: subject_token_type`, and `subject_token_type must be a https:// or urn: URI outside the reserved auth0, okta and ietf namespaces`. An expired JWT is the easy way to test your failure path.

### Organizations and automated logins

Send `organization` to add `org_id` (and `org_name` for a name) to the tokens, as for `client_credentials`: `organization=acme` gives `org_name` `acme` and `org_id` `org_822b33ad87c148a0`. This is the way to get an organization onto a login with no browser. See [Organizations](#organizations).

## Test silent login

The mock keeps a real login session. A successful `/authorize` sets an `auth0` cookie, and a later `prompt=none` request answers from it. With no session the answer is `login_required`. This is the flow `getTokenSilently` in auth0-spa-js uses:

```js
const auth0 = await createAuth0Client({
  domain: 'localhost:8443',
  clientId: 'my-app',
  authorizationParams: { redirect_uri: window.location.origin },
});

await auth0.loginWithPopup(); // sets the session cookie
const token = await auth0.getTokenSilently({ cacheMode: 'off' }); // prompt=none in a hidden iframe
```

Sign in as a specific user with `login_hint`. `/v2/logout`, `/oidc/logout` and `/admin0/reset` end the session, so the next silent request gets `login_required`. `auth_time` stays at the original login instant, so you can freeze and advance the [clock](/auth0-mock/guides/tokens-and-claims/#test-token-expiry-without-sleeps) to test re-authentication windows.

The hidden iframe is cross-site when your app and the mock are on different sites, and browsers only send the cookie there if it is `SameSite=None; Secure`. The mock sets that only over HTTPS, so run it on HTTPS for cross-site iframes (see [Run against HTTPS with a trusted cert](/auth0-mock/guides/https-tls/#run-against-https-with-a-trusted-cert)). Over plain HTTP the cookie is `SameSite=Lax`, which works for same-site setups and for HTTP clients with a cookie jar.

Two more caveats. First, auth0-spa-js always uses `https://<domain>`, so an `http://localhost` app is cross-site with the mock by scheme, and even `SameSite=None; Secure` only works where third-party cookies are allowed. Safari, Firefox with Total Cookie Protection and Chrome with third-party cookies blocked do not send it, so they get `login_required`, as with real Auth0 without a custom domain. Serve the app on `https://localhost` to be same-site with the mock. Second, behind a TLS-terminating proxy the mock sees plain HTTP, so the cookie is `SameSite=Lax`.

### How silent login works

A successful `/authorize` sets an `auth0` session cookie (`HttpOnly`,
`Path=/`; `SameSite=Lax` over HTTP, `SameSite=None; Secure` over HTTPS).
`prompt=none` then answers from that session, or returns `login_required`
when there is none, and `response_mode=web_message` delivers the result with
`postMessage`, so `getTokenSilently` works against the mock. `/v2/logout`,
`/oidc/logout` and `/admin0/reset` end the session.

Behaviour change for clients that keep cookies (browsers, cookie jars): a
second `/authorize` without `login_hint` now returns the earlier user, not
`auth0|mock-user`. Use `prompt=login` or a `login_hint` to pick a user.

Also, `prompt=none` without a live session now returns `login_required`.
Before, `/authorize` ignored `prompt`, so `prompt=none` always succeeded as
`auth0|mock-user` (or the `login_hint` user). Log in first in the same cookie
jar, or drop `prompt=none`, to get tokens.

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

### Per-user MFA

Set MFA for one user, no matter what the global switch says. The key is the `username` the password grant receives (or the subject of the user).

```bash
curl -X PUT http://localhost:8080/admin0/mfa/users/alice@example.com \
  -H 'Content-Type: application/json' \
  -d '{"required":true,"authenticators":[{"type":"otp"},{"type":"oob","oob_channel":"sms"}]}'
```

The response is the stored config. `GET /admin0/mfa/users/{user}` returns `404 unknown_user` when there is none. Authenticators are active unless you send `"active":false`. An inactive authenticator is confirmed by the matching mfa grant (see "Enroll and manage authenticators").

A per-user `required` beats the global `/admin0/mfa-required` switch in both directions. `"required":true` forces MFA for that user while the global switch is off. `"required":false` exempts that user while it is on. Users with no config follow the global switch. The user key is case-sensitive, so `Alice@example.com` and `alice@example.com` are different users.

A user with any per-user config is a strict user, even if the config only sets `required`. A user with no config follows the global switch and is a legacy user, which keeps the old behaviour:

| Grant | Legacy user | Strict user |
|---|---|---|
| `mfa-otp` | `otp=123456` | Needs an `otp` authenticator. An inactive one is confirmed (activated together with its group) by a correct `otp`. Otherwise `403 invalid_grant` "User does not have an active otp authenticator" |
| `mfa-oob` | Any `oob_code` plus `binding_code=123456` | The `oob_code` must come from `/mfa/challenge` or `/mfa/associate` for the same `mfa_token` |
| `mfa-recovery-code` | `ABCDEFGHIJKLMNOP` | Needs an active recovery code |

`POST /mfa/challenge` takes the `mfa_token` and returns the challenge for the user:

```bash
curl -s -X POST http://localhost:8080/mfa/challenge \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d "mfa_token=${MFA_TOKEN}&client_id=demo&challenge_type=oob"
# {"challenge_type":"oob","oob_code":"...","binding_method":"prompt"}
```

A strict user with no active otp or oob authenticator gets `400 association_required`.

#### Enroll and manage authenticators

A client holding an `mfa_token` can enroll a factor with `POST /mfa/associate`. Send `Authorization: Bearer <mfa_token>` and a JSON or form body with `authenticator_types` (`otp` or `oob`), plus `oob_channels` (`sms`, `voice`, `email`, `auth0`) for oob, `phone_number` (E.164, such as `+15551234567`) for sms and voice, and `email` for the email channel.

```bash
curl -s -X POST http://localhost:8080/mfa/associate \
  -H "Authorization: Bearer ${MFA_TOKEN}" \
  -H 'Content-Type: application/json' \
  -d '{"authenticator_types":["otp"]}'
# {"authenticator_type":"otp","secret":"JBSWY3DPEHPK3PXP","barcode_uri":"otpauth://totp/<tenant>:<user>?secret=JBSWY3DPEHPK3PXP&issuer=<tenant>&algorithm=SHA1&digits=6&period=30","recovery_codes":["ABCDEFGHIJKLMNOP"]}
```

`<tenant>` is the request host without the port, and `<user>` is the login. An oob enrollment returns an `oob_code`:

```bash
curl -s -X POST http://localhost:8080/mfa/associate \
  -H "Authorization: Bearer ${MFA_TOKEN}" \
  -H 'Content-Type: application/json' \
  -d '{"authenticator_types":["oob"],"oob_channels":["sms"],"phone_number":"+15555550100"}'
# {"authenticator_type":"oob","oob_channel":"sms","oob_code":"...","binding_method":"prompt","recovery_codes":["ABCDEFGHIJKLMNOP"]}
```

The email channel needs `email`. Push (`auth0`) has no `binding_method`, adds a `barcode_uri`, and creates a push and an otp authenticator together. `recovery_codes` appear only when the user had no recovery code yet, which is the first enrollment.

A user who already has an active authenticator gets `403 access_denied` "User is already enrolled.". A bad token gets `401 invalid_token`, and a bad type, channel, phone number or email gets `400 invalid_request`. Enrolling gives a legacy user a config, so later logins are checked strictly.

New authenticators stay inactive until confirmed. Send `mfa-otp` with `otp=123456`, or `mfa-oob` with the returned `oob_code` and `binding_code=123456` (none for push). That activates every authenticator enrolled together, and the user's recovery code.

`GET /mfa/authenticators` lists the user's authenticators as `{id, authenticator_type, active, oob_channel?, name?}`, including inactive ones. Authenticate with an `mfa_token`, or with an access token whose audience is `<issuer>mfa/` and whose scope includes `read:authenticators`. `DELETE /mfa/authenticators/{id}` needs such an access token with `remove:authenticators`. It returns `204`, or `404 not_found` "Authenticator not found" for an unknown id. Deleting a push authenticator also deletes its otp. An `mfa_token` on DELETE gets `401 invalid_token`, and a token without the scope gets `403 insufficient_scope`.

The access-token routes look the user up by the token's `sub`. For passwordless users, key the config by subject (`email|...`).

Not supported: `/mfa/associate` accepts only an `mfa_token`, so enrolling a second factor with an access token fails. Add it with the admin API instead. Push enrollment polling is not supported either.

From Go, use the SDK client:

```go
on := true
_, err := c.MFA.SetUser(ctx, "alice@example.com", auth0mock.MFAUser{
	Required:       &on,
	Authenticators: []auth0mock.MFAAuthenticator{{Type: "otp"}},
})
```

Seed the same config at startup with the `mfa_users` key. See the [seed file guide](/auth0-mock/guides/seed-file/).

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

## Test strict token handling

By default the mock is lenient. It accepts any authorization code and always issues a refresh token. Set `TOKEN_ENDPOINT_STRICT=true` to make `/oauth/token` behave like Auth0.

In strict mode:

- An authorization code is rejected if it is unknown, expired or already used.
- It is also rejected if `redirect_uri` is missing or differs from the one sent to `/authorize`, or if `client_id` differs.
- A missing `code` gets `400 invalid_request` with "Missing required parameter: code".
- On a code exchange, the scope sent to `/authorize` is used even when it is empty. The scope in the token request is ignored.
- A code is used up by the first request, even when that request fails.
- The password, password-realm, passwordless, authorization code and MFA grants issue a `refresh_token` only when the scope includes `offline_access`.

A rejected code returns `403`:

```json
{"error":"invalid_grant","error_description":"Invalid authorization code"}
```

Refresh tokens follow Auth0's rotation rules in strict mode:

- Every refresh rotates. The response has a new `refresh_token` and the old one is used up, whatever the scope.
- Using a used token again revokes the whole chain, so the newest token stops working too.
- A missing `refresh_token` gets `400 invalid_request` with "Missing required parameter: refresh_token".
- A refresh token the mock did not issue is rejected.
- A refresh with a different `client_id` is rejected but does not use up the token. A missing `client_id` is not a mismatch.
- `POST /oauth/revoke` with the `token` (form or JSON body) ends the whole chain. It always answers `200` with an empty body.

A rejected refresh token returns `403`:

```json
{"error":"invalid_grant","error_description":"Unknown or invalid refresh token."}
```

A client mismatch returns `403` with this description:

```json
{"error":"invalid_grant","error_description":"The client associated with this refresh token (demo) is different than the one sent in the request (other)."}
```

With strict mode off, `/oauth/revoke` does nothing and any refresh token is accepted.
