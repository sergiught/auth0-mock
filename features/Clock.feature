Feature: Clock control
  /admin0/clock lets tests freeze and skew the mock's perception of
  time so token issuance, code TTLs, and bearer validation can be
  exercised deterministically — same clock drives the minter and the
  validator.

  Background:
    Given the mock is running

  Scenario: GET starts in real mode
    When I GET "/admin0/clock"
    Then I receive a 200 response
    And the response JSON path "mode" equals "real"
    And the response JSON path "now" exists

  Scenario: Freeze pins the clock to the requested instant
    When I PUT "/admin0/clock" with body:
      """
      {"now":"2030-01-01T00:00:00Z"}
      """
    Then I receive a 204 response
    When I GET "/admin0/clock"
    Then I receive a 200 response
    And the response JSON path "mode" equals "frozen"
    And the response JSON path "now" equals "2030-01-01T00:00:00Z"

  Scenario: Advance in frozen mode moves the pinned instant
    Given I PUT "/admin0/clock" with body:
      """
      {"now":"2030-01-01T00:00:00Z"}
      """
    When I POST "/admin0/clock/advance" with body:
      """
      {"by":"25h"}
      """
    Then I receive a 204 response
    When I GET "/admin0/clock"
    And the response JSON path "now" equals "2030-01-02T01:00:00Z"

  Scenario: Offset includes the configured skew on GET
    When I PUT "/admin0/clock" with body:
      """
      {"offset":"25h"}
      """
    Then I receive a 204 response
    When I GET "/admin0/clock"
    Then the response JSON path "mode" equals "offset"
    And the response JSON path "offset" equals "25h0m0s"

  Scenario: Advance in real mode is rejected
    When I POST "/admin0/clock/advance" with body:
      """
      {"by":"1h"}
      """
    Then I receive a 400 response
    And the response body contains "invalid_clock_state"

  Scenario: PUT with both now and offset is rejected
    When I PUT "/admin0/clock" with body:
      """
      {"now":"2030-01-01T00:00:00Z","offset":"1h"}
      """
    Then I receive a 400 response
    And the response body contains "invalid_clock_request"

  Scenario: PUT with bad RFC 3339 is rejected
    When I PUT "/admin0/clock" with body:
      """
      {"now":"not-a-timestamp"}
      """
    Then I receive a 400 response
    And the response body contains "invalid_clock_time"

  Scenario: DELETE restores real mode
    Given I PUT "/admin0/clock" with body:
      """
      {"now":"2030-01-01T00:00:00Z"}
      """
    When I DELETE "/admin0/clock"
    Then I receive a 204 response
    When I GET "/admin0/clock"
    Then the response JSON path "mode" equals "real"

  Scenario: POST /admin0/reset also resets the clock
    Given I PUT "/admin0/clock" with body:
      """
      {"now":"2030-01-01T00:00:00Z"}
      """
    When I reset all mock state
    Then I receive a 204 response
    When I GET "/admin0/clock"
    Then the response JSON path "mode" equals "real"

  Scenario: Frozen clock drives the minter — iat reflects the pinned instant
    Given I PUT "/admin0/clock" with body:
      """
      {"now":"2030-01-01T00:00:00Z"}
      """
    When I post to "/oauth/token" with form body:
      """
      grant_type=client_credentials
      client_id=demo
      client_secret=x
      audience=https://api.example.com/
      """
    Then I receive a 200 response
    And the access_token claim "iat" equals "1893456000"

  Scenario: Frozen clock drives the validator — token expires when clock advances
    Given I register the expectation "GET /api/v2/users/auth0|alice":
      """
      {"response":{"status":200,"body":{"user_id":"auth0|alice","email":"alice@example.com"}}}
      """
    And I PUT "/admin0/clock" with body:
      """
      {"now":"2030-01-01T00:00:00Z"}
      """
    And I post to "/oauth/token" with form body:
      """
      grant_type=client_credentials
      client_id=demo
      client_secret=x
      audience=https://api.example.com/
      """
    And I save the access_token as my bearer
    When I POST "/admin0/clock/advance" with body:
      """
      {"by":"25h"}
      """
    And I send "GET /api/v2/users/auth0|alice" with a valid bearer
    Then I receive a 401 response
    And the response body contains "invalid_bearer"
