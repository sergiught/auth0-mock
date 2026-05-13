Feature: Authentication API endpoints
  Background:
    Given the mock is running

  Scenario: /authorize redirects to redirect_uri with a code and preserved state
    When I send "GET /authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&state=s1&response_type=code" without a bearer
    Then I receive a 302 response
    And the response Location header contains "https://app/cb"
    And the response Location header contains "code="
    And the response Location header contains "state=s1"

  Scenario: /authorize without redirect_uri is 400
    When I send "GET /authorize?client_id=abc" without a bearer
    Then I receive a 400 response
    And the response body contains "invalid_request"

  Scenario: /userinfo returns the bearer's claims
    Given I have a valid bearer token
    When I send "GET /userinfo" with a valid bearer
    Then I receive a 200 response
    And the response JSON path "sub" exists

  Scenario: /userinfo without bearer is 401
    When I send "GET /userinfo" without a bearer
    Then I receive a 401 response

  Scenario: /v2/logout redirects to returnTo
    When I send "GET /v2/logout?returnTo=https%3A%2F%2Fapp%2Fbye" without a bearer
    Then I receive a 302 response
    And the response Location header contains "https://app/bye"

  Scenario: /oauth/revoke is a no-op 200
    When I send "POST /oauth/revoke" without a bearer
    Then I receive a 200 response
