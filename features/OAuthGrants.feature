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
    And the response body should match the JSON pattern:
      """
      {
        "access_token": "<<PRESENCE>>",
        "token_type":   "Bearer",
        "expires_in":   "<<PRESENCE>>",
        "scope":        "read:users"
      }
      """

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
    And the response body should match the JSON pattern:
      """
      {
        "access_token":  "<<PRESENCE>>",
        "id_token":      "<<PRESENCE>>",
        "refresh_token": "<<PRESENCE>>",
        "token_type":    "Bearer",
        "expires_in":    "<<PRESENCE>>",
        "scope":         "openid profile email"
      }
      """

  Scenario: refresh_token grant mints a new access_token
    When I post to "/oauth/token" with form body:
      """
      grant_type=refresh_token
      client_id=demo
      refresh_token=any-uuid
      """
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      {
        "access_token": "<<PRESENCE>>",
        "token_type":   "Bearer",
        "expires_in":   "<<PRESENCE>>"
      }
      """

  Scenario: authorization_code grant returns access_token and id_token
    When I post to "/oauth/token" with form body:
      """
      grant_type=authorization_code
      client_id=demo
      code=any-code
      redirect_uri=https://app/cb
      """
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      {
        "access_token":  "<<PRESENCE>>",
        "id_token":      "<<PRESENCE>>",
        "refresh_token": "<<PRESENCE>>",
        "token_type":    "Bearer",
        "expires_in":    "<<PRESENCE>>"
      }
      """

  Scenario: Missing grant_type is rejected with 400 invalid_request
    When I post to "/oauth/token" with body:
      """
      {}
      """
    Then I receive a 400 response
    And the response body should match the JSON pattern:
      """
      {
        "error":             "invalid_request",
        "error_description": "<<PRESENCE>>"
      }
      """

  Scenario: Unknown grant_type is rejected with 400 unsupported_grant_type
    When I post to "/oauth/token" with body:
      """
      {"grant_type":"weird"}
      """
    Then I receive a 400 response
    And the response body should match the JSON pattern:
      """
      {
        "error":             "unsupported_grant_type",
        "error_description": "<<PRESENCE>>"
      }
      """
