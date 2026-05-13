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
    And the response JSON path "user_id" equals "auth0|123"
    And the response JSON path "email" equals "a@x"

  Scenario: Registered headers come through on the response
    Given the mock is running
    And I have a valid bearer token
    And I register "GET /api/v2/users/auth0|h/match" with body:
      """
      {"status":200,"headers":{"X-RateLimit-Limit":"50"},"body":{"user_id":"auth0|h","email":"h@x"}}
      """
    When I send "GET /api/v2/users/auth0|h" with a valid bearer
    Then I receive a 200 response
    And the response header "X-RateLimit-Limit" equals "50"

  Scenario: Match payload violating the response schema is rejected
    Given the mock is running
    When I attempt to register "GET /api/v2/users/auth0|bad/match" with body:
      """
      {"status":200,"body":"not-an-object"}
      """
    Then I receive a 400 response
    And the response body contains "invalid_match"

  Scenario: Match with status not declared by the spec is rejected
    Given the mock is running
    When I attempt to register "GET /api/v2/users/auth0|undeclared/match" with body:
      """
      {"status":418,"body":{"user_id":"x","email":"y"}}
      """
    Then I receive a 400 response

  Scenario: Match registration with missing status field is rejected
    Given the mock is running
    When I attempt to register "GET /api/v2/users/auth0|nostatus/match" with body:
      """
      {"body":{"user_id":"x","email":"y"}}
      """
    Then I receive a 400 response
    And the response body contains "status is required"
