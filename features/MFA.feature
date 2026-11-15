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

  Scenario: A wrong OTP does not burn the mfa_token
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
    When I exchange the mfa_token with grant "http://auth0.com/oauth/grant-type/mfa-otp" and form body:
      """
      otp=123456
      client_id=demo
      """
    Then I receive a 200 response
    And the access_token claim "gty" equals "mfa-otp"

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
    And I reset all mock state
    When I send "GET /admin0/mfa-required" without a bearer
    Then I receive a 200 response
    And the response JSON path "required" equals "false"

  Scenario: Per-user required applies while the global switch is off
    When I PUT "/admin0/mfa/users/alice@example.com" with body:
      """
      {"required":true,"authenticators":[{"type":"otp"}]}
      """
    Then I receive a 200 response
    When I post to "/oauth/token" with form body:
      """
      grant_type=password
      client_id=demo
      username=alice@example.com
      password=ignored
      """
    Then I receive a 403 response
    And the response JSON path "error" equals "mfa_required"
    When I post to "/oauth/token" with form body:
      """
      grant_type=password
      client_id=demo
      username=bob@example.com
      password=ignored
      """
    Then I receive a 200 response
    And the response JSON path "access_token" exists

  Scenario: Per-user exemption applies while the global switch is on
    When I PUT "/admin0/mfa-required" with body:
      """
      {"required":true}
      """
    And I PUT "/admin0/mfa/users/alice@example.com" with body:
      """
      {"required":false}
      """
    And I post to "/oauth/token" with form body:
      """
      grant_type=password
      client_id=demo
      username=alice@example.com
      password=ignored
      """
    Then I receive a 200 response
    When I post to "/oauth/token" with form body:
      """
      grant_type=password
      client_id=demo
      username=bob@example.com
      password=ignored
      """
    Then I receive a 403 response
    And the response JSON path "error" equals "mfa_required"

  Scenario: A strict user without an active otp authenticator is rejected on mfa-otp
    When I PUT "/admin0/mfa/users/alice@example.com" with body:
      """
      {"required":true,"authenticators":[{"type":"oob","oob_channel":"sms"}]}
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
      otp=123456
      client_id=demo
      """
    Then I receive a 403 response
    And the response JSON path "error" equals "invalid_grant"
    And the response JSON path "error_description" equals "User does not have an active otp authenticator"

  Scenario: A strict user challenges an sms authenticator, then passes mfa-oob
    When I PUT "/admin0/mfa/users/alice@example.com" with body:
      """
      {"required":true,"authenticators":[{"type":"oob","oob_channel":"sms"}]}
      """
    And I post to "/oauth/token" with form body:
      """
      grant_type=password
      client_id=demo
      username=alice@example.com
      password=ignored
      """
    And I save the response JSON path "mfa_token" as "mfa_token"
    And I post to "/mfa/challenge" with form body using saved values:
      """
      mfa_token={{mfa_token}}
      client_id=demo
      challenge_type=oob
      """
    Then I receive a 200 response
    And the response JSON path "challenge_type" equals "oob"
    And the response JSON path "binding_method" equals "prompt"
    And the response JSON path "oob_code" exists
    When I save the response JSON path "oob_code" as "oob_code"
    And I post to "/oauth/token" with form body using saved values:
      """
      grant_type=http://auth0.com/oauth/grant-type/mfa-oob
      mfa_token={{mfa_token}}
      oob_code={{oob_code}}
      binding_code=123456
      client_id=demo
      """
    Then I receive a 200 response
    And the access_token claim "gty" equals "mfa-oob"

  Scenario: A strict user with no authenticators gets association_required on challenge
    When I PUT "/admin0/mfa/users/alice@example.com" with body:
      """
      {"required":true,"authenticators":[]}
      """
    And I post to "/oauth/token" with form body:
      """
      grant_type=password
      client_id=demo
      username=alice@example.com
      password=ignored
      """
    And I save the response JSON path "mfa_token" as "mfa_token"
    And I post to "/mfa/challenge" with form body using saved values:
      """
      mfa_token={{mfa_token}}
      client_id=demo
      challenge_type=otp
      """
    Then I receive a 400 response
    And the response JSON path "error" equals "association_required"

  Scenario: Reset clears per-user MFA config
    When I PUT "/admin0/mfa/users/alice@example.com" with body:
      """
      {"required":true,"authenticators":[{"type":"otp"}]}
      """
    And I GET "/admin0/mfa/users/alice@example.com"
    Then I receive a 200 response
    When I reset all mock state
    And I GET "/admin0/mfa/users/alice@example.com"
    Then I receive a 404 response
    And the response JSON path "errorCode" equals "unknown_user"
