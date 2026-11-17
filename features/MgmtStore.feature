Feature: Stored Management API
  With MGMT_STORE on, create, read, update, list and delete calls on stored
  Management API paths work against per-namespace in-memory data.

  Scenario: A role round trip
    Given the mock restarts with the Management store on
    And I have a valid bearer token
    When I send "POST /api/v2/roles" with body and a valid bearer:
      """
      {"name":"editor","description":"can edit"}
      """
    Then I receive a 200 response
    And the response JSON path "name" equals "editor"
    And I save the response JSON path "id" as "rid"
    When I send "GET /api/v2/roles/{{rid}}" using saved values with a valid bearer
    Then I receive a 200 response
    And the response JSON path "description" equals "can edit"
    When I send "GET /api/v2/roles?include_totals=true" with a valid bearer
    Then I receive a 200 response
    And the response JSON path "total" equals "1"
    And the response JSON path "roles.0.name" equals "editor"
    When I send "PATCH /api/v2/roles/{{rid}}" using saved values with body and a valid bearer:
      """
      {"description":"can edit and publish"}
      """
    Then I receive a 200 response
    And the response JSON path "description" equals "can edit and publish"
    And the response JSON path "name" equals "editor"
    When I send "DELETE /api/v2/roles/{{rid}}" using saved values with a valid bearer
    Then I receive a 200 response
    When I send "GET /api/v2/roles/{{rid}}" using saved values with a valid bearer
    Then I receive a 404 response

  Scenario: A nested invitation under a missing organization gets 404
    Given the mock restarts with the Management store on
    And I have a valid bearer token
    When I send "POST /api/v2/organizations/org_missing/invitations" with body and a valid bearer:
      """
      {"inviter":{"name":"Ann"},"invitee":{"email":"bob@example.com"},"client_id":"abc"}
      """
    Then I receive a 404 response

  Scenario: An override stub beats the store and deleting it hands control back
    Given the mock restarts with the Management store on
    And I have a valid bearer token
    When I send "POST /api/v2/roles" with body and a valid bearer:
      """
      {"name":"editor"}
      """
    Then I receive a 200 response
    When I POST "/admin0/expectations" with body:
      """
      {"method":"GET","path":"/api/v2/roles","override":true,"response":{"status":429,"body":{"statusCode":429,"error":"Too Many Requests","message":"slow down"}}}
      """
    Then I receive a 201 response
    And I save the response JSON path "id" as "stub"
    When I send "GET /api/v2/roles" with a valid bearer
    Then I receive a 429 response
    When I send "DELETE /admin0/expectations/{{stub}}" using saved values with a valid bearer
    Then I receive a 204 response
    When I send "GET /api/v2/roles" with a valid bearer
    Then I receive a 200 response
    And the response JSON path "0.name" equals "editor"

  Scenario: With the store off, a stub still answers as before
    Given I have a valid bearer token
    And I register an expectation for "GET /api/v2/roles" with response:
      """
      {"status":200,"body":[{"id":"rol_stub","name":"stubbed"}]}
      """
    When I send "GET /api/v2/roles" with a valid bearer
    Then I receive a 200 response
    And the response JSON path "0.name" equals "stubbed"

  Scenario: Reset brings back seeded users
    Given the mock restarts with the Management store on and seed file:
      """
      management:
        users:
          - user_id: "auth0|seeded"
            email: seeded@example.com
      """
    And I have a valid bearer token
    When I send "POST /api/v2/users" with body and a valid bearer:
      """
      {"email":"extra@example.com","connection":"Username-Password-Authentication","password":"Passw0rd!x"}
      """
    Then I receive a 201 response
    When I send "GET /api/v2/users" with a valid bearer
    Then the response JSON path "#" equals "2"
    When I reset all mock state
    And I send "GET /api/v2/users" with a valid bearer
    Then the response JSON path "#" equals "1"
    And the response JSON path "0.email" equals "seeded@example.com"

  Scenario: Namespaces do not see each other's roles
    Given the mock restarts with the Management store on
    When I post to "/ns/a/oauth/token" with form body:
      """
      grant_type=client_credentials
      client_id=demo
      client_secret=x
      audience=http://example/api/v2/
      """
    And I save the access_token as my bearer
    And I send "POST /ns/a/api/v2/roles" with body and a valid bearer:
      """
      {"name":"only-in-a"}
      """
    Then I receive a 200 response
    When I send "GET /ns/a/api/v2/roles" with a valid bearer
    Then the response JSON path "#" equals "1"
    When I post to "/ns/b/oauth/token" with form body:
      """
      grant_type=client_credentials
      client_id=demo
      client_secret=x
      audience=http://example/api/v2/
      """
    And I save the access_token as my bearer
    And I send "GET /ns/b/api/v2/roles" with a valid bearer
    Then the response JSON path "#" equals "0"
