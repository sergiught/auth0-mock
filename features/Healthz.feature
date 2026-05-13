Feature: /healthz liveness probe
  Background:
    Given the mock is running

  Scenario: /healthz returns 200 without authentication
    When I send "GET /healthz" without a bearer
    Then I receive a 200 response
    And the response JSON path "status" equals "ok"
