Feature: Per-process custom JWT claims
  Background:
    Given the mock is running

  Scenario: Custom claims are merged into every minted access_token
    When I PUT "/admin0/claims" with body:
      """
      {"role":"admin","org_id":"o-42"}
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
    And the access_token claim "role" equals "admin"
    And the access_token claim "org_id" equals "o-42"

  Scenario: GET /admin0/claims returns the current map
    When I PUT "/admin0/claims" with body:
      """
      {"foo":"bar"}
      """
    And I send "GET /admin0/claims" without a bearer
    Then I receive a 200 response
    And the response JSON path "foo" equals "bar"

  Scenario: DELETE /admin0/claims clears the map
    When I PUT "/admin0/claims" with body:
      """
      {"role":"admin"}
      """
    And I DELETE "/admin0/claims"
    Then I receive a 204 response
    When I send "GET /admin0/claims" without a bearer
    Then I receive a 200 response
    And the response body contains "{}"

  Scenario: Invalid JSON body is rejected with 400
    When I PUT "/admin0/claims" with body:
      """
      not-json
      """
    Then I receive a 400 response
    And the response body contains "invalid_body"

  Scenario: Global /admin0/reset clears claims
    When I PUT "/admin0/claims" with body:
      """
      {"role":"admin"}
      """
    And I reset all mock state
    When I send "GET /admin0/claims" without a bearer
    Then I receive a 200 response
    And the response body contains "{}"
