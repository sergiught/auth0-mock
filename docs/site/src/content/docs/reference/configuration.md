---
title: "Configuration"
description: "Every environment variable and flag that configures auth0-mock."
sidebar:
  order: 2
---

Environment variables (see [`.env.example`](https://github.com/sergiught/auth0-mock/blob/main/.env.example) for the full template):

| Variable | Default | Notes |
|---|---|---|
| `HTTP_ADDR` | `127.0.0.1:8080` | The HTTP listener address. Set to `0.0.0.0:8080` to accept LAN/container traffic (the Dockerfile already does); also set `ADMIN_ALLOW_REMOTE=true`. To run HTTPS-only, set this to `off`. |
| `HTTPS_ADDR` | `127.0.0.1:8443` | The HTTPS listener address. Set to `0.0.0.0:8443` to accept LAN/container traffic (the Dockerfile already does); also set `ADMIN_ALLOW_REMOTE=true`. To run HTTP-only, set this to `off`. |
| `ADMIN_ALLOW_REMOTE` | `false` | The mock refuses to start when `HTTP_ADDR` or `HTTPS_ADDR` can be reached from other machines (`0.0.0.0`, `::`, a LAN IP, a hostname other than `localhost`), because `/admin0` is unauthenticated. Set `true` to allow it; Docker needs this |
| `TLS_CERT_FILE` / `TLS_KEY_FILE` | _empty_ | If both set → load. Else → auto-generate (see [HTTPS / TLS](/auth0-mock/guides/https-tls/)) |
| `TLS_CACHE_DIR` | _empty_ | If set, persist auto-gen cert to `<dir>/tls.{crt,key}` and reuse on restart |
| `TLS_HOSTNAMES` | `localhost,127.0.0.1,::1` | SAN entries on the auto-generated cert |
| `SIGNING_KEY_FILE` | _empty_ | PEM-encoded RSA key. Otherwise a fresh RS256 key is generated each boot |
| `SEED_FILE` | _empty_ (off) | Path to a YAML or JSON seed file loaded at startup and re-applied by `POST /admin0/reset`. Reloaded about once a second when its content changes. From release 0.234.0. See [Seed file](/auth0-mock/guides/seed-file/) |
| `ISSUER_URL` | `https://localhost:8443/` | `iss` claim and OIDC discovery base. `auto` derives it from the request's address (scheme, host and port), so a token only verifies at the address it was minted at; `DEFAULT_AUDIENCE` is not derived per request in this mode and keeps its own default |
| `DEFAULT_AUDIENCE` | `https://localhost:8443/api/v2/` | Default `aud` if request doesn't supply one; blank or whitespace means no default audience, as on a tenant without one: user logins then get opaque access tokens that only `/userinfo` accepts, and `client_credentials` without an `audience` gets 403 `access_denied` |
| `ACCESS_TOKEN_TTL` | `24h` | Minted access token lifetime |
| `ID_TOKEN_TTL` | `24h` | Minted ID token lifetime |
| `TOKEN_LEEWAY` | `0s` | Clock-skew grace on JWT exp, nbf and iat checks. `0s` means a token expires exactly when its lifetime ends; set e.g. `60s` for CI runners with skewed clocks |
| `SPEC_VALIDATION_STRICT` | `true` | If `false`, runtime response re-check (defence in depth) logs but doesn't fail |
| `LOG_LEVEL` | `info` | zerolog levels |
| `DEBUG` | `false` | When `true`, every request and response is logged in full at INFO level: method, path, query, headers (Authorization / Cookie redacted), and body (truncated at 8 KiB). Off by default — turn on only while debugging an SDK trace; adds an allocation and a synchronous log write per request. |
| `READ_HEADER_TIMEOUT` | `5s` | http.Server's `ReadHeaderTimeout` |
| `WRITE_TIMEOUT` | `30s` | http.Server's `WriteTimeout`. Bounds slow-write attacks. **Doesn't apply to `/api/v2/events`** — the SSE handler clears the deadline per-connection so long-lived subscribers aren't torn down. |
| `IDLE_TIMEOUT` | `120s` | http.Server's `IdleTimeout`. Bounds idle keep-alive connections. |
| `MAX_REQUEST_BODY_BYTES` | `1048576` (1 MiB) | Per-request body cap. Anything larger is read up to this point and the handler surfaces a 400. Set to `0` to disable. |
| `EVENTS_REPLAY_BUFFER` | `100` | Cap of the `/api/v2/events` SSE replay ring buffer. Reconnecting subscribers can resume from `Last-Event-ID`, `?from=<offset>`, or `?from_timestamp=<rfc3339>` up to this many events back. `<= 0` disables replay (the endpoint still works; resume params become no-ops). |
| `REQUEST_JOURNAL_SIZE` | `1000` | Cap of the in-memory request journal behind `/admin0/journal` (oldest entries are evicted). `<= 0` disables recording: the endpoints return `{"entries":[]}`. The `; closest stub differed on …` hint on `no_match` 404s is unaffected. |
| `EVENTS_RECONNECT_HINT` | `3s` | The SSE `retry:` reconnect-delay hint sent to `/api/v2/events` subscribers on connect (mirrors Auth0). Clients wait this long before reconnecting after a disconnect. `0` omits the hint, so clients use their built-in default. |
| `SHUTDOWN_TIMEOUT` | `5s` | Graceful-shutdown grace period |
| `LOGOUT_ALLOWED_URLS` | _empty_ | Comma-separated allow-list of absolute URLs that `/v2/logout` (`returnTo`) and `/oidc/logout` (`post_logout_redirect_uri`) will 302 to. Empty (default) = no enforcement so SDK tests calling `/v2/logout?returnTo=https://…` work out of the box. When set, mirrors Auth0's tenant "Allowed Logout URLs" setting: relative URLs are always allowed, unlisted absolutes get 400, dangerous schemes (`javascript:`, `data:`, …) and backslash bypasses are rejected. Set in production-like fixtures. |
| `AUTHORIZE_ALLOWED_CALLBACKS` | _empty_ | Comma-separated allow-list of absolute `redirect_uri` values that `/authorize` will 302 to. Same threat model as `LOGOUT_ALLOWED_URLS` but on the higher-value endpoint: `/authorize` carries `code` / `access_token` in the URL, so an unvalidated `redirect_uri` leaks them. Empty (default) = no enforcement so test SDKs can register any callback; set in production-like fixtures. Mirrors Auth0's per-application "Allowed Callback URLs" setting. |
| `CORS_ALLOWED_ORIGINS` | _empty_ | Comma-separated allow-list of browser origins that get CORS headers. Empty (default) = any origin, so a browser app can call the mock with no setup. Entries are exact origins (`http://localhost:3000`) or `https://*.example.com` (matches exactly one subdomain level; scheme and port must match). `*` = any origin. Unlisted origins get no CORS headers and their preflights get 403. Default ports (`:443`/`:80`) are normalised; userinfo and empty ports are rejected. Invalid entries fail startup. Mirrors Auth0's per-application "Allowed Web Origins" / "Allowed Origins (CORS)" settings. |
| `BEARER_REQUIRE_AUDIENCE` | _empty_ | When set, the Mgmt-API bearer middleware rejects tokens whose `aud` claim doesn't contain this value (mirrors Auth0's tenant-API-audience binding). Empty keeps the "echoed, not enforced" default so tests can swap audiences freely. |

> **Changed:** tokens used to stay valid for 60 seconds past their lifetime. They now expire exactly on time, so moving `/admin0/clock` past the TTL gives a 401 at once. Set `TOKEN_LEEWAY=60s` for the old behaviour.
