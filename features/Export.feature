Feature: Export mock state
  GET /admin0/export dumps the expectations, claims, claim mappings,
  permissions and MFA flag in the seed file format.

  Scenario: The export includes seeded and runtime state
    Given the mock is running with seed file:
      """
      expectations:
        - method: GET
          path: /api/v2/users/auth0|seed
          response: {status: 200, body: {user_id: "auth0|seed"}}
      claims:
        tier: gold
      """
    And I register an expectation for "GET /api/v2/users/auth0|extra" with response:
      """
      {"status":200,"body":{"user_id":"auth0|extra"}}
      """
    When I send "GET /admin0/export" without a bearer
    Then I receive a 200 response
    And the response header "Content-Type" equals "application/json"
    And the response JSON path "expectations.#" equals "2"
    And the response JSON path "claims.tier" equals "gold"

  Scenario: The export can be YAML
    Given the mock is running with seed file:
      """
      claims: {tier: gold}
      """
    When I send "GET /admin0/export?format=yaml" without a bearer
    Then I receive a 200 response
    And the response header "Content-Type" equals "application/yaml"
    And the response body contains "tier: gold"

  Scenario: Detail adds ids and hit counts
    Given the mock is running with seed file:
      """
      expectations:
        - method: GET
          path: /api/v2/users/auth0|seed
          response: {status: 200, body: {user_id: "auth0|seed"}}
      """
    And I have a valid bearer token
    When I send "GET /api/v2/users/auth0|seed" with a valid bearer
    And I send "GET /admin0/export?detail=true" without a bearer
    Then the response JSON path "expectations.0.hits" equals "1"
    And the response JSON path "expectations.0.id" exists

  Scenario: An export loads back as the same state
    Given the mock is running with seed file:
      """
      expectations:
        - method: GET
          path: /api/v2/users/{id}
          response: {status: 200, body: {user_id: "auth0|1", n: 9007199254740993}}
        - method: GET
          path: /api/v2/users/auth0|seed
          response: {status: 404}
          then: [{status: 200, body: {user_id: "auth0|seed"}}]
      claims: {tier: gold}
      permissions:
        "https://api.example.test/": [read:things]
      mfa_required: true
      """
    When I send "GET /admin0/export" without a bearer
    And I save the response body
    And I replace the seed file with the saved body
    And I send "GET /admin0/export" without a bearer
    Then the response body equals the saved body

  Scenario: An export loads back as the same state in YAML
    Given the mock is running with seed file:
      """
      expectations:
        - method: GET
          path: /api/v2/users/{id}
          response: {status: 200, body: {user_id: "auth0|1", created: "2024-01-01"}}
      claims: {tier: gold}
      """
    When I send "GET /admin0/export?format=yaml" without a bearer
    And I save the response body
    And I replace the seed file with the saved body
    And I send "GET /admin0/export?format=yaml" without a bearer
    Then the response body equals the saved body

  Scenario: Bad query values are rejected
    When I send "GET /admin0/export?format=xml" without a bearer
    Then I receive a 400 response
    And the response JSON path "errorCode" equals "invalid_query"
    When I send "GET /admin0/export?detail=maybe" without a bearer
    Then I receive a 400 response
