# Cookbook

Practical recipes for using auth0-mock in tests. Each recipe is self-contained: copy, paste, adjust.

## 📑 Table of contents

- [Mint a token and call a stubbed Management API endpoint](#mint-a-token-and-call-a-stubbed-management-api-endpoint)
- [Stub multiple users at once](#stub-multiple-users-at-once)
- [Different responses for different requests](#different-responses-for-different-requests)
- [Test a code path that reads a specific `permissions` claim](#test-a-code-path-that-reads-a-specific-permissions-claim)
- [Inject a custom claim into every minted token](#inject-a-custom-claim-into-every-minted-token)
- [Test a PKCE flow end-to-end](#test-a-pkce-flow-end-to-end)
- [Test an MFA challenge flow](#test-an-mfa-challenge-flow)
- [Test the password-realm grant](#test-the-password-realm-grant)
- [Stub an error response (400, 429, 500)](#stub-an-error-response-400-429-500)
- [Reset state between tests](#reset-state-between-tests)
- [Inspect what's currently registered](#inspect-whats-currently-registered)
- [Run against HTTPS with a trusted cert](#run-against-https-with-a-trusted-cert)
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

## Use a Go test that boots the mock in-process

Until we ship a stable public Go API (planned), the simplest pattern is to start the binary as a subprocess in a `TestMain`. For a worked end-to-end example, see [`examples/consumer/main.go`](../examples/consumer/main.go).

For the in-process pattern used by our own godog suite, see [`features/scenario/context.go`](../features/scenario/context.go); that's the canonical reference for boot/teardown.

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
