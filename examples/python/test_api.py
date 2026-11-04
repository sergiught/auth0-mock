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
