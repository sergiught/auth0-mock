Feature: Token exchange grant
  A subject token names the user; no signature is checked.

  Background:
    Given the mock is running

  Scenario: A plain string subject becomes the user id
    When I post to "/oauth/token" with form body:
      """
      grant_type=urn:ietf:params:oauth:grant-type:token-exchange
      client_id=demo
      subject_token=auth0|jane
      subject_token_type=https://example.com/token-types/legacy
      audience=https://api.example.com
      scope=openid
      """
    Then I receive a 200 response
    And the response JSON path "issued_token_type" equals "urn:ietf:params:oauth:token-type:access_token"
    And the access_token claim "gty" equals "token-exchange"
    And the access_token claim "sub" equals "auth0|jane"

  Scenario: An unsigned JWT subject supplies sub and profile claims
    When I post to "/oauth/token" with form body:
      """
      grant_type=urn:ietf:params:oauth:grant-type:token-exchange
      client_id=demo
      subject_token=eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJzdWIiOiJhdXRoMHxqYW5lIiwiZW1haWwiOiJqYW5lQGV4YW1wbGUuY29tIn0.x
      subject_token_type=urn:example:legacy-token
      audience=https://api.example.com
      scope=openid email
      """
    Then I receive a 200 response
    And the access_token claim "sub" equals "auth0|jane"
    And the id_token claim "sub" equals "auth0|jane"
    And the id_token claim "email" equals "jane@example.com"

  Scenario: An expired JWT subject is rejected
    When I post to "/oauth/token" with form body:
      """
      grant_type=urn:ietf:params:oauth:grant-type:token-exchange
      client_id=demo
      subject_token=eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJzdWIiOiJhdXRoMHxqYW5lIiwiZXhwIjoxfQ.x
      subject_token_type=urn:example:legacy-token
      """
    Then I receive a 400 response
    And the response JSON path "error" equals "invalid_request"
    And the response JSON path "error_description" equals "Invalid subject token"

  Scenario: The organization parameter adds org claims
    When I post to "/oauth/token" with form body:
      """
      grant_type=urn:ietf:params:oauth:grant-type:token-exchange
      client_id=demo
      subject_token=auth0|jane
      subject_token_type=urn:example:legacy-token
      audience=https://api.example.com
      organization=acme
      """
    Then I receive a 200 response
    And the access_token claim "org_name" equals "acme"
    And the access_token claim "org_id" equals "org_822b33ad87c148a0"

  Scenario: A reserved subject token type is rejected
    When I post to "/oauth/token" with form body:
      """
      grant_type=urn:ietf:params:oauth:grant-type:token-exchange
      client_id=demo
      subject_token=auth0|jane
      subject_token_type=urn:ietf:params:oauth:token-type:access_token
      """
    Then I receive a 400 response
    And the response JSON path "error" equals "invalid_request"

  Scenario: Discovery lists the grant
    When I send "GET /.well-known/openid-configuration" without a bearer
    Then I receive a 200 response
    And the response body contains "urn:ietf:params:oauth:grant-type:token-exchange"
