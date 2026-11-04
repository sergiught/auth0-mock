# Node.js quickstart

Test a Node API and a Management API client against the mock, using `express-oauth2-jwt-bearer` and the `auth0` SDK.

## Start the mock

```bash
mkdir -p .auth0-mock
docker run -d --name auth0-mock --user "$(id -u):$(id -g)" \
  -p 127.0.0.1:8443:8443 -p 127.0.0.1:8080:8080 \
  -e ADMIN_ALLOW_REMOTE=true -e TLS_CACHE_DIR=/tls -v "$PWD/.auth0-mock:/tls" \
  ghcr.io/sergiught/auth0-mock:latest
until curl -sf -o /dev/null --cacert .auth0-mock/tls.crt https://localhost:8443/healthz; do sleep 0.5; done
```

The `--user` flag lets the container write the certificate into your `.auth0-mock/` folder.
Run `mkdir -p .auth0-mock` first, otherwise Docker creates the folder as root and the mock cannot write to it.

## Different port

If you map a different host port (for example `-p 127.0.0.1:9443:8443`), also set `-e ISSUER_URL=https://localhost:9443/` and `AUTH0_MOCK_URL=https://localhost:9443/`. Otherwise the `iss` claim in tokens will not match the URL you call.

## Trust its certificate

The mock serves HTTPS with a self-signed certificate. Tell Node to trust it:

```bash
export NODE_EXTRA_CA_CERTS=.auth0-mock/tls.crt
```

Without it, the first call to the mock fails with `self-signed certificate` (error code `DEPTH_ZERO_SELF_SIGNED_CERT`).

## Your API checks tokens

The API from [`examples/node/app.mjs`](../../examples/node/app.mjs) protects `/private` with `express-oauth2-jwt-bearer`:

```js
export function createApp({ issuerBaseURL, audience }) {
  const app = express();
  app.get("/private", auth({ issuerBaseURL, audience }), (req, res) => {
    res.json({ sub: req.auth.payload.sub });
  });
  return app;
}
```

The tests get a token from the mock with `mintToken` in [`examples/node/mock.mjs`](../../examples/node/mock.mjs):

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

Point the API at the mock and check both a good and a wrong audience ([`examples/node/api.test.mjs`](../../examples/node/api.test.mjs)):

```js
const AUDIENCE = "https://my-api.example.com";
const app = createApp({ issuerBaseURL: MOCK_URL, audience: AUDIENCE });

test("token from the mock is accepted", async () => {
  const token = await mintToken(AUDIENCE);
  const res = await fetch(`${base}/private`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  assert.equal(res.status, 200);
  assert.equal((await res.json()).sub, "demo@clients");
});

test("token for another audience is rejected", async () => {
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

Then call it through `ManagementClient` ([`examples/node/management.test.mjs`](../../examples/node/management.test.mjs)):

```js
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
  token: await mintToken(`${MOCK_URL}api/v2/`),
});
const user = await management.users.get("auth0|123");

assert.equal(user.user_id, "auth0|123");
assert.equal(user.email, "alice@example.com");
```

## Run the example

Install the dependencies once, then run the tests. CI does the same with `make test-examples`.

```bash
cd examples/node
npm ci
NODE_EXTRA_CA_CERTS=../../.auth0-mock/tls.crt node --test
```

The full files are in [`examples/node/`](../../examples/node/).

## Other languages

- [curl quickstart](http.md)
- [Python quickstart](python.md)
