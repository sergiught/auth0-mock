---
title: Node.js quickstart
description: Test a Node API and a Management API client against the mock with express-oauth2-jwt-bearer and the auth0 SDK.
sidebar:
  label: Node.js
  order: 2
---

Test a Node API and a Management API client against the mock, using `express-oauth2-jwt-bearer` and the `auth0` SDK.

## Start the mock

```bash
mkdir -p .auth0-mock
docker run -d --rm --name auth0-mock --user "$(id -u):$(id -g)" \
  -p 127.0.0.1:8443:8443 -p 127.0.0.1:8080:8080 \
  -e ADMIN_ALLOW_REMOTE=true -e TLS_CACHE_DIR=/tls -v "$PWD/.auth0-mock:/tls" \
  ghcr.io/sergiught/auth0-mock:latest
until [ -s .auth0-mock/tls.crt ] && curl -sf -o /dev/null --cacert .auth0-mock/tls.crt https://localhost:8443/healthz; do sleep 0.5; done
```

The `--user` flag lets the container write the certificate into your `.auth0-mock/` folder.
Run `mkdir -p .auth0-mock` first, otherwise Docker creates the folder as root and the mock cannot write to it.
If the loop never finishes, check `docker logs auth0-mock`.
Add `.auth0-mock/` to your own `.gitignore`: it holds the mock's private key.

## Different port

If you map a different host port (for example `-p 127.0.0.1:9443:8443`), also set `-e ISSUER_URL=https://localhost:9443/` and `AUTH0_MOCK_URL=https://localhost:9443/`. Otherwise the `iss` claim in tokens will not match the URL you call. Use that port in the commands below too.
From release 0.234.0 you can set `-e ISSUER_URL=auto` instead. The mock then builds the issuer from the address of each request, so any mapped port works.

## Trust its certificate

The mock serves HTTPS with a self-signed certificate. Tell Node to trust it:

```bash
export NODE_EXTRA_CA_CERTS="$PWD/.auth0-mock/tls.crt"
```

Without it, the first call to the mock fails with `self-signed certificate` (error code `DEPTH_ZERO_SELF_SIGNED_CERT`).

## Your API checks tokens

The API from [`examples/node/app.mjs`](https://github.com/sergiught/auth0-mock/blob/main/examples/node/app.mjs) protects `/private` with `express-oauth2-jwt-bearer`:

```js
import express from "express";
import { auth } from "express-oauth2-jwt-bearer";

export function createApp({ issuerBaseURL, audience }) {
  const app = express();
  app.get("/private", auth({ issuerBaseURL, audience }), (req, res) => {
    res.json({ sub: req.auth.payload.sub });
  });
  app.use((err, req, res, next) => {
    res.status(err.status ?? 500).json({ error: err.code ?? "server_error" });
  });
  return app;
}
```

The error handler turns the library's errors (which carry `status`) into a JSON response without logging a stack trace. The tests get a token from the mock with `mintToken` in [`examples/node/mock.mjs`](https://github.com/sergiught/auth0-mock/blob/main/examples/node/mock.mjs):

```js
export const MOCK_URL = process.env.AUTH0_MOCK_URL ?? "https://localhost:8443/";

export async function mintToken(audience) {
  const res = await fetch(new URL("oauth/token", MOCK_URL), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      grant_type: "client_credentials",
      client_id: "demo",
      client_secret: "x",
      audience,
    }),
  });
  if (!res.ok) throw new Error(`oauth/token returned ${res.status}`);
  return (await res.json()).access_token;
}
```

Point the API at the mock and check the good, missing and wrong-audience cases ([`examples/node/api.test.mjs`](https://github.com/sergiught/auth0-mock/blob/main/examples/node/api.test.mjs)):

```js
import { after, before, test } from "node:test";
import assert from "node:assert/strict";
import { createApp } from "./app.mjs";
import { MOCK_URL, mintToken } from "./mock.mjs";

const AUDIENCE = "https://my-api.example.com";
let server;
let base;

before(async () => {
  const app = createApp({ issuerBaseURL: MOCK_URL, audience: AUDIENCE });
  server = app.listen(0);
  await new Promise((resolve) => server.once("listening", resolve));
  base = `http://localhost:${server.address().port}`;
});

after(() => server.close());

test("token from the mock is accepted", async () => {
  const token = await mintToken(AUDIENCE);
  const res = await fetch(`${base}/private`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  assert.equal(res.status, 200);
  assert.equal((await res.json()).sub, "demo@clients");
});

test("missing token is rejected", async () => {
  const res = await fetch(`${base}/private`);
  assert.equal(res.status, 401);
});

test("token for another API is rejected", async () => {
  const token = await mintToken("https://other.example.com");
  const res = await fetch(`${base}/private`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  assert.equal(res.status, 401);
});
```

## Your app calls the Management API

Stub the response with `stub` from `mock.mjs`:

```js
export async function stub(expectation) {
  const res = await fetch(new URL("admin0/expectations", MOCK_URL), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(expectation),
  });
  if (!res.ok) throw new Error(`admin0/expectations returned ${res.status}`);
}
```

Then call it through `ManagementClient` ([`examples/node/management.test.mjs`](https://github.com/sergiught/auth0-mock/blob/main/examples/node/management.test.mjs)). The client fetches its own token from the mock using the client ID and secret:

```js
import { test } from "node:test";
import assert from "node:assert/strict";
import { ManagementClient } from "auth0";
import { MOCK_URL, stub } from "./mock.mjs";

test("SDK reads a stubbed user", async () => {
  await stub({
    method: "GET",
    path: "/api/v2/users/auth0|123",
    response: {
      status: 200,
      body: { user_id: "auth0|123", email: "alice@example.com" },
    },
  });

  const management = new ManagementClient({
    domain: new URL(MOCK_URL).host,
    clientId: "demo",
    clientSecret: "x",
  });
  const user = await management.users.get("auth0|123");

  assert.equal(user.user_id, "auth0|123");
  assert.equal(user.email, "alice@example.com");
});
```

## Run the example

From a clone of this repo: install the dependencies once, then run the tests. CI does the same with `make test-examples`.

```bash
cd examples/node
npm ci
NODE_EXTRA_CA_CERTS=../../.auth0-mock/tls.crt node --test
```

The full files are in [`examples/node/`](https://github.com/sergiught/auth0-mock/tree/main/examples/node/).

## Stop the mock

```bash
docker stop auth0-mock
```

With `--rm` this also removes the container.

## Other languages

- [curl quickstart](/auth0-mock/quickstarts/http/)
- [Python quickstart](/auth0-mock/quickstarts/python/)
- [Go quickstart](/auth0-mock/quickstarts/go/)
- [Go SDK](/auth0-mock/guides/go-sdk/)
