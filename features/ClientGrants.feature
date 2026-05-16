Feature: Management API — /api/v2/client-grants
  As a developer using auth0-mock
  I want to stub the client-grants surface
  So that my tests can drive grant CRUD against deterministic responses

  Background:
    Given the mock is running

  Scenario: List client grants returns the stubbed page
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/client-grants" with response:
      """
      {"status":200,"body":[
        {"id":"cgr_1","client_id":"cli_demo1","audience":"https://reports.example.com","scope":["read:reports"]},
        {"id":"cgr_2","client_id":"cli_demo2","audience":"https://billing.example.com","scope":["read:billing","write:billing"]}
      ]}
      """
    When I send "GET /api/v2/client-grants" with a valid bearer
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      [
        {"id":"cgr_1","client_id":"cli_demo1","audience":"https://reports.example.com","scope":["read:reports"]},
        {"id":"cgr_2","client_id":"cli_demo2","audience":"https://billing.example.com","scope":["read:billing","write:billing"]}
      ]
      """

  Scenario: Get a single client grant by ID
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/client-grants/cgr_1" with response:
      """
      {"status":200,"body":{
        "id":"cgr_1",
        "client_id":"cli_demo1",
        "audience":"https://reports.example.com",
        "scope":["read:reports","write:reports"]
      }}
      """
    When I send "GET /api/v2/client-grants/cgr_1" with a valid bearer
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      {
        "id":"cgr_1",
        "client_id":"cli_demo1",
        "audience":"https://reports.example.com",
        "scope":["read:reports","write:reports"]
      }
      """

  Scenario: Create a client grant
    Given I have a valid bearer token
    And I register an expectation for "POST /api/v2/client-grants" with response:
      """
      {"status":201,"body":{
        "id":"cgr_new",
        "client_id":"cli_demo1",
        "audience":"https://new.example.com",
        "scope":["read:things"]
      }}
      """
    When I send "POST /api/v2/client-grants" with body and a valid bearer:
      """
      {"client_id":"cli_demo1","audience":"https://new.example.com","scope":["read:things"]}
      """
    Then I receive a 201 response
    And the response body should match the JSON pattern:
      """
      {
        "id":"cgr_new",
        "client_id":"cli_demo1",
        "audience":"https://new.example.com",
        "scope":["read:things"]
      }
      """

  Scenario: Update a client grant with PATCH
    Given I have a valid bearer token
    And I register an expectation for "PATCH /api/v2/client-grants/cgr_1" with response:
      """
      {"status":200,"body":{
        "id":"cgr_1",
        "client_id":"cli_demo1",
        "audience":"https://reports.example.com",
        "scope":["read:reports","write:reports","delete:reports"]
      }}
      """
    When I send "PATCH /api/v2/client-grants/cgr_1" with body and a valid bearer:
      """
      {"scope":["read:reports","write:reports","delete:reports"]}
      """
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      {
        "id":"cgr_1",
        "client_id":"cli_demo1",
        "audience":"https://reports.example.com",
        "scope":["read:reports","write:reports","delete:reports"]
      }
      """

  Scenario: Delete a client grant
    Given I have a valid bearer token
    And I register an expectation for "DELETE /api/v2/client-grants/cgr_1" with response:
      """
      {"status":204}
      """
    When I send "DELETE /api/v2/client-grants/cgr_1" with a valid bearer
    Then I receive a 204 response

  Scenario: Get on a missing client grant returns the stubbed 404
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/client-grants/cgr_missing" with response:
      """
      {"status":404}
      """
    When I send "GET /api/v2/client-grants/cgr_missing" with a valid bearer
    Then I receive a 404 response

  Scenario: Creating a duplicate client grant returns the stubbed 409
    Given I have a valid bearer token
    And I register an expectation for "POST /api/v2/client-grants" with response:
      """
      {"status":409}
      """
    When I send "POST /api/v2/client-grants" with body and a valid bearer:
      """
      {"client_id":"cli_demo1","audience":"https://reports.example.com","scope":["read:reports"]}
      """
    Then I receive a 409 response
