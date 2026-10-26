Feature: Authentication API endpoints
  Background:
    Given the mock is running

  Scenario: /authorize redirects to redirect_uri with a code and preserved state
    When I send "GET /authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&state=s1&response_type=code" without a bearer
    Then I receive a 302 response
    And the response Location header contains "https://app/cb"
    And the response Location header contains "code="
    And the response Location header contains "state=s1"

  Scenario: A registered authorize error is sent back to redirect_uri
    Given I POST "/admin0/authorize-errors" with body:
      """
      {"match":{"client_id":"abc"},"error":"access_denied","error_description":"User cancelled"}
      """
    And I receive a 201 response
    When I send "GET /authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&state=s1&response_type=code" without a bearer
    Then I receive a 302 response
    And the response Location header contains "error=access_denied"
    And the response Location header contains "state=s1"

  Scenario: form_post delivers the error as an auto-submitting form
    Given I POST "/admin0/authorize-errors" with body:
      """
      {"match":{"client_id":"abc"},"error":"access_denied","error_description":"User cancelled"}
      """
    And I receive a 201 response
    When I send "GET /authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&state=s1&response_type=code&response_mode=form_post" without a bearer
    Then I receive a 200 response
    And the response body contains "document.forms[0].submit()"
    And the response body contains "access_denied"

  Scenario: Implicit tokens arrive in the fragment
    When I send "GET /authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=token" without a bearer
    Then I receive a 302 response
    And the response Location header contains "#access_token="
    And the response Location header contains "token_type=Bearer"

  Scenario: An id_token arrives in the fragment with the nonce
    When I send "GET /authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=id_token%20token&nonce=n1" without a bearer
    Then I receive a 302 response
    And the response Location header contains "id_token="
    And the redirect fragment id_token claim "nonce" equals "n1"

  Scenario: An unknown response_type is an error
    When I send "GET /authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=cod" without a bearer
    Then I receive a 302 response
    And the response Location header contains "error=unsupported_response_type"

  Scenario: An id_token without a nonce is refused
    When I send "GET /authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=id_token" without a bearer
    Then I receive a 302 response
    And the response Location header contains "error=invalid_request"

  Scenario: /authorize without redirect_uri is 400
    When I send "GET /authorize?client_id=abc" without a bearer
    Then I receive a 400 response
    And the response body contains "invalid_request"

  Scenario: /userinfo returns the bearer's claims
    Given I have a valid bearer token
    When I send "GET /userinfo" with a valid bearer
    Then I receive a 200 response
    And the response JSON path "sub" exists

  Scenario: /userinfo returns the code-flow user's profile
    When I start /authorize with login_hint "alice@example.com"
    And I exchange the code without a verifier
    And I save the access_token as my bearer
    And I send "GET /userinfo" with a valid bearer
    Then I receive a 200 response
    And the response JSON path "sub" equals "alice@example.com"
    And the response JSON path "nickname" equals "alice"
    And the response JSON path "email" equals "alice@example.com"

  Scenario: /userinfo returns the password-grant user's profile
    When I POST "/oauth/token" with body:
      """
      {"grant_type":"password","client_id":"demo","username":"alice@example.com","password":"pw"}
      """
    And I save the access_token as my bearer
    And I send "GET /userinfo" with a valid bearer
    Then I receive a 200 response
    And the response JSON path "nickname" equals "alice"

  Scenario: /admin0/reset forgets remembered profiles
    When I start /authorize with login_hint "alice@example.com"
    And I exchange the code without a verifier
    And I save the access_token as my bearer
    And I reset all mock state
    And I send "GET /userinfo" with a valid bearer
    Then I receive a 200 response
    And the response JSON path "nickname" does not exist

  Scenario: /userinfo without bearer is 401
    When I send "GET /userinfo" without a bearer
    Then I receive a 401 response

  Scenario: /v2/logout redirects to returnTo
    When I send "GET /v2/logout?returnTo=https%3A%2F%2Fapp%2Fbye" without a bearer
    Then I receive a 302 response
    And the response Location header contains "https://app/bye"

  Scenario: /oidc/logout after login redirects with state
    When I start /authorize with code_verifier "the-quick-brown-fox-jumps-over-the-lazy-dog-43"
    And I exchange the code with verifier "the-quick-brown-fox-jumps-over-the-lazy-dog-43"
    And I send "GET /oidc/logout?post_logout_redirect_uri=https%3A%2F%2Fapp%2Fbye&state=s1" with the id_token from the response as id_token_hint
    Then I receive a 302 response
    And the response Location header contains "https://app/bye?state=s1"

  Scenario: /oidc/logout without a redirect shows the logged-out page
    When I send "GET /oidc/logout" without a bearer
    Then I receive a 200 response
    And the response body contains "You have been logged out."

  Scenario: /oidc/logout refuses a redirect without a client
    When I send "GET /oidc/logout?post_logout_redirect_uri=https%3A%2F%2Fapp%2Fbye" without a bearer
    Then I receive a 400 response
    And the response JSON path "error_description" equals "post_logout_redirect_uri requires client_id or id_token_hint"

  Scenario: /oauth/revoke is a no-op 200
    When I send "POST /oauth/revoke" without a bearer
    Then I receive a 200 response
