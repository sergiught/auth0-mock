---
title: Browser apps
description: Call the mock from a browser app.
sidebar:
  order: 7
---

## Call the mock from a browser app

Point the SPA SDK's `domain` at the mock (for example `localhost:8443`). Every route answers CORS preflights and echoes the caller's `Origin`, so there is nothing to configure by default.

The browser must trust the mock's HTTPS cert first (see [Run against HTTPS with a trusted cert](https://github.com/sergiught/auth0-mock/blob/main/docs/COOKBOOK.md#run-against-https-with-a-trusted-cert) and [Trusting the self-signed cert](https://github.com/sergiught/auth0-mock/blob/main/docs/COOKBOOK.md#trusting-the-self-signed-cert)); otherwise the SPA sees a "Failed to fetch" that looks like a CORS error. If the SPA points at the plain-HTTP listener instead, set `ISSUER_URL` to match, or id_token `iss` validation fails.

To test how your app behaves when its origin is blocked, restrict the allow-list:

```bash
CORS_ALLOWED_ORIGINS=http://localhost:3000,https://*.example.com auth0-mock
```

Requests from any other origin get no CORS headers, and their preflights get a 403.
