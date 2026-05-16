Feature: Management API — /api/v2/connections
  As a developer using auth0-mock
  I want to stub the full connections surface
  So that my tests can drive connection CRUD and status checks against
  deterministic responses

  Background:
    Given the mock is running

  Scenario: List connections returns the stubbed page
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/connections" with response:
      """
      {"status":200,"body":[
        {"id":"con_db","name":"Username-Password-Authentication","strategy":"auth0","display_name":"Database"},
        {"id":"con_google","name":"google-oauth2","strategy":"google-oauth2","display_name":"Google"}
      ]}
      """
    When I send "GET /api/v2/connections" with a valid bearer
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      [
        {"id":"con_db","name":"Username-Password-Authentication","strategy":"auth0","display_name":"Database"},
        {"id":"con_google","name":"google-oauth2","strategy":"google-oauth2","display_name":"Google"}
      ]
      """

  Scenario: Get a single connection by ID
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/connections/con_db" with response:
      """
      {"status":200,"body":{
        "id":"con_db",
        "name":"Username-Password-Authentication",
        "strategy":"auth0",
        "display_name":"Database",
        "is_domain_connection":false,
        "enabled_clients":["cli_demo1","cli_demo2"]
      }}
      """
    When I send "GET /api/v2/connections/con_db" with a valid bearer
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      {
        "id":"con_db",
        "name":"Username-Password-Authentication",
        "strategy":"auth0",
        "display_name":"Database",
        "is_domain_connection":false,
        "enabled_clients":["cli_demo1","cli_demo2"]
      }
      """

  Scenario: Create a connection
    Given I have a valid bearer token
    And I register an expectation for "POST /api/v2/connections" with response:
      """
      {"status":201,"body":{
        "id":"con_new",
        "name":"my-new-connection",
        "strategy":"auth0",
        "display_name":"My New Connection"
      }}
      """
    When I send "POST /api/v2/connections" with body and a valid bearer:
      """
      {"name":"my-new-connection","strategy":"auth0","display_name":"My New Connection"}
      """
    Then I receive a 201 response
    And the response body should match the JSON pattern:
      """
      {
        "id":"con_new",
        "name":"my-new-connection",
        "strategy":"auth0",
        "display_name":"My New Connection"
      }
      """

  Scenario: Update a connection with PATCH
    Given I have a valid bearer token
    And I register an expectation for "PATCH /api/v2/connections/con_db" with response:
      """
      {"status":200,"body":{
        "id":"con_db",
        "name":"Username-Password-Authentication",
        "strategy":"auth0",
        "display_name":"Database (renamed)"
      }}
      """
    When I send "PATCH /api/v2/connections/con_db" with body and a valid bearer:
      """
      {"display_name":"Database (renamed)"}
      """
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      {
        "id":"con_db",
        "name":"Username-Password-Authentication",
        "strategy":"auth0",
        "display_name":"Database (renamed)"
      }
      """

  Scenario: Delete a connection
    Given I have a valid bearer token
    And I register an expectation for "DELETE /api/v2/connections/con_db" with response:
      """
      {"status":204}
      """
    When I send "DELETE /api/v2/connections/con_db" with a valid bearer
    Then I receive a 204 response

  Scenario: Fetch a connection's health status
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/connections/con_db/status" with response:
      """
      {"status":200}
      """
    When I send "GET /api/v2/connections/con_db/status" with a valid bearer
    Then I receive a 200 response

  Scenario: Get on a missing connection returns the stubbed 404
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/connections/con_missing" with response:
      """
      {"status":404}
      """
    When I send "GET /api/v2/connections/con_missing" with a valid bearer
    Then I receive a 404 response

  Scenario: Creating a connection with a conflicting name returns the stubbed 409
    Given I have a valid bearer token
    And I register an expectation for "POST /api/v2/connections" with response:
      """
      {"status":409}
      """
    When I send "POST /api/v2/connections" with body and a valid bearer:
      """
      {"name":"Username-Password-Authentication","strategy":"auth0"}
      """
    Then I receive a 409 response
