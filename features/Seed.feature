Feature: Seed file
  SEED_FILE loads expectations, claims, permissions and the MFA flag at
  startup; reset returns to that state and edits to the file are reloaded.

  Scenario: The seeded state is live at startup
    Given the mock is running with seed file:
      """
      expectations:
        - method: GET
          path: /api/v2/users/auth0|seed
          response:
            status: 200
            body: {user_id: "auth0|seed"}
      claims:
        tier: gold
      permissions:
        "https://api.example.test/": [read:things]
      mfa_required: true
      """
    And I have a valid bearer token
    When I send "GET /api/v2/users/auth0|seed" with a valid bearer
    Then I receive a 200 response
    And the response JSON path "user_id" equals "auth0|seed"
    When I send "GET /admin0/claims" without a bearer
    Then the response JSON path "tier" equals "gold"
    When I send "GET /admin0/permissions/https://api.example.test/" without a bearer
    Then the response JSON path "0" equals "read:things"
    When I send "GET /admin0/mfa-required" without a bearer
    Then the response JSON path "required" equals "true"

  Scenario: Reset returns to the seed
    Given the mock is running with seed file:
      """
      expectations:
        - method: GET
          path: /api/v2/users/auth0|seed
          response: {status: 200, body: {user_id: "auth0|seed"}}
      """
    And I register an expectation for "GET /api/v2/users/auth0|extra" with response:
      """
      {"status":200,"body":{"user_id":"auth0|extra"}}
      """
    When I reset all mock state
    And I list registered expectations
    Then the expectations list has 1 entries
    And the expectations list contains "GET /api/v2/users/auth0|seed"

  Scenario: keep_seed=false wipes everything, a plain reset restores the seed
    Given the mock is running with seed file:
      """
      expectations:
        - method: GET
          path: /api/v2/users/auth0|seed
          response: {status: 200, body: {user_id: "auth0|seed"}}
      claims:
        tier: gold
      mfa_required: true
      """
    When I send "POST /admin0/reset?keep_seed=false" without a bearer
    Then I receive a 204 response
    When I list registered expectations
    Then the expectations list has 0 entries
    When I send "GET /admin0/claims" without a bearer
    Then the response JSON path "tier" does not exist
    When I send "GET /admin0/mfa-required" without a bearer
    Then the response JSON path "required" equals "false"
    When I reset all mock state
    And I list registered expectations
    Then the expectations list has 1 entries

  Scenario: A non-boolean keep_seed is rejected
    Given the mock is running with seed file:
      """
      expectations:
        - method: GET
          path: /api/v2/users/auth0|seed
          response: {status: 200, body: {user_id: "auth0|seed"}}
      """
    When I send "POST /admin0/reset?keep_seed=maybe" without a bearer
    Then I receive a 400 response
    When I send "POST /admin0/reset?keep_seed=0" without a bearer
    Then I receive a 400 response
    When I send "POST /admin0/reset?keep_seed=" without a bearer
    Then I receive a 400 response
    When I send "POST /admin0/reset?keep_seed=false;x=1" without a bearer
    Then I receive a 400 response
    When I send "POST /admin0/reset?keep_seed=fals%zz" without a bearer
    Then I receive a 400 response
    When I send "POST /admin0/reset?keep_seed=true&keep_seed=false" without a bearer
    Then I receive a 400 response

  Scenario: Editing the file replaces the state, runtime stubs included
    Given the mock is running with seed file:
      """
      expectations:
        - method: GET
          path: /api/v2/users/auth0|seed
          response: {status: 200, body: {user_id: "auth0|seed"}}
      """
    And I register an expectation for "GET /api/v2/users/auth0|extra" with response:
      """
      {"status":200,"body":{"user_id":"auth0|extra"}}
      """
    When I replace the seed file with:
      """
      expectations:
        - method: GET
          path: /api/v2/users/auth0|new
          response: {status: 200, body: {user_id: "auth0|new"}}
      """
    And I list registered expectations
    Then the expectations list has 1 entries
    And the expectations list contains "GET /api/v2/users/auth0|new"

  Scenario: A broken edit keeps the current state
    Given the mock is running with seed file:
      """
      expectations:
        - method: GET
          path: /api/v2/users/auth0|seed
          response: {status: 200, body: {user_id: "auth0|seed"}}
      """
    When I replace the seed file with:
      """
      expectations:
        - method: GET
          path: /api/v2/nope
          response: {status: 200}
      """
    And I list registered expectations
    Then the expectations list contains "GET /api/v2/users/auth0|seed"

  Scenario: A deleted file keeps the current state
    Given the mock is running with seed file:
      """
      expectations:
        - method: GET
          path: /api/v2/users/auth0|seed
          response: {status: 200, body: {user_id: "auth0|seed"}}
      """
    When I delete the seed file
    And I list registered expectations
    Then the expectations list contains "GET /api/v2/users/auth0|seed"

  Scenario: An invalid seed file stops startup
    Then starting the mock with seed file fails with "unknown key \"expectation\"":
      """
      expectation:
        - method: GET
          path: /api/v2/users
          response: {status: 200}
      """
