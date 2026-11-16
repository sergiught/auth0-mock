Feature: Organization logins
  The organization login parameter becomes org_id and org_name claims.
  No organizations are stored; the values are taken from the request.

  Background:
    Given the mock is running

  Scenario: Code flow with an organization id adds org_id only
    When I send "GET /authorize?client_id=demo&redirect_uri=https://app/cb&response_type=code&audience=https://api.example.com&scope=openid&organization=org_abc123" without a bearer
    And I save the code from the redirect as "c1"
    And I post to "/oauth/token" with form body using saved values:
      """
      grant_type=authorization_code
      client_id=demo
      code={{c1}}
      redirect_uri=https://app/cb
      """
    Then I receive a 200 response
    And the access_token claim "org_id" equals "org_abc123"
    And the access_token claim "org_name" is absent
    And the id_token claim "org_id" equals "org_abc123"

  Scenario: Code flow with an organization name adds both claims
    When I send "GET /authorize?client_id=demo&redirect_uri=https://app/cb&response_type=code&audience=https://api.example.com&scope=openid&organization=Acme&invitation=inv_1" without a bearer
    And I save the code from the redirect as "c1"
    And I post to "/oauth/token" with form body using saved values:
      """
      grant_type=authorization_code
      client_id=demo
      code={{c1}}
      redirect_uri=https://app/cb
      """
    Then I receive a 200 response
    And the access_token claim "org_name" equals "acme"
    And the access_token claim "org_id" equals "org_822b33ad87c148a0"
    And the id_token claim "org_name" equals "acme"
    And the id_token claim "org_id" equals "org_822b33ad87c148a0"

  Scenario: A refreshed token keeps the organization
    When I send "GET /authorize?client_id=demo&redirect_uri=https://app/cb&response_type=code&audience=https://api.example.com&scope=openid%20offline_access&organization=acme" without a bearer
    And I save the code from the redirect as "c1"
    And I post to "/oauth/token" with form body using saved values:
      """
      grant_type=authorization_code
      client_id=demo
      code={{c1}}
      redirect_uri=https://app/cb
      """
    And I save the refresh_token from the response
    And I refresh with the saved refresh_token and form body:
      """
      client_id=demo
      organization=other
      """
    Then I receive a 200 response
    And the access_token claim "org_name" equals "acme"
    And the access_token claim "org_id" equals "org_822b33ad87c148a0"

  Scenario: client_credentials accepts an organization
    When I post to "/oauth/token" with form body:
      """
      grant_type=client_credentials
      client_id=demo
      client_secret=x
      audience=https://api.example.com
      organization=acme
      """
    Then I receive a 200 response
    And the access_token claim "org_name" equals "acme"
    And the access_token claim "org_id" equals "org_822b33ad87c148a0"

  Scenario: A password grant ignores the organization
    When I post to "/oauth/token" with form body:
      """
      grant_type=password
      client_id=demo
      username=alice@example.com
      password=ignored
      audience=https://api.example.com
      organization=acme
      """
    Then I receive a 200 response
    And the access_token claim "org_id" is absent

  Scenario: A non-member is rejected and a member logs in
    Given I POST "/admin0/authorize-errors" with body:
      """
      {"match":{"organization":"org_abc123","login_hint":"bob@example.com"},"error":"access_denied","error_description":"user bob@example.com is not a member of org_abc123"}
      """
    And I receive a 201 response
    When I send "GET /authorize?client_id=demo&redirect_uri=https://app/cb&response_type=code&organization=org_abc123&login_hint=bob@example.com" without a bearer
    Then I receive a 302 response
    And the response Location header contains "error=access_denied"
    When I send "GET /authorize?client_id=demo&redirect_uri=https://app/cb&response_type=code&organization=org_abc123&login_hint=alice@example.com" without a bearer
    Then I receive a 302 response
    And the response Location header contains "code="
