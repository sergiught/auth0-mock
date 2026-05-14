Feature: Reset operations
  Background:
    Given the mock is running

  Scenario: Clearing one expectation leaves the others
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/users/auth0|a" with response:
      """
      {"status":200,"body":{"user_id":"auth0|a","email":"a@x"}}
      """
    And I register an expectation for "GET /api/v2/users/auth0|b" with response:
      """
      {"status":200,"body":{"user_id":"auth0|b","email":"b@x"}}
      """
    When I clear the expectation for "GET /api/v2/users/auth0|a"
    Then I receive a 204 response
    When I send "GET /api/v2/users/auth0|a" with a valid bearer
    Then I receive a 404 response
    When I send "GET /api/v2/users/auth0|b" with a valid bearer
    Then I receive a 200 response

  Scenario: Clearing all expectations empties the list
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/users/auth0|a" with response:
      """
      {"status":200,"body":{"user_id":"auth0|a","email":"a@x"}}
      """
    And I register an expectation for "GET /api/v2/users/auth0|b" with response:
      """
      {"status":200,"body":{"user_id":"auth0|b","email":"b@x"}}
      """
    When I clear all expectations
    And I list registered expectations
    Then the expectations list has 0 entries

  Scenario: Global /admin0/reset also wipes expectations
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/users/auth0|a" with response:
      """
      {"status":200,"body":{"user_id":"auth0|a","email":"a@x"}}
      """
    When I reset all mock state
    And I list registered expectations
    Then the expectations list has 0 entries

  Scenario: GET /admin0/expectations lists currently registered expectations
    Given I register an expectation for "GET /api/v2/users/auth0|x" with response:
      """
      {"status":200,"body":{"user_id":"auth0|x","email":"x@x"}}
      """
    When I list registered expectations
    Then I receive a 200 response
    And the expectations list has 1 entries
    And the expectations list contains "GET /api/v2/users/auth0|x"
