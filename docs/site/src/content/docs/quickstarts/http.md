---
title: curl quickstart
description: Call the mock with plain curl, from starting it to calling a stubbed endpoint.
sidebar:
  order: 1
---

Call the mock with plain `curl`. No SDK needed.

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

The mock serves HTTPS with a self-signed certificate. Pass it to curl with `--cacert .auth0-mock/tls.crt`.
Other tools use their own setting:

- Node: `NODE_EXTRA_CA_CERTS`
- Python: `SSL_CERT_FILE`
- Go: pass the certificate in the client's `RootCAs` pool

See [HTTPS / TLS](/auth0-mock/guides/https-tls/#https--tls) in the HTTPS guide.

## Point your code at the mock

| Setting | Value |
|---|---|
| Domain | `localhost:8443` |
| Issuer / base URL | `https://localhost:8443/` |
| JWKS | `https://localhost:8443/.well-known/jwks.json` |
| Management API | `https://localhost:8443/api/v2/` |

## Get a token and call a stubbed endpoint

Check that discovery reports the issuer you expect:

```bash
curl -s --cacert .auth0-mock/tls.crt https://localhost:8443/.well-known/openid-configuration | jq -r .issuer
# => https://localhost:8443/
```

Mint a token for your API:

```bash
TOKEN=$(curl -s --cacert .auth0-mock/tls.crt -X POST https://localhost:8443/oauth/token \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'grant_type=client_credentials&client_id=demo&client_secret=x&audience=https://my-api.example.com' \
  | jq -r .access_token)
```

Stub a Management API response:

```bash
curl -s --cacert .auth0-mock/tls.crt -X POST https://localhost:8443/admin0/expectations \
  -H 'Content-Type: application/json' \
  -d '{"method":"GET","path":"/api/v2/users/auth0|123","response":{"status":200,"body":{"user_id":"auth0|123","email":"alice@example.com"}}}'
```

The Management API expects a token for its own audience. Mint one without an `audience` parameter, which gives the default `https://localhost:8443/api/v2/`:

```bash
MGMT_TOKEN=$(curl -s --cacert .auth0-mock/tls.crt -X POST https://localhost:8443/oauth/token \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'grant_type=client_credentials&client_id=demo&client_secret=x' \
  | jq -r .access_token)
```

Call the endpoint with it:

```bash
curl -s --cacert .auth0-mock/tls.crt https://localhost:8443/api/v2/users/auth0%7C123 \
  -H "Authorization: Bearer ${MGMT_TOKEN}"
# => {"user_id":"auth0|123","email":"alice@example.com"}
```

## Run the example

From a clone of this repo: [`examples/http/quickstart.sh`](https://github.com/sergiught/auth0-mock/blob/main/examples/http/quickstart.sh) runs the same requests, checks the results and prints `ok:` for each step. It needs `curl` and `jq`. CI runs it with `make test-examples`.

```bash
AUTH0_MOCK_CA=.auth0-mock/tls.crt examples/http/quickstart.sh
```

## Stop the mock

```bash
docker stop auth0-mock
```

With `--rm` this also removes the container.

## Other languages

- [Node.js quickstart](/auth0-mock/quickstarts/node/)
- [Python quickstart](/auth0-mock/quickstarts/python/)
- [Go SDK](/auth0-mock/guides/go-sdk/)
