Feature: OIDC discovery and JWKS publication
  Background:
    Given the mock is running

  Scenario: JWKS document exposes an RS256 public key
    When I send "GET /.well-known/jwks.json" without a bearer
    Then I receive a 200 response
    And the response JSON path "keys.0.kty" equals "RSA"
    And the response JSON path "keys.0.alg" equals "RS256"
    And the response JSON path "keys.0.use" equals "sig"
    And the response JSON path "keys.0.kid" exists
    And the response JSON path "keys.0.n" exists
    And the response JSON path "keys.0.e" exists

  Scenario: OpenID discovery document points at the configured endpoints
    When I send "GET /.well-known/openid-configuration" without a bearer
    Then I receive a 200 response
    And the response JSON path "issuer" exists
    And the response JSON path "jwks_uri" exists
    And the response JSON path "token_endpoint" exists
    And the response JSON path "authorization_endpoint" exists
    And the response JSON path "userinfo_endpoint" exists
    And the response JSON path "id_token_signing_alg_values_supported.0" equals "RS256"
