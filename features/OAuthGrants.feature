Feature: /oauth/token supports all four grant types
  Background:
    Given the mock is running

  Scenario: client_credentials grant returns access_token, no id_token
    When I post to "/oauth/token" with form body:
      """
      grant_type=client_credentials
      client_id=demo
      client_secret=x
      audience=http://example/api/v2/
      scope=read:users
      """
    Then I receive a 200 response
    And the response JSON path "access_token" exists
    And the response JSON path "token_type" equals "Bearer"
    And the response JSON path "scope" equals "read:users"

  Scenario: password grant returns access_token, id_token, and refresh_token
    When I post to "/oauth/token" with form body:
      """
      grant_type=password
      client_id=demo
      username=alice@example.com
      password=ignored
      audience=http://example/api/v2/
      scope=openid profile email
      """
    Then I receive a 200 response
    And the response JSON path "access_token" exists
    And the response JSON path "id_token" exists
    And the response JSON path "refresh_token" exists

  Scenario: refresh_token grant mints a new access_token
    When I post to "/oauth/token" with form body:
      """
      grant_type=refresh_token
      client_id=demo
      refresh_token=any-uuid
      """
    Then I receive a 200 response
    And the response JSON path "access_token" exists

  Scenario: authorization_code grant returns access_token and id_token
    When I post to "/oauth/token" with form body:
      """
      grant_type=authorization_code
      client_id=demo
      code=any-code
      redirect_uri=https://app/cb
      """
    Then I receive a 200 response
    And the response JSON path "access_token" exists
    And the response JSON path "id_token" exists

  Scenario: Missing grant_type is rejected with 400 invalid_request
    When I post to "/oauth/token" with body:
      """
      {}
      """
    Then I receive a 400 response
    And the response JSON path "error" equals "invalid_request"

  Scenario: Unknown grant_type is rejected with 400 unsupported_grant_type
    When I post to "/oauth/token" with body:
      """
      {"grant_type":"weird"}
      """
    Then I receive a 400 response
    And the response JSON path "error" equals "unsupported_grant_type"
