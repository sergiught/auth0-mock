import json
import os
import urllib.request
from urllib.parse import urljoin

import pytest


@pytest.fixture
def mock_url():
    return os.environ.get("AUTH0_MOCK_URL", "https://localhost:8443/")


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
