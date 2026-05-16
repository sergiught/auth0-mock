Feature: Management API — /api/v2/users
  As a developer using auth0-mock
  I want to stub the full user-management surface
  So that my tests can drive user CRUD, role assignment, and permission grants
  against deterministic responses

  Background:
    Given the mock is running

  Scenario: List users returns the stubbed page
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/users" with response:
      """
      {"status":200,"body":[
        {"user_id":"auth0|alice","email":"alice@example.com","name":"Alice"},
        {"user_id":"auth0|bob","email":"bob@example.com","name":"Bob"}
      ]}
      """
    When I send "GET /api/v2/users" with a valid bearer
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      [
        {"user_id":"auth0|alice","email":"alice@example.com","name":"Alice"},
        {"user_id":"auth0|bob","email":"bob@example.com","name":"Bob"}
      ]
      """

  Scenario: Get a single user by ID
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/users/auth0|alice" with response:
      """
      {"status":200,"body":{
        "user_id":"auth0|alice",
        "email":"alice@example.com",
        "email_verified":true,
        "name":"Alice"
      }}
      """
    When I send "GET /api/v2/users/auth0|alice" with a valid bearer
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      {
        "user_id":"auth0|alice",
        "email":"alice@example.com",
        "email_verified":true,
        "name":"Alice"
      }
      """

  Scenario: Create a user
    Given I have a valid bearer token
    And I register an expectation for "POST /api/v2/users" with response:
      """
      {"status":201,"body":{
        "user_id":"auth0|new",
        "email":"new@example.com",
        "connection":"Username-Password-Authentication"
      }}
      """
    When I send "POST /api/v2/users" with body and a valid bearer:
      """
      {"email":"new@example.com","password":"S3cretPass!","connection":"Username-Password-Authentication"}
      """
    Then I receive a 201 response
    And the response body should match the JSON pattern:
      """
      {
        "user_id":"auth0|new",
        "email":"new@example.com",
        "connection":"Username-Password-Authentication"
      }
      """

  Scenario: Update a user with PATCH
    Given I have a valid bearer token
    And I register an expectation for "PATCH /api/v2/users/auth0|alice" with response:
      """
      {"status":200,"body":{
        "user_id":"auth0|alice",
        "email":"alice@example.com",
        "name":"Alice Updated"
      }}
      """
    When I send "PATCH /api/v2/users/auth0|alice" with body and a valid bearer:
      """
      {"name":"Alice Updated"}
      """
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      {
        "user_id":"auth0|alice",
        "email":"alice@example.com",
        "name":"Alice Updated"
      }
      """

  Scenario: Delete a user
    Given I have a valid bearer token
    And I register an expectation for "DELETE /api/v2/users/auth0|alice" with response:
      """
      {"status":204}
      """
    When I send "DELETE /api/v2/users/auth0|alice" with a valid bearer
    Then I receive a 204 response

  Scenario: Look up a user by email
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/users-by-email" with response:
      """
      {"status":200,"body":[
        {"user_id":"auth0|alice","email":"alice@example.com"}
      ]}
      """
    When I send "GET /api/v2/users-by-email?email=alice@example.com" with a valid bearer
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      [
        {"user_id":"auth0|alice","email":"alice@example.com"}
      ]
      """

  Scenario: List a user's roles
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/users/auth0|alice/roles" with response:
      """
      {"status":200,"body":[
        {"id":"rol_admin","name":"admin","description":"Full access"},
        {"id":"rol_viewer","name":"viewer","description":"Read-only"}
      ]}
      """
    When I send "GET /api/v2/users/auth0|alice/roles" with a valid bearer
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      [
        {"id":"rol_admin","name":"admin","description":"Full access"},
        {"id":"rol_viewer","name":"viewer","description":"Read-only"}
      ]
      """

  Scenario: Assign roles to a user
    Given I have a valid bearer token
    And I register an expectation for "POST /api/v2/users/auth0|alice/roles" with response:
      """
      {"status":204}
      """
    When I send "POST /api/v2/users/auth0|alice/roles" with body and a valid bearer:
      """
      {"roles":["rol_admin","rol_viewer"]}
      """
    Then I receive a 204 response

  Scenario: Remove roles from a user
    Given I have a valid bearer token
    And I register an expectation for "DELETE /api/v2/users/auth0|alice/roles" with response:
      """
      {"status":204}
      """
    When I send "DELETE /api/v2/users/auth0|alice/roles" with body and a valid bearer:
      """
      {"roles":["rol_viewer"]}
      """
    Then I receive a 204 response

  Scenario: List a user's permissions
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/users/auth0|alice/permissions" with response:
      """
      {"status":200,"body":[
        {"resource_server_identifier":"https://api.example.com","permission_name":"read:reports"},
        {"resource_server_identifier":"https://api.example.com","permission_name":"write:reports"}
      ]}
      """
    When I send "GET /api/v2/users/auth0|alice/permissions" with a valid bearer
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      [
        {"resource_server_identifier":"https://api.example.com","permission_name":"read:reports"},
        {"resource_server_identifier":"https://api.example.com","permission_name":"write:reports"}
      ]
      """

  Scenario: Grant permissions to a user
    Given I have a valid bearer token
    And I register an expectation for "POST /api/v2/users/auth0|alice/permissions" with response:
      """
      {"status":201}
      """
    When I send "POST /api/v2/users/auth0|alice/permissions" with body and a valid bearer:
      """
      {"permissions":[
        {"resource_server_identifier":"https://api.example.com","permission_name":"read:reports"}
      ]}
      """
    Then I receive a 201 response

  Scenario: Revoke permissions from a user
    Given I have a valid bearer token
    And I register an expectation for "DELETE /api/v2/users/auth0|alice/permissions" with response:
      """
      {"status":204}
      """
    When I send "DELETE /api/v2/users/auth0|alice/permissions" with body and a valid bearer:
      """
      {"permissions":[
        {"resource_server_identifier":"https://api.example.com","permission_name":"read:reports"}
      ]}
      """
    Then I receive a 204 response

  Scenario: Unauthenticated requests are rejected before the stub is consulted
    Given I register an expectation for "GET /api/v2/users/auth0|alice" with response:
      """
      {"status":200,"body":{"user_id":"auth0|alice"}}
      """
    When I send "GET /api/v2/users/auth0|alice" without a bearer
    Then I receive a 401 response

  Scenario: Get on a missing user returns the stubbed 404
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/users/auth0|missing" with response:
      """
      {"status":404}
      """
    When I send "GET /api/v2/users/auth0|missing" with a valid bearer
    Then I receive a 404 response

  Scenario: Creating a user with a duplicate email returns the stubbed 409
    Given I have a valid bearer token
    And I register an expectation for "POST /api/v2/users" with response:
      """
      {"status":409}
      """
    When I send "POST /api/v2/users" with body and a valid bearer:
      """
      {"email":"alice@example.com","password":"S3cretPass!","connection":"Username-Password-Authentication"}
      """
    Then I receive a 409 response
