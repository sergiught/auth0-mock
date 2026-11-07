import assert from "node:assert/strict";
import { test } from "node:test";
import { createRemoteJWKSet, jwtVerify } from "jose";
import { GenericContainer, Wait } from "testcontainers";

test("auth0-mock in Testcontainers", async (t) => {
  // CI sets AUTH0_MOCK_IMAGE to a locally built image; readers get the published one.
  const image = process.env.AUTH0_MOCK_IMAGE || "ghcr.io/sergiught/auth0-mock:latest";

  const container = await new GenericContainer(image)
    .withEnvironment({
      ADMIN_ALLOW_REMOTE: "true", // the image listens on 0.0.0.0; the mock refuses to start that way without this, since /admin0 has no auth
      HTTPS_ADDR: "off", // plain HTTP, no certificate to trust
      ISSUER_URL: "auto", // issuer follows the URL the test reaches the mock on
    })
    .withExposedPorts(8080)
    // Wait for readiness instead of sleeping.
    .withWaitStrategy(Wait.forHttp("/healthz", 8080).forStatusCode(200))
    .start();
  t.after(() => container.stop());

  // Ask Testcontainers for the host and the random mapped port; the host is not always this machine.
  const base = `http://${container.getHost()}:${container.getMappedPort(8080)}`;

  // Stub the Management API call the app under test will make.
  const stub = {
    method: "GET",
    path: "/api/v2/users/auth0|123",
    response: {
      status: 200,
      body: { user_id: "auth0|123", email: "alice@example.com", name: "Alice" },
    },
  };
  let res = await fetch(`${base}/admin0/expectations`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(stub),
  });
  assert.ok(res.status < 300, `stub status ${res.status}`);

  // Get a token the way the app does.
  res = await fetch(`${base}/oauth/token`, {
    method: "POST",
    body: new URLSearchParams({
      grant_type: "client_credentials",
      client_id: "demo",
      client_secret: "x",
      audience: `${base}/api/v2/`,
    }),
  });
  assert.equal(res.status, 200);
  const { access_token } = await res.json();
  assert.ok(access_token);

  // Call the stubbed endpoint; the "|" is percent-encoded so every HTTP client accepts it.
  res = await fetch(`${base}/api/v2/users/auth0%7C123`, {
    headers: { authorization: `Bearer ${access_token}` },
  });
  assert.equal(res.status, 200);
  const user = await res.json();
  assert.equal(user.user_id, "auth0|123");
  assert.equal(user.email, "alice@example.com");

  // Verify the token as a real app would: discovery, JWKS, RS256 signature, issuer and audience.
  const discovery = await (await fetch(`${base}/.well-known/openid-configuration`)).json();
  const jwks = createRemoteJWKSet(new URL(discovery.jwks_uri));
  await jwtVerify(access_token, jwks, {
    issuer: `${base}/`,
    audience: `${base}/api/v2/`,
    algorithms: ["RS256"],
  });
});
