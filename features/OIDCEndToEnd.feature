Feature: OIDC end-to-end
  Scenario: Token from /oauth/token verifies against the mock's JWKS
    Given the mock is running
    When I post to "/oauth/token" with body:
      """
      {"grant_type":"client_credentials","client_id":"abc","client_secret":"x","audience":"https://api/"}
      """
    Then I receive a 200 response
    And the response body contains "access_token"
