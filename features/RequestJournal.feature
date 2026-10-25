Feature: Request journal
  As a developer using auth0-mock
  I want to see what my application actually sent to the Management API
  So that I can assert on requests and debug stubs that did not match

  Background:
    Given the mock is running
    And I have a valid bearer token

  Scenario: A matched call is recorded with its body and outcome
    Given I register the expectation "PATCH /api/v2/users/auth0|j1":
      """
      {"request":{"body":{"blocked":true}},"response":{"status":200,"body":{"user_id":"auth0|j1","email":"j@x"}}}
      """
    When I send "PATCH /api/v2/users/auth0|j1" with body and a valid bearer:
      """
      {"blocked":true}
      """
    Then I receive a 200 response
    When I GET "/admin0/journal?method=PATCH"
    Then I receive a 200 response
    And the response JSON path "entries.0.body.blocked" equals "true"
    And the response JSON path "entries.0.outcome" equals "matched"

  Scenario: An unmatched call is explained in the 404 and in the journal
    Given I register the expectation "PATCH /api/v2/users/auth0|j2":
      """
      {"request":{"body":{"blocked":true}},"response":{"status":200,"body":{"user_id":"auth0|j2","email":"j@x"}}}
      """
    When I send "PATCH /api/v2/users/auth0|j2" with body and a valid bearer:
      """
      {"blocked":false}
      """
    Then I receive a 404 response
    And the response body contains "closest stub differed on"
    When I GET "/admin0/journal/unmatched"
    Then I receive a 200 response
    And the response JSON path "entries.0.nearMisses.0.differences.0.field" equals "body.blocked"

  Scenario: Global reset empties the journal
    Given I register the expectation "GET /api/v2/users/auth0|j3":
      """
      {"response":{"status":200,"body":{"user_id":"auth0|j3","email":"j@x"}}}
      """
    When I send "GET /api/v2/users/auth0|j3" with a valid bearer
    And I GET "/admin0/journal"
    Then the response JSON path "entries.#" equals "1"
    When I reset all mock state
    And I GET "/admin0/journal"
    Then the response JSON path "entries.#" equals "0"
