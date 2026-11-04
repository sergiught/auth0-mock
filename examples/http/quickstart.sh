#!/usr/bin/env bash
# curl quickstart: checks discovery, mints a token, stubs and calls the Management API.
set -euo pipefail

URL="${AUTH0_MOCK_URL:-https://localhost:8443/}"
CA="${AUTH0_MOCK_CA:-.auth0-mock/tls.crt}"
AUDIENCE="https://my-api.example.com"

fail() { echo "FAIL: $*" >&2; exit 1; }
call() { curl -sS --fail-with-body --cacert "$CA" "$@"; }

# 1. Discovery issuer must match the URL clients use.
issuer=$(call "${URL}.well-known/openid-configuration" | jq -r .issuer)
[ "$issuer" = "$URL" ] || fail "issuer mismatch: discovery says '$issuer', AUTH0_MOCK_URL is '$URL'"
echo "ok: discovery issuer is $issuer"

# 2. Mint a client_credentials token and check its claims.
token=$(call -X POST "${URL}oauth/token" \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d "grant_type=client_credentials&client_id=demo&client_secret=x&audience=${AUDIENCE}" \
  | jq -r .access_token)
payload=$(printf '%s' "$token" | cut -d. -f2 | tr '_-' '/+')
while [ $(( ${#payload} % 4 )) -ne 0 ]; do payload="${payload}="; done
claims=$(printf '%s' "$payload" | base64 -d)
aud=$(printf '%s' "$claims" | jq -r 'if (.aud|type)=="array" then .aud[0] else .aud end')
sub=$(printf '%s' "$claims" | jq -r .sub)
[ "$aud" = "$AUDIENCE" ] || fail "token aud is '$aud', expected '$AUDIENCE'"
[ "$sub" = "demo@clients" ] || fail "token sub is '$sub', expected 'demo@clients'"
echo "ok: token has aud=$aud sub=$sub"

# 3. Stub a Management API response, then call it with the token.
call -X POST "${URL}admin0/expectations" -H 'Content-Type: application/json' \
  -d '{"method":"GET","path":"/api/v2/users/auth0|123","response":{"status":200,"body":{"user_id":"auth0|123","email":"alice@example.com"}}}' >/dev/null
email=$(call "${URL}api/v2/users/auth0%7C123" -H "Authorization: Bearer ${token}" | jq -r .email)
[ "$email" = "alice@example.com" ] || fail "stubbed user email is '$email', expected 'alice@example.com'"
echo "ok: stubbed user returned $email"
