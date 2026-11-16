Feature: Device login flow
  A device asks /oauth/device/code for a code, then polls /oauth/token while
  the user approves it at /activate (LOGIN_PAGE on) or gets it approved at
  once (LOGIN_PAGE off).

  Scenario: With the page off, the first poll returns tokens
    Given the mock is running
    When I post to "/oauth/device/code" with form body:
      """
      client_id=tv
      """
    Then I receive a 200 response
    And I save the response JSON path "device_code" as "dc"
    When I post to "/oauth/token" with form body using saved values:
      """
      grant_type=urn:ietf:params:oauth:grant-type:device_code
      client_id=tv
      device_code={{dc}}
      """
    Then I receive a 200 response
    And the response JSON path "access_token" exists
    And the access_token claim "sub" equals "auth0|mock-user"

  Scenario: With the page on, a poll is pending
    Given the mock is running with LOGIN_PAGE
    When I post to "/oauth/device/code" with form body:
      """
      client_id=tv
      """
    And I save the response JSON path "device_code" as "dc"
    And I post to "/oauth/token" with form body using saved values:
      """
      grant_type=urn:ietf:params:oauth:grant-type:device_code
      client_id=tv
      device_code={{dc}}
      """
    Then I receive a 403 response
    And the response JSON path "error" equals "authorization_pending"

  Scenario: Activating the code through the page approves the device
    Given the mock is running with LOGIN_PAGE
    When I post to "/oauth/device/code" with form body:
      """
      client_id=tv
      """
    And I save the response JSON path "device_code" as "dc"
    And I save the response JSON path "user_code" as "uc"
    And I get "/activate?user_code={{uc}}" using saved values
    Then I receive a 200 response
    And the response body has a form field named "code"
    When I post to "/activate" with form body using saved values:
      """
      code={{uc}}
      action=default
      """
    Then I receive a 200 response
    And the response body has a form field named "username"
    And I save the form field "state" from the page as "step"
    When I post to "/activate" with form body using saved values:
      """
      state={{step}}
      username=jane@example.com
      password=anything
      action=default
      """
    Then I receive a 200 response
    And the response body contains "Device Confirmation"
    And I save the form field "state" from the page as "confirm"
    When I post to "/activate" with form body using saved values:
      """
      state={{confirm}}
      action=confirm
      """
    Then I receive a 200 response
    And the response body contains "all set"
    When I post to "/oauth/token" with form body using saved values:
      """
      grant_type=urn:ietf:params:oauth:grant-type:device_code
      client_id=tv
      device_code={{dc}}
      """
    Then I receive a 200 response
    And the access_token claim "sub" equals "jane@example.com"

  Scenario: Cancelling at the page denies the device
    Given the mock is running with LOGIN_PAGE
    When I post to "/oauth/device/code" with form body:
      """
      client_id=tv
      """
    And I save the response JSON path "device_code" as "dc"
    And I save the response JSON path "user_code" as "uc"
    And I post to "/activate" with form body using saved values:
      """
      code={{uc}}
      action=default
      """
    And I save the form field "state" from the page as "step"
    And I post to "/activate" with form body using saved values:
      """
      state={{step}}
      username=jane@example.com
      password=anything
      action=default
      """
    And I save the form field "state" from the page as "confirm"
    And I post to "/activate" with form body using saved values:
      """
      state={{confirm}}
      action=cancel
      """
    Then I receive a 200 response
    And the response body contains "Device activation cancelled"
    When I post to "/oauth/token" with form body using saved values:
      """
      grant_type=urn:ietf:params:oauth:grant-type:device_code
      client_id=tv
      device_code={{dc}}
      """
    Then I receive a 403 response
    And the response JSON path "error" equals "access_denied"

  Scenario: Discovery lists the device authorization endpoint
    Given the mock is running
    When I send "GET /.well-known/openid-configuration" without a bearer
    Then I receive a 200 response
    And the response body contains "device_authorization_endpoint"
