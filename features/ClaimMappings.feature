Feature: Request-parameter to claim projection
  Background:
    Given the mock is running

  Scenario: Mapped request parameter is projected into the minted access_token
    When I PUT "/admin0/claims/mappings" with body:
      """
      {"resource":"https://example.com/resource"}
      """
    Then I receive a 204 response
    When I post to "/oauth/token" with form body:
      """
      grant_type=client_credentials
      client_id=demo
      client_secret=x
      audience=http://example/api/v2/
      resource=urn:api:orders
      """
    Then I receive a 200 response
    And the access_token claim "https://example\.com/resource" equals "urn:api:orders"
    When I post to "/oauth/token" with form body:
      """
      grant_type=client_credentials
      client_id=demo
      client_secret=x
      audience=http://example/api/v2/
      resource=urn:api:billing
      """
    Then I receive a 200 response
    And the access_token claim "https://example\.com/resource" equals "urn:api:billing"

  Scenario: JSON token requests project too (private_key_jwt variant)
    When I PUT "/admin0/claims/mappings" with body:
      """
      {"resource":"https://example.com/resource"}
      """
    Then I receive a 204 response
    When I post to "/oauth/token" with body:
      """
      {
        "grant_type": "client_credentials",
        "client_id": "demo",
        "client_assertion_type": "urn:ietf:params:oauth:client-assertion-type:jwt-bearer",
        "client_assertion": "eyJ.fake.jwt",
        "audience": "http://example/api/v2/",
        "resource": "urn:api:orders"
      }
      """
    Then I receive a 200 response
    And the access_token claim "https://example\.com/resource" equals "urn:api:orders"

  Scenario: Per-request value overrides the global claims store
    When I PUT "/admin0/claims" with body:
      """
      {"https://example.com/resource":"global-default"}
      """
    And I PUT "/admin0/claims/mappings" with body:
      """
      {"resource":"https://example.com/resource"}
      """
    And I post to "/oauth/token" with form body:
      """
      grant_type=client_credentials
      client_id=demo
      client_secret=x
      audience=http://example/api/v2/
      resource=urn:api:orders
      """
    Then I receive a 200 response
    And the access_token claim "https://example\.com/resource" equals "urn:api:orders"
    When I post to "/oauth/token" with form body:
      """
      grant_type=client_credentials
      client_id=demo
      client_secret=x
      audience=http://example/api/v2/
      """
    Then I receive a 200 response
    And the access_token claim "https://example\.com/resource" equals "global-default"

  Scenario: Without a mapping, extra request parameters are ignored as before
    When I post to "/oauth/token" with form body:
      """
      grant_type=client_credentials
      client_id=demo
      client_secret=x
      audience=http://example/api/v2/
      resource=urn:api:orders
      """
    Then I receive a 200 response
    And the access_token claim "resource" is absent
    And the access_token claim "https://example\.com/resource" is absent

  Scenario: GET /admin0/claims/mappings returns the current map
    When I PUT "/admin0/claims/mappings" with body:
      """
      {"resource":"https://example.com/resource"}
      """
    And I send "GET /admin0/claims/mappings" without a bearer
    Then I receive a 200 response
    And the response JSON path "resource" equals "https://example.com/resource"

  Scenario: DELETE /admin0/claims/mappings clears the map
    When I PUT "/admin0/claims/mappings" with body:
      """
      {"resource":"https://example.com/resource"}
      """
    And I DELETE "/admin0/claims/mappings"
    Then I receive a 204 response
    When I send "GET /admin0/claims/mappings" without a bearer
    Then I receive a 200 response
    And the response body contains "{}"

  Scenario: Invalid JSON body is rejected with 400
    When I PUT "/admin0/claims/mappings" with body:
      """
      not-json
      """
    Then I receive a 400 response
    And the response body contains "invalid_body"

  Scenario: Global /admin0/reset clears claim mappings
    When I PUT "/admin0/claims/mappings" with body:
      """
      {"resource":"https://example.com/resource"}
      """
    And I reset all mock state
    When I send "GET /admin0/claims/mappings" without a bearer
    Then I receive a 200 response
    And the response body contains "{}"
