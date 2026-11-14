Feature: Namespaces isolate state on one shared mock
  A namespace is picked per request by the X-Auth0-Mock-Namespace header or
  a /ns/<id>/ path prefix. Each one has private state; keys and the spec are
  shared. Requests with neither use the default namespace.

  Background:
    Given the mock is running

  Scenario: Two namespaces answer the same request differently
    When I POST "/ns/a/admin0/expectations" with body:
      """
      {"method":"GET","path":"/api/v2/users/auth0|1","response":{"status":200,"body":{"user_id":"auth0|1","email":"a@x"}}}
      """
    Then I receive a 201 response
    When I POST "/ns/b/admin0/expectations" with body:
      """
      {"method":"GET","path":"/api/v2/users/auth0|1","response":{"status":200,"body":{"user_id":"auth0|1","email":"b@x"}}}
      """
    Then I receive a 201 response
    When I post to "/ns/a/oauth/token" with form body:
      """
      grant_type=client_credentials
      client_id=demo
      client_secret=x
      audience=http://example/api/v2/
      """
    And I save the access_token as my bearer
    And I send "GET /ns/a/api/v2/users/auth0|1" with a valid bearer
    Then the response JSON path "email" equals "a@x"
    When I post to "/ns/b/oauth/token" with form body:
      """
      grant_type=client_credentials
      client_id=demo
      client_secret=x
      audience=http://example/api/v2/
      """
    And I save the access_token as my bearer
    And I send "GET /ns/b/api/v2/users/auth0|1" with a valid bearer
    Then the response JSON path "email" equals "b@x"

  Scenario: The header and the prefix select the same namespace
    When I POST "/ns/a/admin0/expectations" with body:
      """
      {"method":"GET","path":"/api/v2/users/auth0|1","response":{"status":200,"body":{"user_id":"auth0|1","email":"a@x"}}}
      """
    Then I receive a 201 response
    When I send "GET /admin0/expectations" with headers:
      | X-Auth0-Mock-Namespace | a |
    Then I receive a 200 response
    And the response JSON path "expectations.#" equals "1"
    When I GET "/admin0/expectations"
    Then the response JSON path "expectations.#" equals "0"

  Scenario: Reset in one namespace leaves the other alone
    Given I PUT "/ns/a/admin0/claims" with body:
      """
      {"role":"admin"}
      """
    And I PUT "/ns/b/admin0/claims" with body:
      """
      {"role":"viewer"}
      """
    When I send "POST /ns/a/admin0/reset" without a bearer
    Then I receive a 204 response
    When I GET "/ns/a/admin0/claims"
    Then the response JSON path "role" does not exist
    When I GET "/ns/b/admin0/claims"
    Then the response JSON path "role" equals "viewer"

  Scenario: A prefixed token carries the prefixed issuer
    When I post to "/ns/a/oauth/token" with form body:
      """
      grant_type=client_credentials
      client_id=demo
      client_secret=x
      audience=http://example/api/v2/
      """
    Then I receive a 200 response
    And the access_token claim "iss" ends with "/ns/a/"

  Scenario: Discovery under a prefix advertises prefixed URLs
    When I GET "/ns/a/.well-known/openid-configuration"
    Then I receive a 200 response
    And the response body contains "/ns/a/.well-known/jwks.json"

  Scenario: The clock moves in one namespace only
    When I PUT "/ns/a/admin0/clock" with body:
      """
      {"now":"2030-01-01T00:00:00Z"}
      """
    Then I receive a 204 response
    When I GET "/ns/a/admin0/clock"
    Then the response JSON path "mode" equals "frozen"
    When I GET "/ns/b/admin0/clock"
    Then the response JSON path "mode" equals "real"

  Scenario: A header and a prefix that disagree are rejected
    When I send "GET /ns/a/admin0/clock" with headers:
      | X-Auth0-Mock-Namespace | b |
    Then I receive a 400 response
    And the response body contains "invalid_namespace"

  Scenario: An invalid namespace id is rejected
    When I GET "/ns/bad.id/admin0/clock"
    Then I receive a 400 response
    And the response body contains "invalid_namespace"

  Scenario: Deleting a namespace drops its state
    Given I PUT "/ns/a/admin0/claims" with body:
      """
      {"role":"admin"}
      """
    When I GET "/admin0/namespaces"
    Then the response JSON path "#" equals "1"
    And the response JSON path "0.id" equals "a"
    When I DELETE "/admin0/namespaces/a"
    Then I receive a 204 response
    When I GET "/admin0/namespaces"
    Then the response body contains "[]"
    When I GET "/ns/a/admin0/claims"
    Then the response JSON path "role" does not exist
    When I DELETE "/admin0/namespaces/nope"
    Then I receive a 404 response
    And the response body contains "namespace_not_found"

  Scenario: Listing namespaces does not create one
    When I GET "/admin0/namespaces"
    Then the response body contains "[]"

  Scenario: The namespace cap answers 429 and spares existing namespaces
    Given I GET "/ns/n1/admin0/clock"
    And I GET "/ns/n2/admin0/clock"
    And I GET "/ns/n3/admin0/clock"
    And I GET "/ns/n4/admin0/clock"
    When I GET "/ns/n5/admin0/clock"
    Then I receive a 429 response
    And the response body contains "too_many_namespaces"
    And the response body contains "NAMESPACE_MAX=4"
    When I GET "/ns/n1/admin0/clock"
    Then I receive a 200 response
    When I DELETE "/admin0/namespaces"
    Then I receive a 204 response
    When I GET "/ns/n5/admin0/clock"
    Then I receive a 200 response
