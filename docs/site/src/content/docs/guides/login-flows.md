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

Refresh tokens and `/oauth/revoke` stay lenient for now, even in strict mode.
