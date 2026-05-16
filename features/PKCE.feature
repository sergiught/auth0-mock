Feature: PKCE validation on authorization_code grant
  Auth0 SPA and native SDKs use PKCE (RFC 7636) to bind a /authorize
  redirect to its later /oauth/token exchange. The mock must enforce that
  link the same way real Auth0 does.

  Background:
    Given the mock is running

  Scenario: Correct S256 verifier mints a token
    When I start /authorize with code_verifier "the-quick-brown-fox-jumps-over-the-lazy-dog-43"
    Then I receive a 302 response
    And the response Location header contains "code="
    When I exchange the code with verifier "the-quick-brown-fox-jumps-over-the-lazy-dog-43"
    Then I receive a 200 response
    And the response JSON path "access_token" exists

  Scenario: Wrong verifier is rejected with invalid_grant
    When I start /authorize with code_verifier "the-original-verifier-43-characters-long-yes-ok"
    And I exchange the code with verifier "a-different-verifier-43-characters-long-too-ok"
    Then I receive a 400 response
    And the response JSON path "error" equals "invalid_grant"

  Scenario: Missing verifier when challenge was set is rejected
    When I start /authorize with code_verifier "the-original-verifier-43-characters-long-yes-ok"
    And I exchange the code without a verifier
    Then I receive a 400 response
    And the response JSON path "error" equals "invalid_grant"

  Scenario: Codes without a stored challenge still mint (backward compat)
    When I post to "/oauth/token" with form body:
      """
      grant_type=authorization_code
      client_id=demo
      code=any-code-never-seen-on-authorize
      redirect_uri=https://app/cb
      """
    Then I receive a 200 response
    And the response JSON path "access_token" exists

  Scenario: Verifier supplied for an unknown code is rejected (PKCE-bypass guard)
    # If the client sends code_verifier, PKCE is in play — the server having
    # no stashed challenge means the code is stolen, expired, or never came
    # from a PKCE /authorize. Either way, fail closed instead of minting.
    When I post to "/oauth/token" with form body:
      """
      grant_type=authorization_code
      client_id=demo
      code=stolen-or-expired-code-with-no-stashed-challenge
      code_verifier=the-attacker-supplied-verifier-43-chars-long
      redirect_uri=https://app/cb
      """
    Then I receive a 400 response
    And the response JSON path "error" equals "invalid_grant"
