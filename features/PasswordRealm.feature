Feature: password-realm grant
  Auth0-specific grant used by Native SDKs (auth0-android, auth0-swift,
  auth0-react-native) to authenticate against a specific connection
  by name. Behaves like the password grant, with realm threaded into
  the issued token's claims.

  Background:
    Given the mock is running

  Scenario: password-realm mints access_token + id_token with realm in claims
    When I post to "/oauth/token" with form body:
      """
      grant_type=http://auth0.com/oauth/grant-type/password-realm
      client_id=demo
      username=alice@example.com
      password=ignored
      realm=Username-Password-Authentication
      audience=http://example/api/v2/
      scope=openid profile email
      """
    Then I receive a 200 response
    And the response JSON path "access_token" exists
    And the response JSON path "id_token" exists
    And the response JSON path "refresh_token" exists
    And the access_token claim "gty" equals "password-realm"
    And the access_token claim "connection" equals "Username-Password-Authentication"

  Scenario: Missing realm is rejected with 400 invalid_request
    When I post to "/oauth/token" with form body:
      """
      grant_type=http://auth0.com/oauth/grant-type/password-realm
      client_id=demo
      username=alice@example.com
      password=ignored
      audience=http://example/api/v2/
      """
    Then I receive a 400 response
    And the response JSON path "error" equals "invalid_request"
