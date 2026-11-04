# curl quickstart

Call the mock with plain `curl`. No SDK needed.

## Start the mock

```bash
mkdir -p .auth0-mock
docker run -d --name auth0-mock --user "$(id -u):$(id -g)" \
  -p 127.0.0.1:8443:8443 -p 127.0.0.1:8080:8080 \
  -e ADMIN_ALLOW_REMOTE=true -e TLS_CACHE_DIR=/tls -v "$PWD/.auth0-mock:/tls" \
  ghcr.io/sergiught/auth0-mock:latest
until curl -sf --cacert .auth0-mock/tls.crt https://localhost:8443/healthz; do sleep 0.5; done
```

The `--user` flag lets the container write the certificate into your `.auth0-mock/` folder.
Run `mkdir -p .auth0-mock` first, otherwise Docker creates the folder as root and the mock cannot write to it.

If you map a different host port (for example `-p 127.0.0.1:9443:8443`), also set `-e ISSUER_URL=https://localhost:9443/` and `AUTH0_MOCK_URL=https://localhost:9443/`. Otherwise the `iss` claim in tokens will not match the URL you call.

## Trust its certificate

The mock serves HTTPS with a self-signed certificate. Pass it to curl with `--cacert .auth0-mock/tls.crt`.
Other tools use environment variables: `NODE_EXTRA_CA_CERTS` for Node and `SSL_CERT_FILE` for Python. See [HTTPS / TLS](../../README.md#-https--tls) in the README.

## Point your code at the mock

| Setting | Value |
|---|---|
| Domain | `localhost:8443` |
| Issuer / base URL | `https://localhost:8443/` |
| JWKS | `https://localhost:8443/.well-known/jwks.json` |
| Management API | `https://localhost:8443/api/v2/` |

## Run the example

[`examples/http/quickstart.sh`](../../examples/http/quickstart.sh) does three things and prints `ok:` for each:

1. Reads the discovery document and checks the issuer matches `AUTH0_MOCK_URL`.
2. Gets a `client_credentials` token for `https://my-api.example.com` and checks its `aud` and `sub`.
3. Stubs `GET /api/v2/users/auth0|123`, then calls it with the token and checks the email.

```bash
AUTH0_MOCK_CA=.auth0-mock/tls.crt examples/http/quickstart.sh
```

It needs `curl` and `jq`. CI runs it with `make test-examples`.
