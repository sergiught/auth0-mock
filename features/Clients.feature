Feature: Management API — /api/v2/clients
  As a developer using auth0-mock
  I want to stub the full client-application surface
  So that my tests can drive client CRUD, secret rotation, and credential
  management against deterministic responses

  Background:
    Given the mock is running

  Scenario: List clients returns the stubbed page
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/clients" with response:
      """
      {"status":200,"body":[
        {"client_id":"cli_demo1","name":"Demo App 1","app_type":"non_interactive"},
        {"client_id":"cli_demo2","name":"Demo App 2","app_type":"spa"}
      ]}
      """
    When I send "GET /api/v2/clients" with a valid bearer
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      [
        {"client_id":"cli_demo1","name":"Demo App 1","app_type":"non_interactive"},
        {"client_id":"cli_demo2","name":"Demo App 2","app_type":"spa"}
      ]
      """

  Scenario: Get a single client by ID
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/clients/cli_demo1" with response:
      """
      {"status":200,"body":{
        "client_id":"cli_demo1",
        "name":"Demo App 1",
        "app_type":"non_interactive",
        "callbacks":["https://app.example.com/callback"]
      }}
      """
    When I send "GET /api/v2/clients/cli_demo1" with a valid bearer
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      {
        "client_id":"cli_demo1",
        "name":"Demo App 1",
        "app_type":"non_interactive",
        "callbacks":["https://app.example.com/callback"]
      }
      """

  Scenario: Create a client
    Given I have a valid bearer token
    And I register an expectation for "POST /api/v2/clients" with response:
      """
      {"status":201,"body":{
        "client_id":"cli_new",
        "client_secret":"s3cret-rotated-by-auth0",
        "name":"New App",
        "app_type":"regular_web"
      }}
      """
    When I send "POST /api/v2/clients" with body and a valid bearer:
      """
      {"name":"New App","app_type":"regular_web"}
      """
    Then I receive a 201 response
    And the response body should match the JSON pattern:
      """
      {
        "client_id":"cli_new",
        "client_secret":"s3cret-rotated-by-auth0",
        "name":"New App",
        "app_type":"regular_web"
      }
      """

  Scenario: Update a client with PATCH
    Given I have a valid bearer token
    And I register an expectation for "PATCH /api/v2/clients/cli_demo1" with response:
      """
      {"status":200,"body":{
        "client_id":"cli_demo1",
        "name":"Renamed App",
        "app_type":"non_interactive"
      }}
      """
    When I send "PATCH /api/v2/clients/cli_demo1" with body and a valid bearer:
      """
      {"name":"Renamed App"}
      """
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      {
        "client_id":"cli_demo1",
        "name":"Renamed App",
        "app_type":"non_interactive"
      }
      """

  Scenario: Delete a client
    Given I have a valid bearer token
    And I register an expectation for "DELETE /api/v2/clients/cli_demo1" with response:
      """
      {"status":204}
      """
    When I send "DELETE /api/v2/clients/cli_demo1" with a valid bearer
    Then I receive a 204 response

  Scenario: Rotate a client's secret
    Given I have a valid bearer token
    And I register an expectation for "POST /api/v2/clients/cli_demo1/rotate-secret" with response:
      """
      {"status":200,"body":{
        "client_id":"cli_demo1",
        "client_secret":"rotated-s3cret-v2",
        "name":"Demo App 1"
      }}
      """
    When I send "POST /api/v2/clients/cli_demo1/rotate-secret" with a valid bearer
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      {
        "client_id":"cli_demo1",
        "client_secret":"rotated-s3cret-v2",
        "name":"Demo App 1"
      }
      """

  Scenario: List a client's credentials
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/clients/cli_demo1/credentials" with response:
      """
      {"status":200,"body":[
        {"id":"cred_1","credential_type":"public_key","kid":"abc123"},
        {"id":"cred_2","credential_type":"public_key","kid":"def456"}
      ]}
      """
    When I send "GET /api/v2/clients/cli_demo1/credentials" with a valid bearer
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      [
        {"id":"cred_1","credential_type":"public_key","kid":"abc123"},
        {"id":"cred_2","credential_type":"public_key","kid":"def456"}
      ]
      """

  Scenario: Create a client credential
    Given I have a valid bearer token
    And I register an expectation for "POST /api/v2/clients/cli_demo1/credentials" with response:
      """
      {"status":201,"body":{
        "id":"cred_new",
        "credential_type":"public_key",
        "kid":"new-kid-xyz",
        "alg":"RS256"
      }}
      """
    When I send "POST /api/v2/clients/cli_demo1/credentials" with body and a valid bearer:
      """
      {"credential_type":"public_key","name":"new","pem":"-----BEGIN PUBLIC KEY-----\nMIIB...\n-----END PUBLIC KEY-----"}
      """
    Then I receive a 201 response
    And the response body should match the JSON pattern:
      """
      {
        "id":"cred_new",
        "credential_type":"public_key",
        "kid":"new-kid-xyz",
        "alg":"RS256"
      }
      """

  Scenario: Delete a client credential
    Given I have a valid bearer token
    And I register an expectation for "DELETE /api/v2/clients/cli_demo1/credentials/cred_1" with response:
      """
      {"status":204}
      """
    When I send "DELETE /api/v2/clients/cli_demo1/credentials/cred_1" with a valid bearer
    Then I receive a 204 response

  Scenario: Get on a missing client returns the stubbed 404
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/clients/cli_missing" with response:
      """
      {"status":404}
      """
    When I send "GET /api/v2/clients/cli_missing" with a valid bearer
    Then I receive a 404 response

  Scenario: Creating a client with a conflicting name returns the stubbed 409
    Given I have a valid bearer token
    And I register an expectation for "POST /api/v2/clients" with response:
      """
      {"status":409}
      """
    When I send "POST /api/v2/clients" with body and a valid bearer:
      """
      {"name":"Demo App 1","app_type":"non_interactive"}
      """
    Then I receive a 409 response
