Feature: ISSUER_URL=auto derives the issuer from the request address
  Scenario: Discovery follows the address it is fetched from
    Given the mock is running with ISSUER_URL "auto"
    And requests are sent with Host "other.test:1234"
    When I send "GET /.well-known/openid-configuration" without a bearer
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      {
        "issuer":                 "http://other.test:1234/",
        "authorization_endpoint": "http://other.test:1234/authorize",
        "token_endpoint":         "http://other.test:1234/oauth/token",
        "userinfo_endpoint":      "http://other.test:1234/userinfo",
        "jwks_uri":               "http://other.test:1234/.well-known/jwks.json",
        "end_session_endpoint":   "http://other.test:1234/oidc/logout",
        "revocation_endpoint":    "http://other.test:1234/oauth/revoke",
        "response_types_supported":              "<<PRESENCE>>",
        "subject_types_supported":               "<<PRESENCE>>",
        "id_token_signing_alg_values_supported": "<<PRESENCE>>",
        "token_endpoint_auth_methods_supported": "<<PRESENCE>>",
        "scopes_supported":                      "<<PRESENCE>>",
        "grant_types_supported":                 "<<PRESENCE>>",
        "code_challenge_methods_supported":      "<<PRESENCE>>",
        "response_modes_supported":              "<<PRESENCE>>",
        "claims_supported":                      "<<PRESENCE>>"
      }
      """

  Scenario: client_credentials tokens carry the request address as iss
    Given the mock is running with ISSUER_URL "auto"
    And requests are sent with Host "other.test:1234"
    When I post to "/oauth/token" with form body:
      """
      grant_type=client_credentials
      client_id=demo
      client_secret=x
      audience=http://example/api/v2/
      """
    Then I receive a 200 response
    And the access_token claim "iss" equals "http://other.test:1234/"
    And the access_token verifies against the published JWKS

  Scenario: A bearer minted at the same address is accepted
    Given the mock is running with ISSUER_URL "auto"
    And I have a valid bearer token
    When I send "GET /api/v2/users/auth0|123" with a valid bearer
    Then I receive a 404 response
    And the response body contains "no_match"

  Scenario: A bearer minted at another address is rejected
    Given the mock is running with ISSUER_URL "auto"
    And I have a valid bearer token minted via Host "other.test:1234"
    When I send "GET /api/v2/users/auth0|123" with a valid bearer
    Then I receive a 401 response
    And the response body contains "invalid_bearer"

  Scenario: A refreshed token carries the request address as iss
    Given the mock is running with ISSUER_URL "auto"
    And requests are sent with Host "other.test:1234"
    When I post to "/oauth/token" with form body:
      """
      grant_type=password
      client_id=demo
      username=alice@example.com
      password=ignored
      scope=openid offline_access
      """
    And I save the refresh_token from the response
    And I refresh with the saved refresh_token and form body:
      """
      client_id=demo
      """
    Then I receive a 200 response
    And the access_token claim "iss" equals "http://other.test:1234/"

  Scenario: The authorization_code id_token carries the request address as iss
    Given the mock is running with ISSUER_URL "auto"
    And requests are sent with Host "other.test:1234"
    When I start /authorize with code_verifier "the-quick-brown-fox-jumps-over-the-lazy-dog-43"
    And I exchange the code with verifier "the-quick-brown-fox-jumps-over-the-lazy-dog-43"
    Then I receive a 200 response
    And the id_token claim "iss" equals "http://other.test:1234/"

  Scenario: The implicit id_token carries the request address as iss
    Given the mock is running with ISSUER_URL "auto"
    And requests are sent with Host "other.test:1234"
    When I send "GET /authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=id_token%20token&nonce=n1" without a bearer
    Then I receive a 302 response
    And the redirect fragment id_token claim "iss" equals "http://other.test:1234/"

  Scenario: /userinfo accepts a bearer minted at the same address and rejects another
    Given the mock is running with ISSUER_URL "auto"
    And I have a valid bearer token
    When I send "GET /userinfo" with a valid bearer
    Then I receive a 200 response
    Given I have a valid bearer token minted via Host "other.test:1234"
    When I send "GET /userinfo" with a valid bearer
    Then I receive a 401 response

  Scenario: /oidc/logout accepts an id_token_hint minted at the same address and rejects another
    Given the mock is running with ISSUER_URL "auto"
    When I start /authorize with code_verifier "the-quick-brown-fox-jumps-over-the-lazy-dog-43"
    And I exchange the code with verifier "the-quick-brown-fox-jumps-over-the-lazy-dog-43"
    And I send "GET /oidc/logout?post_logout_redirect_uri=https%3A%2F%2Fapp%2Fbye&state=s1" with the id_token from the response as id_token_hint
    Then I receive a 302 response
    And the response Location header contains "https://app/bye?state=s1"
    Given I have a valid bearer token minted via Host "other.test:1234"
    When I send "GET /oidc/logout?post_logout_redirect_uri=https%3A%2F%2Fapp%2Fbye&state=s1" with the id_token from the response as id_token_hint
    Then I receive a 400 response

  Scenario: A bearer minted at an address is accepted at that address
    Given the mock is running with ISSUER_URL "auto"
    And I have a valid bearer token minted via Host "other.test:1234"
    And requests are sent with Host "other.test:1234"
    When I send "GET /api/v2/users/auth0|123" with a valid bearer
    Then I receive a 404 response
    And the response body contains "no_match"
