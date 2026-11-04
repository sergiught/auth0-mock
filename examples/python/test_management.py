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
