---
title: Common recipes
description: Short curl recipes for claims, permissions, MFA and the clock.
sidebar:
  order: 9
---

→ See the [tokens and claims](/auth0-mock/guides/tokens-and-claims/) and [login flows](/auth0-mock/guides/login-flows/) guides for the full versions. Highlights:

```bash
# Inject a custom claim into every token
curl -X PUT http://localhost:8080/admin0/claims \
  -H 'Content-Type: application/json' \
  -d '{"role":"admin","org_id":"o-42"}'

# Project a token-request parameter into the minted claims (per request,
# no global-state race): map it once, then every /oauth/token call that
# carries `resource` mints it under the namespaced claim
curl -X PUT http://localhost:8080/admin0/claims/mappings \
  -H 'Content-Type: application/json' \
  -d '{"resource":"https://example.com/resource"}'

# Set RBAC for an audience (URL-form audience works thanks to chi wildcard)
curl -X PUT http://localhost:8080/admin0/permissions/https://api.example.com/ \
  -H 'Content-Type: application/json' \
  -d '["read:users","write:users"]'

# Force MFA on the next password grant
curl -X PUT http://localhost:8080/admin0/mfa-required \
  -H 'Content-Type: application/json' \
  -d '{"required":true}'

# Freeze the clock so token iat/exp are deterministic
curl -X PUT http://localhost:8080/admin0/clock \
  -H 'Content-Type: application/json' \
  -d '{"now":"2030-01-01T00:00:00Z"}'

# Advance 7 days to simulate token expiry without sleeping
curl -X POST http://localhost:8080/admin0/clock/advance \
  -H 'Content-Type: application/json' \
  -d '{"by":"168h"}'

# Reset everything between tests
curl -X POST http://localhost:8080/admin0/reset
```
