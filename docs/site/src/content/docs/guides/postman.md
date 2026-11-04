---
title: Calling the API from Postman or Insomnia
description: Import the merged OpenAPI document into Postman or Insomnia, and regenerate it after spec changes.
sidebar:
  order: 6
---

:::tip
**Prefer a browser?** Run the mock and open
`http://localhost:8080/docs` for an interactive
reference rendered by [Scalar](https://github.com/scalar/scalar). Every
endpoint is clickable, and "Try it" points at the same instance that served
the page.
:::

The mock ships a merged OpenAPI 3.1 document that covers every HTTP surface
it exposes:

- The Auth0 **Authentication API** (`/oauth/token`, `/authorize`,
  `/userinfo`, `/v2/logout`, `/oidc/logout`, `/dbconnections/*`, `/passwordless/*`).
- The Auth0 **Management API** (everything under `/api/v2`). Canned responses
  are programmed centrally via `POST /admin0/expectations`.
- The **admin0** control plane (`/admin0/*`).
- The **service** endpoints (`/healthz`, `/.well-known/jwks.json`,
  `/openapi.json`, `/openapi.yaml`).

### Silent login

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

### Importing

- **From the repo:** import `api/auth0-mock.openapi.json` directly.
- **From a running instance:** point your client at
  `http://localhost:8080/openapi.json` (or `/openapi.yaml`).

Both Postman and Insomnia will create a folder per tag (`auth-api`,
`admin0`, `service`, plus the Management API's existing tags) and
fill in request bodies from the schemas.

### Regenerating the spec

The merged JSON is committed and checked for drift in CI. Re-run
`make openapi` after editing any of the auth0-mock-authored fragments:

- `internal/authapi/authapi.openapi.yaml`
- `internal/admin0/admin0.openapi.yaml`
- `internal/router/service.openapi.yaml`

`api/auth0-management-api.openapi.json` is a generated skeleton, not a
hand-edited file. To pull in a newer Auth0 Management API spec, run
`make refresh-spec` (see [CONTRIBUTING.md](https://github.com/sergiught/auth0-mock/blob/main/CONTRIBUTING.md)).
