import { test } from "node:test";
import assert from "node:assert/strict";
import { ManagementClient } from "auth0";
import { MOCK_URL, mintToken, stub } from "./mock.mjs";

test("Management API call is answered by a stub", async () => {
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
});
