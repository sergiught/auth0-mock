Feature: Mgmt API requires a bearer token
  Scenario: Missing bearer returns 401
    Given the mock is running
    When I send "GET /api/v2/users/auth0|123" without a bearer
    Then I receive a 401 response
    And the response body contains "missing_bearer"

  Scenario: /match does not require a bearer
    Given the mock is running
    And I register "GET /api/v2/users/auth0|123/match" with body:
      """
      {"status":200,"body":{"user_id":"auth0|123","email":"a@x"}}
      """
    Then I receive a 204 response
