---
title: Stored data
description: Turn on MGMT_STORE so the mock remembers what you create through the Management API, instead of needing a stub for every call.
sidebar:
  order: 1.1
---

By default the Management API answers only what you stub. With `MGMT_STORE=true` the mock also remembers what your code creates. A role you POST can be read, listed, updated and deleted, with no stubs registered.

## Turn it on

```yaml
environment:
  MGMT_STORE: "true"
```

It is off by default, and nothing changes until you set it.

## What is stored

Most Management API collections are stored: lists such as `/api/v2/roles` and `/api/v2/users`, single items such as `/api/v2/roles/{id}`, collections under another object such as `/api/v2/organizations/{id}/invitations`, and settings objects such as `/api/v2/tenants/settings`.

Some paths stay stub-only. Examples are experimentation feature flags, segments and experiments, branding themes, branding phone providers, `/api/v2/organizations/{id}/client-grants`, `/api/v2/sessions/{id}` and `/api/v2/refresh-tokens/{id}`. The full list, path by path, is in the [classification file](https://github.com/sergiught/auth0-mock/blob/main/internal/mgmtstore/testdata/classification.golden). A path marked `stub` there behaves as before.

A nested collection needs its parent. Creating an invitation under an organization that does not exist gets a 404, as in Auth0.

## Ids, timestamps and paging

- A new object gets a random 24-character hexadecimal id, padded with zeros if the schema needs a longer one.
- `created_at` and `updated_at` are set when the Auth0 schema has them, and `updated_at` moves on every update.
- Lists support `page`, `per_page` and `include_totals`. Lists that Auth0 pages by checkpoint support `from` and `take`; the cursor is a plain number.
- Fields that Auth0 always returns, but that you never wrote, get a minimal placeholder. You may see empty strings or zero values for fields you never set.
- On a settings path, a PATCH that sets a field to `null` removes it.

## Which answer wins

For a call to `/api/v2`, the mock tries these in order:

1. Request validation, as always.
2. A stub registered with `"override": true`.
3. The store.
4. Your normal stubs.

So once the store is on, an ordinary stub on a stored path no longer answers. To force an error or a fixed answer, add `override` to the stub:

```bash
curl -X POST http://localhost:8080/admin0/expectations \
  -H 'Content-Type: application/json' \
  -d '{"method":"GET","path":"/api/v2/roles","override":true,
       "response":{"status":429,"body":{"statusCode":429,"error":"Too Many Requests","message":"slow down"}}}'
```

In the Go SDK, use `.Override()` on the builder. Delete the stub and the store answers again. `override` is ignored when the store is off. The journal marks calls the store answered with the outcome `stored`.

## Reset, seed and export

- Each [namespace](/auth0-mock/guides/parallel-tests/) has its own store. Two namespaces never see each other's objects.
- `POST /admin0/reset` re-applies the [seed file](/auth0-mock/guides/seed-file/) if there is one, and empties the store otherwise. With `keep_seed=false` the store ends empty.
- `GET /admin0/export` writes the stored data under a `management` key, only when there is some. Passwords are never included.

## Limit

Each stored collection holds at most 10000 objects. Past that, a create gets `429` with `mock store limit reached: 10000 objects in <path>`.

## Not in this release yet

These are planned:

- Auth0's own defaults and error wording for the main types (users, roles, organizations and similar).
- Links between objects, such as the roles of a user.
- Search.
- Logging in as a stored user.
