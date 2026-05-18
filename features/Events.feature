Feature: GET /events Server-Sent Events
  Background:
    Given the mock is running
    And I have a valid bearer token

  Scenario: Pushed event is delivered to a connected subscriber
    When I subscribe to /api/v2/events
    And I push an event:
      """
      {
        "type":"user.created",
        "offset":"0",
        "event":{
          "specversion":"1.0",
          "type":"user.created",
          "source":"https://auth0.local/",
          "id":"evt_aaaaaaaaaaaaaaaa",
          "time":"2026-05-19T00:00:00Z",
          "a0tenant":"my-tenant",
          "a0stream":"est_aaaaaaaaaaaaaaaa",
          "data":{"object":{
            "user_id":"u-1",
            "created_at":"2026-05-19T00:00:00Z",
            "updated_at":"2026-05-19T00:00:00Z",
            "identities":[]
          }}
        }
      }
      """
    Then the SSE stream delivers an event with id "evt_aaaaaaaaaaaaaaaa" within 3s
