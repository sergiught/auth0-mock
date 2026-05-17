Feature: Mgmt API requires a bearer; admin surface does not
  Scenario: Missing bearer returns 401
    Given the mock is running
    When I send "GET /api/v2/users/auth0|123" without a bearer
    Then I receive a 401 response
    And the response body contains "missing_bearer"

  Scenario: /admin0/expectations accepts requests without a bearer
    Given the mock is running
    When I register an expectation for "GET /api/v2/users/auth0|123" with response:
      """
      {"status":200,"body":{"user_id":"auth0|123","email":"a@x"}}
      """
    Then I receive a 201 response
    And the response body contains "id"

  Scenario: clearing an expectation accepts requests without a bearer
    Given the mock is running
    And I register an expectation for "GET /api/v2/users/auth0|r" with response:
      """
      {"status":200,"body":{"user_id":"auth0|r","email":"r@x"}}
      """
    When I clear the expectation for "GET /api/v2/users/auth0|r"
    Then I receive a 204 response

  Scenario: GET /admin0/expectations accepts requests without a bearer
    Given the mock is running
    When I send "GET /admin0/expectations" without a bearer
    Then I receive a 200 response
