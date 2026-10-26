Feature: OIDC discovery and JWKS publication
  Background:
    Given the mock is running

  Scenario: JWKS document exposes an RS256 public key
    When I send "GET /.well-known/jwks.json" without a bearer
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      {
        "keys": [
          {
            "kty": "RSA",
            "alg": "RS256",
            "use": "sig",
            "kid": "<<PRESENCE>>",
            "n":   "<<PRESENCE>>",
            "e":   "AQAB"
          }
        ]
      }
      """

  Scenario: OpenID discovery document points at the configured endpoints
    When I send "GET /.well-known/openid-configuration" without a bearer
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      {
        "issuer":                 "${BASE_URL}/",
        "authorization_endpoint": "${BASE_URL}/authorize",
        "token_endpoint":         "${BASE_URL}/oauth/token",
        "userinfo_endpoint":      "${BASE_URL}/userinfo",
        "jwks_uri":               "${BASE_URL}/.well-known/jwks.json",
        "end_session_endpoint":   "${BASE_URL}/oidc/logout",
        "revocation_endpoint":    "${BASE_URL}/oauth/revoke",
        "response_types_supported": [
          "code", "token", "id_token",
          "code token", "code id_token", "token id_token", "code token id_token"
        ],
        "subject_types_supported":               ["public"],
        "id_token_signing_alg_values_supported": ["RS256"],
        "token_endpoint_auth_methods_supported": ["client_secret_basic", "client_secret_post", "none"],
        "scopes_supported":                      ["openid", "profile", "email", "offline_access"],
        "grant_types_supported":                 [
          "client_credentials", "authorization_code", "refresh_token", "password", "implicit",
          "http://auth0.com/oauth/grant-type/password-realm",
          "http://auth0.com/oauth/grant-type/passwordless/otp",
          "http://auth0.com/oauth/grant-type/mfa-oob",
          "http://auth0.com/oauth/grant-type/mfa-otp",
          "http://auth0.com/oauth/grant-type/mfa-recovery-code"
        ],
        "code_challenge_methods_supported":      ["S256", "plain"],
        "response_modes_supported":              ["query", "fragment", "form_post"],
        "claims_supported": [
          "aud", "auth_time", "azp", "email", "email_verified", "exp", "iat", "iss", "name", "nickname", "nonce", "phone_number", "phone_number_verified", "picture", "sub"
        ]
      }
      """
