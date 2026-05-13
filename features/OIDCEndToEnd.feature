Feature: OIDC end-to-end loop
  Scenario: Token from /oauth/token is signed and accepted by the Mgmt API
    Given the mock is running
    When I post to "/oauth/token" with form body:
      """
      grant_type=client_credentials
      client_id=demo
      client_secret=x
      audience=http://example/api/v2/
      """
    Then I receive a 200 response
    And the response JSON path "access_token" exists
    And the response JSON path "token_type" equals "Bearer"
    And the access_token verifies against the published JWKS
    And I save the access_token as my bearer
    When I send "GET /api/v2/users/auth0|loop" with a valid bearer
    Then I receive a 404 response
    And the response body contains "no_match"
