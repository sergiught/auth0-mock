Feature: Concrete-first, template fallback
  Scenario: Template registration serves any concrete id
    Given the mock is running
    And I have a valid bearer token
    And I register "GET /api/v2/users/{id}/match" with body:
      """
      {"status":200,"body":{"user_id":"auth0|*","email":"any@x"}}
      """
    When I send "GET /api/v2/users/auth0|999" with a valid bearer
    Then I receive a 200 response
    And the response JSON path "email" equals "any@x"
    When I send "GET /api/v2/clients" with a valid bearer
    Then I receive a 404 response

  Scenario: Concrete match wins over template match
    Given the mock is running
    And I have a valid bearer token
    And I register "GET /api/v2/users/{id}/match" with body:
      """
      {"status":200,"body":{"user_id":"auth0|*","email":"template@x"}}
      """
    And I register "GET /api/v2/users/auth0|specific/match" with body:
      """
      {"status":200,"body":{"user_id":"auth0|specific","email":"exact@x"}}
      """
    When I send "GET /api/v2/users/auth0|specific" with a valid bearer
    Then I receive a 200 response
    And the response JSON path "email" equals "exact@x"
    When I send "GET /api/v2/users/auth0|other" with a valid bearer
    Then I receive a 200 response
    And the response JSON path "email" equals "template@x"
