Feature: Management API — /api/v2/organizations
  As a developer using auth0-mock
  I want to stub the full organizations surface
  So that my tests can drive organization CRUD plus member and connection
  management against deterministic responses

  Background:
    Given the mock is running

  Scenario: List organizations returns the stubbed page
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/organizations" with response:
      """
      {"status":200,"body":[
        {"id":"org_acme","name":"acme","display_name":"Acme Inc."},
        {"id":"org_globex","name":"globex","display_name":"Globex Corp."}
      ]}
      """
    When I send "GET /api/v2/organizations" with a valid bearer
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      [
        {"id":"org_acme","name":"acme","display_name":"Acme Inc."},
        {"id":"org_globex","name":"globex","display_name":"Globex Corp."}
      ]
      """

  Scenario: Get a single organization by ID
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/organizations/org_acme" with response:
      """
      {"status":200,"body":{
        "id":"org_acme",
        "name":"acme",
        "display_name":"Acme Inc.",
        "metadata":{"plan":"enterprise"}
      }}
      """
    When I send "GET /api/v2/organizations/org_acme" with a valid bearer
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      {
        "id":"org_acme",
        "name":"acme",
        "display_name":"Acme Inc.",
        "metadata":{"plan":"enterprise"}
      }
      """

  Scenario: Look up an organization by name
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/organizations/name/acme" with response:
      """
      {"status":200,"body":{
        "id":"org_acme",
        "name":"acme",
        "display_name":"Acme Inc."
      }}
      """
    When I send "GET /api/v2/organizations/name/acme" with a valid bearer
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      {
        "id":"org_acme",
        "name":"acme",
        "display_name":"Acme Inc."
      }
      """

  Scenario: Create an organization
    Given I have a valid bearer token
    And I register an expectation for "POST /api/v2/organizations" with response:
      """
      {"status":201,"body":{
        "id":"org_new",
        "name":"new-org",
        "display_name":"New Org"
      }}
      """
    When I send "POST /api/v2/organizations" with body and a valid bearer:
      """
      {"name":"new-org","display_name":"New Org"}
      """
    Then I receive a 201 response
    And the response body should match the JSON pattern:
      """
      {
        "id":"org_new",
        "name":"new-org",
        "display_name":"New Org"
      }
      """

  Scenario: Update an organization with PATCH
    Given I have a valid bearer token
    And I register an expectation for "PATCH /api/v2/organizations/org_acme" with response:
      """
      {"status":200,"body":{
        "id":"org_acme",
        "name":"acme",
        "display_name":"Acme Industries"
      }}
      """
    When I send "PATCH /api/v2/organizations/org_acme" with body and a valid bearer:
      """
      {"display_name":"Acme Industries"}
      """
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      {
        "id":"org_acme",
        "name":"acme",
        "display_name":"Acme Industries"
      }
      """

  Scenario: Delete an organization
    Given I have a valid bearer token
    And I register an expectation for "DELETE /api/v2/organizations/org_acme" with response:
      """
      {"status":204}
      """
    When I send "DELETE /api/v2/organizations/org_acme" with a valid bearer
    Then I receive a 204 response

  Scenario: List organization members
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/organizations/org_acme/members" with response:
      """
      {"status":200,"body":[
        {"user_id":"auth0|alice","email":"alice@acme.com","name":"Alice"},
        {"user_id":"auth0|bob","email":"bob@acme.com","name":"Bob"}
      ]}
      """
    When I send "GET /api/v2/organizations/org_acme/members" with a valid bearer
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      [
        {"user_id":"auth0|alice","email":"alice@acme.com","name":"Alice"},
        {"user_id":"auth0|bob","email":"bob@acme.com","name":"Bob"}
      ]
      """

  Scenario: Add members to an organization
    Given I have a valid bearer token
    And I register an expectation for "POST /api/v2/organizations/org_acme/members" with response:
      """
      {"status":204}
      """
    When I send "POST /api/v2/organizations/org_acme/members" with body and a valid bearer:
      """
      {"members":["auth0|alice","auth0|bob"]}
      """
    Then I receive a 204 response

  Scenario: Remove members from an organization
    Given I have a valid bearer token
    And I register an expectation for "DELETE /api/v2/organizations/org_acme/members" with response:
      """
      {"status":204}
      """
    When I send "DELETE /api/v2/organizations/org_acme/members" with body and a valid bearer:
      """
      {"members":["auth0|bob"]}
      """
    Then I receive a 204 response

  Scenario: List an organization's enabled connections
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/organizations/org_acme/enabled_connections" with response:
      """
      {"status":200,"body":[
        {"connection_id":"con_db","assign_membership_on_login":false,"connection":{"name":"Username-Password-Authentication","strategy":"auth0"}}
      ]}
      """
    When I send "GET /api/v2/organizations/org_acme/enabled_connections" with a valid bearer
    Then I receive a 200 response
    And the response body should match the JSON pattern:
      """
      [
        {"connection_id":"con_db","assign_membership_on_login":false,"connection":{"name":"Username-Password-Authentication","strategy":"auth0"}}
      ]
      """

  Scenario: Creating an organization with a duplicate name returns the stubbed 409
    Given I have a valid bearer token
    And I register an expectation for "POST /api/v2/organizations" with response:
      """
      {"status":409}
      """
    When I send "POST /api/v2/organizations" with body and a valid bearer:
      """
      {"name":"acme","display_name":"Acme Inc."}
      """
    Then I receive a 409 response

  Scenario: PATCH with an invalid payload returns the stubbed 400
    Given I have a valid bearer token
    And I register an expectation for "PATCH /api/v2/organizations/org_acme" with response:
      """
      {"status":400}
      """
    When I send "PATCH /api/v2/organizations/org_acme" with body and a valid bearer:
      """
      {"display_name":""}
      """
    Then I receive a 400 response

  Scenario: Enable a connection for an organization
    Given I have a valid bearer token
    And I register an expectation for "POST /api/v2/organizations/org_acme/enabled_connections" with response:
      """
      {"status":201,"body":{
        "connection_id":"con_db",
        "assign_membership_on_login":true,
        "is_signup_enabled":false,
        "show_as_button":true,
        "connection":{"name":"Username-Password-Authentication","strategy":"auth0"}
      }}
      """
    When I send "POST /api/v2/organizations/org_acme/enabled_connections" with body and a valid bearer:
      """
      {"connection_id":"con_db","assign_membership_on_login":true}
      """
    Then I receive a 201 response
    And the response body should match the JSON pattern:
      """
      {
        "connection_id":"con_db",
        "assign_membership_on_login":true,
        "is_signup_enabled":false,
        "show_as_button":true,
        "connection":{"name":"Username-Password-Authentication","strategy":"auth0"}
      }
      """
