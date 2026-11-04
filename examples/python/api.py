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
