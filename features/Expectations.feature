Feature: Expectation registration and lookup
  As a developer using auth0-mock
  I want to register canned responses for Auth0 Mgmt API endpoints
  So that my tests are deterministic

  Scenario: Concrete URL expectation returns the registered body
    Given the mock is running
    And I have a valid bearer token
    And I register an expectation for "GET /api/v2/users/auth0|123" with response:
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
    And I register an expectation for "GET /api/v2/users/auth0|h" with response:
      """
      {"status":200,"headers":{"X-RateLimit-Limit":"50"},"body":{"user_id":"auth0|h","email":"h@x"}}
      """
    When I send "GET /api/v2/users/auth0|h" with a valid bearer
    Then I receive a 200 response
    And the response header "X-RateLimit-Limit" equals "50"

  Scenario: Expectation body violating the response schema is rejected
    Given the mock is running
    When I attempt to register an expectation for "GET /api/v2/users/auth0|bad" with response:
      """
      {"status":200,"body":"not-an-object"}
      """
    Then I receive a 400 response
    And the response body contains "invalid_match"

  Scenario: Expectation with status not declared by the spec is rejected
    Given the mock is running
    When I attempt to register an expectation for "GET /api/v2/users/auth0|undeclared" with response:
      """
      {"status":418,"body":{"user_id":"x","email":"y"}}
      """
    Then I receive a 400 response

  Scenario: Expectation with missing status field is rejected
    Given the mock is running
    When I attempt to register an expectation for "GET /api/v2/users/auth0|nostatus" with response:
      """
      {"body":{"user_id":"x","email":"y"}}
      """
    Then I receive a 400 response
    And the response body contains "status is required"

  Scenario: Expectation for an unknown operation is rejected
    Given the mock is running
    When I attempt to register an expectation for "GET /api/v2/not-a-real-endpoint" with response:
      """
      {"status":200,"body":{}}
      """
    Then I receive a 400 response
    And the response body contains "unknown_operation"

  Scenario: A request-body matcher routes to a specific response
    Given the mock is running
    And I have a valid bearer token
    And I register the expectation "POST /api/v2/users":
      """
      {"request":{"body":{"email":"a@x"}},"response":{"status":201,"body":{"user_id":"auth0|a","email":"a@x"}}}
      """
    And I register the expectation "POST /api/v2/users":
      """
      {"request":{"body":{"email":"b@x"}},"response":{"status":201,"body":{"user_id":"auth0|b","email":"b@x"}}}
      """
    When I send "POST /api/v2/users" with body and a valid bearer:
      """
      {"email":"b@x","connection":"Username-Password-Authentication"}
      """
    Then I receive a 201 response
    And the response JSON path "user_id" equals "auth0|b"

  Scenario: A request-matched expectation beats a catch-all
    Given the mock is running
    And I have a valid bearer token
    And I register an expectation for "POST /api/v2/users" with response:
      """
      {"status":201,"body":{"user_id":"auth0|catchall"}}
      """
    And I register the expectation "POST /api/v2/users":
      """
      {"request":{"body":{"email":"specific@x"}},"response":{"status":201,"body":{"user_id":"auth0|specific"}}}
      """
    When I send "POST /api/v2/users" with body and a valid bearer:
      """
      {"email":"specific@x","connection":"Username-Password-Authentication"}
      """
    Then I receive a 201 response
    And the response JSON path "user_id" equals "auth0|specific"

  Scenario: A request the specific matcher rejects falls back to the catch-all
    Given the mock is running
    And I have a valid bearer token
    And I register an expectation for "POST /api/v2/users" with response:
      """
      {"status":201,"body":{"user_id":"auth0|catchall"}}
      """
    And I register the expectation "POST /api/v2/users":
      """
      {"request":{"body":{"email":"specific@x"}},"response":{"status":201,"body":{"user_id":"auth0|specific"}}}
      """
    When I send "POST /api/v2/users" with body and a valid bearer:
      """
      {"email":"someone-else@x","connection":"Username-Password-Authentication"}
      """
    Then I receive a 201 response
    And the response JSON path "user_id" equals "auth0|catchall"

  Scenario: A request matcher with unknown fields is rejected at registration
    Given the mock is running
    When I attempt to register the expectation "POST /api/v2/users":
      """
      {"request":{"body":{"hello":"hola"}},"response":{"status":201,"body":{"user_id":"auth0|x"}}}
      """
    Then I receive a 400 response
    And the response body contains "invalid_request_match"

  Scenario: A query-parameter matcher routes to a specific response
    Given the mock is running
    And I have a valid bearer token
    And I register the expectation "GET /api/v2/users":
      """
      {"request":{"query":{"q":"email:found@x"}},"response":{"status":200,"body":[{"user_id":"auth0|found"}]}}
      """
    When I send "GET /api/v2/users?q=email:found@x" with a valid bearer
    Then I receive a 200 response
    And the response JSON path "0.user_id" equals "auth0|found"
