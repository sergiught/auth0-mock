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

test("token for another audience is rejected", async () => {
  const token = await mintToken("https://other.example.com");
  const res = await fetch(`${base}/private`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  assert.equal(res.status, 401);
});
