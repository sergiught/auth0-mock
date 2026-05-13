Feature: MFA challenge flow
  Auth0 enforces MFA via a two-step /oauth/token dance: the initial
  password (or password-realm) grant returns 403 with an mfa_token, then
  the client re-calls with one of the three MFA grants
  (mfa-otp, mfa-oob, mfa-recovery-code) plus the user's factor.

  Background:
    Given the mock is running

  Scenario: With MFA disabled, password grant mints directly (regression)
    When I post to "/oauth/token" with form body:
      """
      grant_type=password
      client_id=demo
      username=alice@example.com
      password=ignored
      audience=http://example/api/v2/
      """
    Then I receive a 200 response
    And the response JSON path "access_token" exists

  Scenario: With MFA enabled, password grant returns 403 mfa_required
    When I PUT "/admin0/mfa-required" with body:
      """
      {"required":true}
      """
    Then I receive a 204 response
    When I post to "/oauth/token" with form body:
      """
      grant_type=password
      client_id=demo
      username=alice@example.com
      password=ignored
      audience=http://example/api/v2/
      """
    Then I receive a 403 response
    And the response JSON path "error" equals "mfa_required"
    And the response JSON path "mfa_token" exists

  Scenario: mfa-otp grant with accepted OTP mints a stepped-up token
    When I PUT "/admin0/mfa-required" with body:
      """
      {"required":true}
      """
    And I post to "/oauth/token" with form body:
      """
      grant_type=password
      client_id=demo
      username=alice@example.com
      password=ignored
      audience=http://example/api/v2/
      scope=read:users
      """
    And I save the mfa_token from the response
    And I exchange the mfa_token with grant "http://auth0.com/oauth/grant-type/mfa-otp" and form body:
      """
      otp=123456
      client_id=demo
      """
    Then I receive a 200 response
    And the response JSON path "access_token" exists
    And the access_token claim "gty" equals "mfa-otp"

  Scenario: mfa-otp with wrong OTP is rejected with invalid_grant
    When I PUT "/admin0/mfa-required" with body:
      """
      {"required":true}
      """
    And I post to "/oauth/token" with form body:
      """
      grant_type=password
      client_id=demo
      username=alice@example.com
      password=ignored
      """
    And I save the mfa_token from the response
    And I exchange the mfa_token with grant "http://auth0.com/oauth/grant-type/mfa-otp" and form body:
      """
      otp=wrong
      client_id=demo
      """
    Then I receive a 403 response
    And the response JSON path "error" equals "invalid_grant"

  Scenario: mfa-oob grant with binding_code mints a stepped-up token
    When I PUT "/admin0/mfa-required" with body:
      """
      {"required":true}
      """
    And I post to "/oauth/token" with form body:
      """
      grant_type=password
      client_id=demo
      username=alice@example.com
      password=ignored
      """
    And I save the mfa_token from the response
    And I exchange the mfa_token with grant "http://auth0.com/oauth/grant-type/mfa-oob" and form body:
      """
      oob_code=push-abc
      binding_code=123456
      client_id=demo
      """
    Then I receive a 200 response
    And the access_token claim "gty" equals "mfa-oob"

  Scenario: mfa-recovery-code grant accepts the canned recovery code
    When I PUT "/admin0/mfa-required" with body:
      """
      {"required":true}
      """
    And I post to "/oauth/token" with form body:
      """
      grant_type=password
      client_id=demo
      username=alice@example.com
      password=ignored
      """
    And I save the mfa_token from the response
    And I exchange the mfa_token with grant "http://auth0.com/oauth/grant-type/mfa-recovery-code" and form body:
      """
      recovery_code=ABCDEFGHIJKLMNOP
      client_id=demo
      """
    Then I receive a 200 response
    And the access_token claim "gty" equals "mfa-recovery-code"

  Scenario: Unknown mfa_token is rejected with invalid_grant
    When I PUT "/admin0/mfa-required" with body:
      """
      {"required":true}
      """
    And I post to "/oauth/token" with form body:
      """
      grant_type=http://auth0.com/oauth/grant-type/mfa-otp
      mfa_token=does-not-exist
      otp=123456
      client_id=demo
      """
    Then I receive a 403 response
    And the response JSON path "error" equals "invalid_grant"

  Scenario: Global /admin0/reset clears the MFA flag
    When I PUT "/admin0/mfa-required" with body:
      """
      {"required":true}
      """
    And I reset all matches
    When I send "GET /admin0/mfa-required" without a bearer
    Then I receive a 200 response
    And the response JSON path "required" equals "false"
