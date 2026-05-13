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
            "e":   "<<PRESENCE>>"
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
        "issuer":                                "<<PRESENCE>>",
        "jwks_uri":                              "<<PRESENCE>>",
        "token_endpoint":                        "<<PRESENCE>>",
        "authorization_endpoint":                "<<PRESENCE>>",
        "userinfo_endpoint":                     "<<PRESENCE>>",
        "end_session_endpoint":                  "<<PRESENCE>>",
        "revocation_endpoint":                   "<<PRESENCE>>",
        "response_types_supported":              "<<PRESENCE>>",
        "subject_types_supported":               "<<PRESENCE>>",
        "id_token_signing_alg_values_supported": ["RS256"],
        "token_endpoint_auth_methods_supported": "<<PRESENCE>>",
        "scopes_supported":                      "<<PRESENCE>>",
        "grant_types_supported":                 "<<PRESENCE>>"
      }
      """
