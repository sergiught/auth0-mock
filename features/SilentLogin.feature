Feature: Silent login
  A successful /authorize leaves an auth0 session cookie, so a later
  prompt=none request can be answered without showing a login again.

  Background:
    Given the mock is running
    And I keep cookies

  Scenario: A login sets the session cookie
    When I send "GET /authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=code" without a bearer
    Then I receive a 302 response
    And the response header "Set-Cookie" contains "auth0="
    And the response header "Set-Cookie" contains "HttpOnly"
    And the response header "Set-Cookie" contains "SameSite=Lax"

  Scenario: prompt=none without a session is login_required
    When I send "GET /authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=code&prompt=none" without a bearer
    Then I receive a 302 response
    And the response Location header contains "error=login_required"

  Scenario: prompt=none answers the session user and keeps the original auth_time
    Given I PUT "/admin0/clock" with body:
      """
      {"now":"2030-01-01T00:00:00Z"}
      """
    When I send "GET /authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=id_token&nonce=n1&login_hint=auth0%7Calice" without a bearer
    Then I receive a 302 response
    When I POST "/admin0/clock/advance" with body:
      """
      {"by":"1h"}
      """
    And I send "GET /authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=id_token&nonce=n2&prompt=none" without a bearer
    Then I receive a 302 response
    And the redirect fragment id_token claim "sub" equals "auth0|alice"
    And the redirect fragment id_token claim "auth_time" equals "1893456000"

  Scenario: web_message answers with an HTML page that posts the response
    Given I send "GET /authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=code" without a bearer
    When I send "GET /authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=code&response_mode=web_message&prompt=none" without a bearer
    Then I receive a 200 response
    And the response body contains "authorization_response"

  Scenario Outline: Logging out ends the session
    Given I send "GET /authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=code" without a bearer
    When I send "GET <logout>" without a bearer
    And I send "GET /authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=code&prompt=none" without a bearer
    Then I receive a 302 response
    And the response Location header contains "error=login_required"

    Examples:
      | logout                                                          |
      | /v2/logout                                                      |
      | /oidc/logout?client_id=abc                                      |

  Scenario: Resetting mock state forgets the session
    Given I send "GET /authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=code" without a bearer
    When I reset all mock state
    And I send "GET /authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=code&prompt=none" without a bearer
    Then I receive a 302 response
    And the response Location header contains "error=login_required"

  Scenario: An authorize-error rule wins over a live session
    Given I send "GET /authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=code" without a bearer
    And I POST "/admin0/authorize-errors" with body:
      """
      {"match":{"prompt":"none"},"error":"interaction_required","error_description":"Forced"}
      """
    When I send "GET /authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=code&prompt=none" without a bearer
    Then I receive a 302 response
    And the response Location header contains "error=interaction_required"
