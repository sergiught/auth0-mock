Feature: /openapi.* spec endpoints
  Background:
    Given the mock is running

  Scenario: /openapi.json serves the embedded merged spec
    When I send "GET /openapi.json" without a bearer
    Then I receive a 200 response
    And the response JSON path "openapi" equals "3.1.0"
    And the response JSON path "info.title" exists

  Scenario: /openapi.yaml serves a YAML-encoded copy of the same spec
    When I send "GET /openapi.yaml" without a bearer
    Then I receive a 200 response
