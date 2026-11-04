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

export async function stub(expectation) {
  const res = await fetch(new URL("admin0/expectations", MOCK_URL), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(expectation),
  });
  if (!res.ok) throw new Error(`admin0/expectations returned ${res.status}`);
}
