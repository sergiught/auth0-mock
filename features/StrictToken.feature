Feature: Strict /oauth/token mode

  Scenario: Strict mode rejects a reused code
    Given the mock is running with TOKEN_ENDPOINT_STRICT
    When I send "GET /authorize?client_id=demo&redirect_uri=https://app/cb&response_type=code&scope=openid%20offline_access" without a bearer
    And I save the code from the redirect as "c1"
    And I post to "/oauth/token" with form body using saved values:
      """
      grant_type=authorization_code
      client_id=demo
      code={{c1}}
      redirect_uri=https://app/cb
      """
    And I receive a 200 response
    And I post to "/oauth/token" with form body using saved values:
      """
      grant_type=authorization_code
      client_id=demo
      code={{c1}}
      redirect_uri=https://app/cb
      """
    Then I receive a 403 response
    And the response JSON path "error" equals "invalid_grant"
    And the response JSON path "error_description" equals "Invalid authorization code"

  Scenario: Strict mode rejects a different redirect_uri
    Given the mock is running with TOKEN_ENDPOINT_STRICT
    When I send "GET /authorize?client_id=demo&redirect_uri=https://app/cb&response_type=code&scope=openid%20offline_access" without a bearer
    And I save the code from the redirect as "c1"
    And I post to "/oauth/token" with form body using saved values:
      """
      grant_type=authorization_code
      client_id=demo
      code={{c1}}
      redirect_uri=https://other/cb
      """
    Then I receive a 403 response
    And the response JSON path "error" equals "invalid_grant"
    And the response JSON path "error_description" equals "Invalid authorization code"

  Scenario: Strict mode rejects a different client_id
    Given the mock is running with TOKEN_ENDPOINT_STRICT
    When I send "GET /authorize?client_id=demo&redirect_uri=https://app/cb&response_type=code&scope=openid%20offline_access" without a bearer
    And I save the code from the redirect as "c1"
    And I post to "/oauth/token" with form body using saved values:
      """
      grant_type=authorization_code
      client_id=other
      code={{c1}}
      redirect_uri=https://app/cb
      """
    Then I receive a 403 response
    And the response JSON path "error" equals "invalid_grant"
    And the response JSON path "error_description" equals "Invalid authorization code"

  Scenario: Strict mode omits refresh_token without offline_access
    Given the mock is running with TOKEN_ENDPOINT_STRICT
    When I post to "/oauth/token" with form body:
      """
      grant_type=password
      client_id=demo
      username=alice@example.com
      password=ignored
      scope=openid
      """
    Then I receive a 200 response
    And the response JSON path "refresh_token" does not exist

  Scenario: Strict mode issues refresh_token with offline_access
    Given the mock is running with TOKEN_ENDPOINT_STRICT
    When I post to "/oauth/token" with form body:
      """
      grant_type=password
      client_id=demo
      username=alice@example.com
      password=ignored
      scope=openid offline_access
      """
    Then I receive a 200 response
    And the response JSON path "refresh_token" exists

  Scenario: Lenient mode accepts a reused code
    Given the mock is running
    When I send "GET /authorize?client_id=demo&redirect_uri=https://app/cb&response_type=code&scope=openid%20offline_access" without a bearer
    And I save the code from the redirect as "c1"
    And I post to "/oauth/token" with form body using saved values:
      """
      grant_type=authorization_code
      client_id=demo
      code={{c1}}
      redirect_uri=https://app/cb
      """
    And I receive a 200 response
    And I post to "/oauth/token" with form body using saved values:
      """
      grant_type=authorization_code
      client_id=demo
      code={{c1}}
      redirect_uri=https://app/cb
      """
    Then I receive a 200 response

  Scenario: Lenient mode issues refresh_token without offline_access
    Given the mock is running
    When I post to "/oauth/token" with form body:
      """
      grant_type=password
      client_id=demo
      username=alice@example.com
      password=ignored
      scope=openid
      """
    Then I receive a 200 response
    And the response JSON path "refresh_token" exists
