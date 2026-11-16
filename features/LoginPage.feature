Feature: Login page
  With LOGIN_PAGE on, a logged-out /authorize shows an Auth0-style login form
  instead of signing in silently. Any email and password logs in.

  Scenario: A logged-out authorize redirects to the login page
    Given the mock is running with LOGIN_PAGE
    When I send "GET /authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=code&state=s1" without a bearer
    Then I receive a 302 response
    And the response Location header contains "u/login?state="

  Scenario: The login page has the form fields
    Given the mock is running with LOGIN_PAGE
    When I send "GET /authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=code" without a bearer
    And I save the login state from the redirect as "tx"
    And I get "/u/login?state={{tx}}" using saved values
    Then I receive a 200 response
    And the response body has a form field named "username"
    And the response body has a form field named "password"
    And the response body has a form field named "action"

  Scenario: Submitting the form signs in as the email
    Given the mock is running with LOGIN_PAGE
    When I send "GET /authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=code&state=s1" without a bearer
    And I save the login state from the redirect as "tx"
    And I post to "/u/login?state={{tx}}" with form body using saved values:
      """
      username=jane@example.com
      password=anything
      action=default
      """
    Then I receive a 302 response
    And the response Location header contains "https://app/cb"
    And the response Location header contains "state=s1"
    And I save the code from the redirect as "code"
    When I post to "/oauth/token" with form body using saved values:
      """
      grant_type=authorization_code
      client_id=abc
      code={{code}}
      redirect_uri=https://app/cb
      """
    Then I receive a 200 response
    And the access_token claim "sub" equals "jane@example.com"

  Scenario: A failure rule on username shows an error
    Given the mock is running with LOGIN_PAGE
    And I POST "/admin0/authorize-errors" with body:
      """
      {"match":{"username":"blocked@example.com"},"error":"access_denied","error_description":"Blocked by rule"}
      """
    And I receive a 201 response
    When I send "GET /authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=code" without a bearer
    And I save the login state from the redirect as "tx"
    And I post to "/u/login?state={{tx}}" with form body using saved values:
      """
      username=blocked@example.com
      password=x
      action=default
      """
    Then I receive a 400 response
    And the response body contains "Blocked by rule"

  Scenario: With the page off, authorize goes straight to the redirect_uri
    Given the mock is running
    When I send "GET /authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&response_type=code" without a bearer
    Then I receive a 302 response
    And the response Location header contains "https://app/cb?code="
