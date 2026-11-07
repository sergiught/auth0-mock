import json
import os
import urllib.error
import urllib.parse
import urllib.request

import jwt
from testcontainers.core.container import DockerContainer
from testcontainers.core.wait_strategies import HttpWaitStrategy


def call(method, url, *, data=None, headers=None):
    req = urllib.request.Request(url, data=data, headers=headers or {}, method=method)
    try:
        res = urllib.request.urlopen(req)
    except urllib.error.HTTPError as e:  # urllib raises on 4xx/5xx; keep the status and body for assertion messages
        res = e
    with res:
        text = res.read().decode()
    try:
        return res.status, json.loads(text)
    except ValueError:
        return res.status, text


def test_auth0_mock():
    # CI sets AUTH0_MOCK_IMAGE to a locally built image; readers get the published one.
    image = os.environ.get("AUTH0_MOCK_IMAGE", "ghcr.io/sergiught/auth0-mock:latest")

    container = (
        DockerContainer(image)
        .with_env("ADMIN_ALLOW_REMOTE", "true")  # the admin API is loopback-only by default; the test runs outside the container
        .with_env("HTTPS_ADDR", "off")  # plain HTTP, no certificate to trust
        .with_env("ISSUER_URL", "auto")  # issuer follows the URL the test reaches the mock on
        .with_exposed_ports(8080)
        # Wait for readiness instead of sleeping.
        .waiting_for(HttpWaitStrategy(8080, "/healthz").for_status_code(200))
    )
    with container:
        # Ask Testcontainers for the host and the random mapped port; the host is not always this machine.
        base = f"http://{container.get_container_host_ip()}:{container.get_exposed_port(8080)}"

        # Stub the Management API call the app under test will make.
        stub = {
            "method": "GET",
            "path": "/api/v2/users/auth0|123",
            "response": {
                "status": 200,
                "body": {"user_id": "auth0|123", "email": "alice@example.com", "name": "Alice"},
            },
        }
        status, body = call(
            "POST",
            f"{base}/admin0/expectations",
            data=json.dumps(stub).encode(),
            headers={"content-type": "application/json"},
        )
        assert status < 300, f"stub status {status}: {body}"

        # Get a token the way the app does.
        form = urllib.parse.urlencode(
            {
                "grant_type": "client_credentials",
                "client_id": "demo",
                "client_secret": "x",
                "audience": f"{base}/api/v2/",
            }
        )
        status, body = call("POST", f"{base}/oauth/token", data=form.encode())
        assert status == 200, f"token status {status}: {body}"
        token = body["access_token"]
        assert token

        # Call the stubbed endpoint; the "|" is percent-encoded so every HTTP client accepts it.
        status, user = call(
            "GET",
            f"{base}/api/v2/users/auth0%7C123",
            headers={"authorization": f"Bearer {token}"},
        )
        assert status == 200, f"user status {status}: {user}"
        assert user["user_id"] == "auth0|123"
        assert user["email"] == "alice@example.com"

        # Verify the token as a real app would: discovery, JWKS, RS256 signature, issuer and audience.
        _, discovery = call("GET", f"{base}/.well-known/openid-configuration")
        key = jwt.PyJWKClient(discovery["jwks_uri"]).get_signing_key_from_jwt(token)
        jwt.decode(
            token,
            key,
            algorithms=["RS256"],
            audience=f"{base}/api/v2/",
            issuer=f"{base}/",
        )
