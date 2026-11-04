---
title: First request
description: Mint a token, stub a Management API response and call it, using only curl.
sidebar:
  order: 2
---


```bash
# 1. Mint a real signed access token
TOKEN=$(curl -s -X POST http://localhost:8080/oauth/token \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'grant_type=client_credentials&client_id=demo&client_secret=x&audience=http://localhost:8080/api/v2/' \
  | jq -r .access_token)

# 2. Stub a Management API response
curl -X POST http://localhost:8080/admin0/expectations \
  -H 'Content-Type: application/json' \
  -d '{"method":"GET","path":"/api/v2/users/auth0|123","response":{"status":200,"body":{"user_id":"auth0|123","email":"alice@example.com"}}}'

# 3. Call the stubbed endpoint with your bearer
curl http://localhost:8080/api/v2/users/auth0%7C123 \
  -H "Authorization: Bearer ${TOKEN}"
# => {"user_id":"auth0|123","email":"alice@example.com"}
```

Your code calls auth0-mock the same way it calls Auth0. No SDK shims, no monkey-patching.

