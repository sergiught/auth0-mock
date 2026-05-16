Feature: Management API — /api/v2/resource-servers
  As a developer using auth0-mock
  I want to stub the resource-server (API) surface
  So that my tests can drive API CRUD against deterministic responses
  Note: PATCH /resource-servers/{id} returns 201 (not 200) per the Auth0 spec.

  Background:
    Given the mock is running

  Scenario: List resource servers returns the stubbed page
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/resource-servers" with response:
      """
      {"status":200,"body":[
        {"id":"rs_reports","name":"Reports API","identifier":"https://reports.example.com"},
        {"id":"rs_billing","name":"Billing API","identifier":"https://billing.example.com"}
      ]}
      """
    When I send "GET /api/v2/resource-servers" with a valid bearer
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      [
        {"id":"rs_reports","name":"Reports API","identifier":"https://reports.example.com"},
        {"id":"rs_billing","name":"Billing API","identifier":"https://billing.example.com"}
      ]
      """

  Scenario: Get a single resource server by ID
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/resource-servers/rs_reports" with response:
      """
      {"status":200,"body":{
        "id":"rs_reports",
        "name":"Reports API",
        "identifier":"https://reports.example.com",
        "scopes":[
          {"value":"read:reports","description":"Read reports"},
          {"value":"write:reports","description":"Write reports"}
        ],
        "signing_alg":"RS256"
      }}
      """
    When I send "GET /api/v2/resource-servers/rs_reports" with a valid bearer
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      {
        "id":"rs_reports",
        "name":"Reports API",
        "identifier":"https://reports.example.com",
        "scopes":[
          {"value":"read:reports","description":"Read reports"},
          {"value":"write:reports","description":"Write reports"}
        ],
        "signing_alg":"RS256"
      }
      """

  Scenario: Create a resource server
    Given I have a valid bearer token
    And I register an expectation for "POST /api/v2/resource-servers" with response:
      """
      {"status":201,"body":{
        "id":"rs_new",
        "name":"New API",
        "identifier":"https://new.example.com",
        "signing_alg":"RS256"
      }}
      """
    When I send "POST /api/v2/resource-servers" with body and a valid bearer:
      """
      {"name":"New API","identifier":"https://new.example.com","signing_alg":"RS256"}
      """
    Then I receive a 201 response
    And the response body should match the JSON pattern:
      """
      {
        "id":"rs_new",
        "name":"New API",
        "identifier":"https://new.example.com",
        "signing_alg":"RS256"
      }
      """

  Scenario: Update a resource server with PATCH (returns 201)
    Given I have a valid bearer token
    And I register an expectation for "PATCH /api/v2/resource-servers/rs_reports" with response:
      """
      {"status":201,"body":{
        "id":"rs_reports",
        "name":"Reports API v2",
        "identifier":"https://reports.example.com"
      }}
      """
    When I send "PATCH /api/v2/resource-servers/rs_reports" with body and a valid bearer:
      """
      {"name":"Reports API v2"}
      """
    Then I receive a 201 response
    And the response body should match the JSON pattern:
      """
      {
        "id":"rs_reports",
        "name":"Reports API v2",
        "identifier":"https://reports.example.com"
      }
      """

  Scenario: Delete a resource server
    Given I have a valid bearer token
    And I register an expectation for "DELETE /api/v2/resource-servers/rs_reports" with response:
      """
      {"status":204}
      """
    When I send "DELETE /api/v2/resource-servers/rs_reports" with a valid bearer
    Then I receive a 204 response

  Scenario: Get on a missing resource server returns the stubbed 404
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/resource-servers/rs_missing" with response:
      """
      {"status":404}
      """
    When I send "GET /api/v2/resource-servers/rs_missing" with a valid bearer
    Then I receive a 404 response

  Scenario: Creating a resource server with a duplicate identifier returns the stubbed 409
    Given I have a valid bearer token
    And I register an expectation for "POST /api/v2/resource-servers" with response:
      """
      {"status":409}
      """
    When I send "POST /api/v2/resource-servers" with body and a valid bearer:
      """
      {"name":"Reports API","identifier":"https://reports.example.com","signing_alg":"RS256"}
      """
    Then I receive a 409 response
