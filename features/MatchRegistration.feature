Feature: Match registration and lookup
  As a developer using auth0-mock
  I want to register canned responses for Auth0 Mgmt API endpoints
  So that my tests are deterministic

  Scenario: Concrete URL match returns the registered body
    Given the mock is running
    And I have a valid bearer token
    And I register "GET /api/v2/users/auth0|123/match" with body:
      """
      {"status":200,"body":{"user_id":"auth0|123","email":"a@x"}}
      """
    When I send "GET /api/v2/users/auth0|123" with a valid bearer
    Then I receive a 200 response
    And the response body contains "auth0|123"
