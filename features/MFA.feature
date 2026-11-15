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

  Scenario: A strict user is rejected on mfa-oob with an oob_code the mock did not issue
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
    And I exchange the mfa_token with grant "http://auth0.com/oauth/grant-type/mfa-oob" and form body:
      """
      oob_code=bogus
      binding_code=123456
      client_id=demo
      """
    Then I receive a 403 response
    And the response JSON path "error" equals "invalid_grant"
    And the response JSON path "error_description" equals "Invalid oob_code"

  Scenario: Enrolling an otp authenticator makes later logins challenge strictly
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
    And I send "POST /mfa/associate" with the saved mfa_token as bearer and body:
      """
      {"authenticator_types":["otp"]}
      """
    Then I receive a 200 response
    And the response JSON path "authenticator_type" equals "otp"
    And the response JSON path "recovery_codes" exists
    When I send "GET /mfa/authenticators" with the saved mfa_token as bearer
    Then I receive a 200 response
    And the response JSON path "#" equals "2"
    And the response JSON path "0.active" equals "false"
    And the response JSON path "1.active" equals "false"
    When I exchange the mfa_token with grant "http://auth0.com/oauth/grant-type/mfa-otp" and form body:
      """
      otp=123456
      client_id=demo
      """
    Then I receive a 200 response
    When I post to "/oauth/token" with form body:
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
    Then I receive a 200 response
    And the response JSON path "challenge_type" equals "otp"

  Scenario: Enrolling an sms authenticator activates it after the binding code
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
    And I save the response JSON path "mfa_token" as "mfa_token"
    And I send "POST /mfa/associate" with the saved mfa_token as bearer and body:
      """
      {"authenticator_types":["oob"],"oob_channels":["sms"],"phone_number":"+15555550100"}
      """
    Then I receive a 200 response
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
    When I GET "/admin0/mfa/users/alice@example.com"
    Then I receive a 200 response
    And the response JSON path "authenticators.#(oob_channel==sms).active" equals "true"

  Scenario: Associating when already enrolled is rejected
    When I PUT "/admin0/mfa/users/alice@example.com" with body:
      """
      {"required":true,"authenticators":[{"type":"otp","active":true}]}
      """
    And I post to "/oauth/token" with form body:
      """
      grant_type=password
      client_id=demo
      username=alice@example.com
      password=ignored
      """
    And I save the mfa_token from the response
    And I send "POST /mfa/associate" with the saved mfa_token as bearer and body:
      """
      {"authenticator_types":["otp"]}
      """
    Then I receive a 403 response
    And the response JSON path "error" equals "access_denied"

  Scenario: An access token for the mfa audience lists authenticators
    When I PUT "/admin0/mfa/users/alice@example.com" with body:
      """
      {"required":false,"authenticators":[{"type":"otp","active":true}]}
      """
    And I GET "/.well-known/openid-configuration"
    And I save the response JSON path "issuer" as "issuer"
    And I post to "/oauth/token" with form body using saved values:
      """
      grant_type=password
      client_id=demo
      username=alice@example.com
      password=ignored
      audience={{issuer}}mfa/
      scope=read:authenticators
      """
    Then I receive a 200 response
    When I save the access_token as my bearer
    And I send "GET /mfa/authenticators" with a valid bearer
    Then I receive a 200 response
    And the response JSON path "#" equals "1"
    And the response JSON path "0.authenticator_type" equals "otp"

  Scenario: An access token with remove scope deletes an authenticator once
    When I PUT "/admin0/mfa/users/alice@example.com" with body:
      """
      {"required":false,"authenticators":[{"type":"otp","id":"totp|dev1","active":true}]}
      """
    And I GET "/.well-known/openid-configuration"
    And I save the response JSON path "issuer" as "issuer"
    And I post to "/oauth/token" with form body using saved values:
      """
      grant_type=password
      client_id=demo
      username=alice@example.com
      password=ignored
      audience={{issuer}}mfa/
      scope=remove:authenticators
      """
    And I save the access_token as my bearer
    And I send "DELETE /mfa/authenticators/totp%7Cdev1" with a valid bearer
    Then I receive a 204 response
    When I send "DELETE /mfa/authenticators/totp%7Cdev1" with a valid bearer
    Then I receive a 404 response
    And the response JSON path "error" equals "not_found"

  Scenario: An access token for another audience cannot list authenticators
    When I post to "/oauth/token" with form body:
      """
      grant_type=password
      client_id=demo
      username=alice@example.com
      password=ignored
      audience=http://example/api/v2/
      """
    And I save the access_token as my bearer
    And I send "GET /mfa/authenticators" with a valid bearer
    Then I receive a 401 response
    And the response JSON path "error" equals "invalid_token"
