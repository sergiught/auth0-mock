Feature: Reset operations
  Background:
    Given the mock is running

  Scenario: Per-endpoint reset clears only that match
    Given I have a valid bearer token
    And I register "GET /api/v2/users/auth0|a/match" with body:
      """
      {"status":200,"body":{"user_id":"auth0|a","email":"a@x"}}
      """
    And I register "GET /api/v2/users/auth0|b/match" with body:
      """
      {"status":200,"body":{"user_id":"auth0|b","email":"b@x"}}
      """
    When I reset "GET /api/v2/users/auth0|a/reset"
    Then I receive a 204 response
    When I send "GET /api/v2/users/auth0|a" with a valid bearer
    Then I receive a 404 response
    When I send "GET /api/v2/users/auth0|b" with a valid bearer
    Then I receive a 200 response

  Scenario: Global /admin0/reset wipes everything
    Given I have a valid bearer token
    And I register "GET /api/v2/users/auth0|a/match" with body:
      """
      {"status":200,"body":{"user_id":"auth0|a","email":"a@x"}}
      """
    And I register "GET /api/v2/users/auth0|b/match" with body:
      """
      {"status":200,"body":{"user_id":"auth0|b","email":"b@x"}}
      """
    When I reset all matches
    And I list registered matches
    Then the matches list has 0 entries

  Scenario: GET /admin0/matches lists currently registered matches
    Given I register "GET /api/v2/users/auth0|x/match" with body:
      """
      {"status":200,"body":{"user_id":"auth0|x","email":"x@x"}}
      """
    When I list registered matches
    Then I receive a 200 response
    And the matches list has 1 entries
    And the matches list contains "GET /api/v2/users/auth0|x"
