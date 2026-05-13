Feature: Database connection signup and password change
  Background:
    Given the mock is running

  Scenario: signup creates a user-like record
    When I post to "/dbconnections/signup" with body:
      """
      {"client_id":"demo","email":"alice@example.com","password":"pw","connection":"Username-Password-Authentication"}
      """
    Then I receive a 201 response
    And the response body should match the JSON pattern:
      """
      {
        "_id":            "<<PRESENCE>>",
        "email":          "alice@example.com",
        "email_verified": false
      }
      """

  Scenario: signup without email is 400
    When I post to "/dbconnections/signup" with body:
      """
      {"client_id":"demo","password":"pw","connection":"x"}
      """
    Then I receive a 400 response

  Scenario: change_password returns the canned message
    When I post to "/dbconnections/change_password" with body:
      """
      {"client_id":"demo","email":"alice@example.com","connection":"x"}
      """
    Then I receive a 200 response
    And the response body contains "We've just sent you an email"
