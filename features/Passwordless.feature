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
    And the response JSON path "id_token" exists
    And the response JSON path "refresh_token" exists

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

  Scenario: the passwordless grant on /oauth/token returns an Auth0-style email user
    When I post to "/oauth/token" with form body:
      """
      grant_type=http://auth0.com/oauth/grant-type/passwordless/otp
      client_id=demo
      realm=email
      username=alice@example.com
      otp=000000
      """
    Then I receive a 200 response
    And the id_token claim "email" equals "alice@example.com"
    And the id_token claim "connection" equals "email"

  Scenario: the passwordless grant needs a realm
    When I post to "/oauth/token" with form body:
      """
      grant_type=http://auth0.com/oauth/grant-type/passwordless/otp
      client_id=demo
      username=alice@example.com
      otp=000000
      """
    Then I receive a 400 response
    And the response JSON path "error_description" equals "Missing required parameter: realm"

  Scenario: an SMS login has a phone profile
    When I post to "/oauth/token" with form body:
      """
      grant_type=http://auth0.com/oauth/grant-type/passwordless/otp
      client_id=demo
      realm=sms
      username=+15551234567
      otp=000000
      """
    Then I receive a 200 response
    And the id_token claim "phone_number" equals "+15551234567"
