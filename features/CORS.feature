Feature: CORS
  As a developer testing a browser app against auth0-mock
  I want every route to answer CORS preflights and echo allowed origins
  So that SPA SDKs can call the mock from a browser

  Background:
    Given the mock is running

  Scenario: A preflight from an allowed origin is answered
    When I send "OPTIONS /oauth/token" with headers:
      | Origin                         | http://app.test |
      | Access-Control-Request-Method  | POST            |
    Then I receive a 204 response
    And the response header "Access-Control-Allow-Origin" equals "http://app.test"
    And the response header "Access-Control-Allow-Methods" equals "GET, POST, PUT, PATCH, DELETE, OPTIONS"

  Scenario: A wildcard origin entry matches one subdomain level
    When I send "GET /.well-known/openid-configuration" with headers:
      | Origin | https://spa.example.test |
    Then I receive a 200 response
    And the response header "Access-Control-Allow-Origin" equals "https://spa.example.test"

  Scenario: A preflight from an unlisted origin is rejected
    When I send "OPTIONS /oauth/token" with headers:
      | Origin                         | https://evil.test |
      | Access-Control-Request-Method  | POST              |
    Then I receive a 403 response
    And the response header "Access-Control-Allow-Origin" does not exist
