---
title: Python quickstart
description: Test a Python API and a Management API client against the mock with PyJWT and the auth0-python SDK.
sidebar:
  label: Python
  order: 3
---

Test a Python API and a Management API client against the mock, using `PyJWT` and the `auth0-python` SDK.

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

The mock serves HTTPS with a self-signed certificate. Run `uv sync` / `pip install` first, then tell Python to trust it:

```bash
export SSL_CERT_FILE="$PWD/.auth0-mock/tls.crt"
```

Without it, the first call to the mock fails with `CERTIFICATE_VERIFY_FAILED` (`self-signed certificate`).

Install first because `SSL_CERT_FILE` replaces Python's trust store instead of adding to it, so other HTTPS calls in the same process (and tools like `uv` or `pip`) fail while it is set.

If your code calls the mock through `requests`, also set `REQUESTS_CA_BUNDLE` to the same file, because `requests` ignores `SSL_CERT_FILE`.

If your tests also call real HTTPS services, build a combined bundle and point `SSL_CERT_FILE` at that instead. `certifi` comes in with `auth0-python`, so ask the example's environment for it. In your own project, drop `--project examples/python`:

```bash
cat "$(uv run --project examples/python --no-sync python -m certifi)" .auth0-mock/tls.crt > bundle.pem
export SSL_CERT_FILE="$PWD/bundle.pem"
```

## Your API checks tokens

`verify_token` from [`examples/python/api.py`](https://github.com/sergiught/auth0-mock/blob/main/examples/python/api.py) checks a token's signature, issuer and audience against the mock's JWKS:

```python
import jwt

_jwks_clients = {}


def verify_token(token, *, issuer, audience):
    jwks = _jwks_clients.setdefault(
        issuer, jwt.PyJWKClient(f"{issuer}.well-known/jwks.json")
    )
    signing_key = jwks.get_signing_key_from_jwt(token)
    return jwt.decode(
        token,
        signing_key,
        algorithms=["RS256"],
        audience=audience,
        issuer=issuer,
    )
```

Failures raise `jwt.PyJWTError` subclasses such as `jwt.InvalidAudienceError`. The tests share fixtures in [`examples/python/conftest.py`](https://github.com/sergiught/auth0-mock/blob/main/examples/python/conftest.py). The top of the file reads the mock's URL:

```python
import json
import os
import urllib.request
from urllib.parse import urljoin

import pytest


@pytest.fixture
def mock_url():
    return os.environ.get("AUTH0_MOCK_URL", "https://localhost:8443/")
```

The tests get a token from the mock with `mint_token`:

```python
@pytest.fixture
def mint_token(mock_url):
    def mint(audience):
        req = urllib.request.Request(
            urljoin(mock_url, "oauth/token"),
            data=json.dumps(
                {
                    "grant_type": "client_credentials",
                    "client_id": "demo",
                    "client_secret": "x",
                    "audience": audience,
                }
            ).encode(),
            headers={"Content-Type": "application/json"},
        )
        with urllib.request.urlopen(req) as res:
            return json.load(res)["access_token"]

    return mint
```

Check the good case and the wrong-audience and garbage-token cases ([`examples/python/test_api.py`](https://github.com/sergiught/auth0-mock/blob/main/examples/python/test_api.py)):

```python
import jwt
import pytest

from api import verify_token

AUDIENCE = "https://my-api.example.com"


def test_token_from_mock_is_accepted(mock_url, mint_token):
    claims = verify_token(mint_token(AUDIENCE), issuer=mock_url, audience=AUDIENCE)
    assert claims["sub"] == "demo@clients"


def test_token_for_another_api_is_rejected(mock_url, mint_token):
    token = mint_token("https://other.example.com")
    with pytest.raises(jwt.InvalidAudienceError):
        verify_token(token, issuer=mock_url, audience=AUDIENCE)


def test_garbage_token_is_rejected(mock_url):
    with pytest.raises(jwt.PyJWTError):
        verify_token("not-a-jwt", issuer=mock_url, audience=AUDIENCE)
```

## Your app calls the Management API

Stub a response with `stub` from `conftest.py`:

```python
@pytest.fixture
def stub(mock_url):
    def add(expectation):
        req = urllib.request.Request(
            urljoin(mock_url, "admin0/expectations"),
            data=json.dumps(expectation).encode(),
            headers={"Content-Type": "application/json"},
        )
        urllib.request.urlopen(req).close()

    return add
```

Then call it through `ManagementClient` ([`examples/python/test_management.py`](https://github.com/sergiught/auth0-mock/blob/main/examples/python/test_management.py)). The client fetches its own token from the mock using the client ID and secret:

```python
from urllib.parse import urlparse

from auth0.management import ManagementClient


def test_sdk_reads_stubbed_user(mock_url, stub):
    stub(
        {
            "method": "GET",
            "path": "/api/v2/users/auth0|123",
            "response": {
                "status": 200,
                "body": {"user_id": "auth0|123", "email": "alice@example.com"},
            },
        }
    )

    management = ManagementClient(
        domain=urlparse(mock_url).netloc,
        client_id="demo",
        client_secret="x",
    )
    user = management.users.get("auth0|123")

    assert user.user_id == "auth0|123"
    assert user.email == "alice@example.com"
```

## Run the example

From a clone of this repo: install the dependencies once, then run the tests. `env -u SSL_CERT_FILE` keeps an exported mock certificate away from `uv sync`. CI does the same with `make test-examples`.

```bash
cd examples/python
env -u SSL_CERT_FILE uv sync --locked
SSL_CERT_FILE=../../.auth0-mock/tls.crt uv run --locked pytest
```

Without `uv`, install `pyjwt[crypto]`, `auth0-python` and `pytest` into a virtual environment and run `SSL_CERT_FILE=../../.auth0-mock/tls.crt pytest`.

The full files are in [`examples/python/`](https://github.com/sergiught/auth0-mock/tree/main/examples/python/).

## Stop the mock

```bash
docker stop auth0-mock
```

With `--rm` this also removes the container.

## Other languages

- [curl quickstart](/auth0-mock/quickstarts/http/)
- [Node.js quickstart](/auth0-mock/quickstarts/node/)
- [Go quickstart](/auth0-mock/quickstarts/go/)
- [Go SDK](/auth0-mock/guides/go-sdk/)
