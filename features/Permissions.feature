Feature: Per-audience permission injection
  Background:
    Given the mock is running

  Scenario: Permissions registered for an audience appear in tokens for that audience
    When I PUT "/admin0/permissions/http://example/api/v2/" with body:
      """
      ["read:users","write:users"]
      """
    Then I receive a 204 response
    When I post to "/oauth/token" with form body:
      """
      grant_type=client_credentials
      client_id=demo
      client_secret=x
      audience=http://example/api/v2/
      """
    Then I receive a 200 response
    And the access_token claim "permissions" array contains "read:users"
    And the access_token claim "permissions" array contains "write:users"

  Scenario: Different audiences carry different permissions
    When I PUT "/admin0/permissions/http://api1/" with body:
      """
      ["read:a"]
      """
    And I PUT "/admin0/permissions/http://api2/" with body:
      """
      ["read:b"]
      """
    When I post to "/oauth/token" with form body:
      """
      grant_type=client_credentials
      client_id=demo
      client_secret=x
      audience=http://api1/
      """
    Then the access_token claim "permissions" array contains "read:a"
    When I post to "/oauth/token" with form body:
      """
      grant_type=client_credentials
      client_id=demo
      client_secret=x
      audience=http://api2/
      """
    Then the access_token claim "permissions" array contains "read:b"

  Scenario: Token for an unregistered audience has no permissions claim
    When I post to "/oauth/token" with form body:
      """
      grant_type=client_credentials
      client_id=demo
      client_secret=x
      audience=http://no-permissions/
      """
    Then I receive a 200 response
    And the response JSON path "access_token" exists

  Scenario: GET single audience returns its permissions
    When I PUT "/admin0/permissions/http://api1/" with body:
      """
      ["read:a","write:a"]
      """
    And I send "GET /admin0/permissions/http://api1/" without a bearer
    Then I receive a 200 response
    And the response body contains "read:a"
    And the response body contains "write:a"

  Scenario: GET listing returns all audiences
    When I PUT "/admin0/permissions/http://api1/" with body:
      """
      ["x"]
      """
    And I PUT "/admin0/permissions/http://api2/" with body:
      """
      ["y"]
      """
    And I send "GET /admin0/permissions" without a bearer
    Then I receive a 200 response
    And the response body contains "http://api1/"
    And the response body contains "http://api2/"

  Scenario: DELETE single audience clears only that audience
    When I PUT "/admin0/permissions/http://api1/" with body:
      """
      ["x"]
      """
    And I PUT "/admin0/permissions/http://api2/" with body:
      """
      ["y"]
      """
    And I DELETE "/admin0/permissions/http://api1/"
    Then I receive a 204 response
    When I send "GET /admin0/permissions/http://api2/" without a bearer
    Then the response body contains "y"

  Scenario: DELETE /admin0/permissions clears everything
    When I PUT "/admin0/permissions/http://api1/" with body:
      """
      ["x"]
      """
    And I DELETE "/admin0/permissions"
    Then I receive a 204 response
    When I send "GET /admin0/permissions" without a bearer
    Then the response body contains "{}"

  Scenario: Global /admin0/reset clears all permissions
    When I PUT "/admin0/permissions/http://api1/" with body:
      """
      ["x"]
      """
    And I reset all matches
    When I send "GET /admin0/permissions" without a bearer
    Then the response body contains "{}"
