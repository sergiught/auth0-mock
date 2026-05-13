Feature: Passwordless authentication
  Background:
    Given the mock is running

  Scenario: start returns an id
    When I post to "/passwordless/start" with body:
      """
      {"client_id":"demo","connection":"email","email":"alice@example.com","send":"code"}
      """
    Then I receive a 200 response
    And the response JSON path "_id" exists

  Scenario: verify with the accepted OTP mints an access_token
    When I post to "/passwordless/verify" with form body:
      """
      grant_type=http://auth0.com/oauth/grant-type/passwordless/otp
      client_id=demo
      realm=email
      username=alice@example.com
      otp=000000
      """
    Then I receive a 200 response
    And the response JSON path "access_token" exists
    And the response JSON path "token_type" equals "Bearer"

  Scenario: verify with a wrong OTP is 403 invalid_grant
    When I post to "/passwordless/verify" with form body:
      """
      grant_type=http://auth0.com/oauth/grant-type/passwordless/otp
      client_id=demo
      realm=email
      username=alice@example.com
      otp=wrong
      """
    Then I receive a 403 response
    And the response JSON path "error" equals "invalid_grant"
