Feature: GET /api/v2/events Server-Sent Events
  Background:
    Given the mock is running
    And I have a valid bearer token
    And I reset all mock state

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
          "id":"evt_happypath0000000",
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
    Then the SSE stream delivers an event with id "0" within 3s

  Scenario: event_type filter delivers only matching events
    When I subscribe to /api/v2/events with query "?event_type=user.created"
    And I push an event:
      """
      {"type":"user.deleted","offset":"0","event":{"specversion":"1.0","type":"user.deleted","source":"x","id":"evt_skipdelete000000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","deleted_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I push an event:
      """
      {"type":"user.created","offset":"1","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_keepcreated00000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    Then the SSE stream delivers an event with id "1" within 3s

  Scenario: event_type filter excludes non-matching events
    When I subscribe to /api/v2/events with query "?event_type=user.created"
    And I push an event:
      """
      {"type":"user.deleted","offset":"0","event":{"specversion":"1.0","type":"user.deleted","source":"x","id":"evt_excludenoise0000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","deleted_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    Then the SSE stream delivers no event within 1s

  Scenario: offset-only progress marker reaches a filtered subscriber
    When I subscribe to /api/v2/events with query "?event_type=user.created"
    And I push an event:
      """
      {"type":"offset-only","offset":"9"}
      """
    Then the SSE stream delivers an event with id "9" within 3s

  Scenario: Last-Event-ID header replays missed events
    When I push an event:
      """
      {"type":"user.created","offset":"0","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_resume0000000001","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I push an event:
      """
      {"type":"user.created","offset":"1","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_resume0000000002","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa","data":{"object":{"user_id":"u-2","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I subscribe to /api/v2/events with header "Last-Event-ID: 0"
    Then the SSE stream delivers an event with id "1" within 3s

  Scenario: ?from query promotes to Last-Event-ID and replays
    When I push an event:
      """
      {"type":"user.created","offset":"0","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_fromquery0000001","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I push an event:
      """
      {"type":"user.created","offset":"1","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_fromquery0000002","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa","data":{"object":{"user_id":"u-2","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I subscribe to /api/v2/events with query "?from=0"
    Then the SSE stream delivers an event with id "1" within 3s

  Scenario: Last-Event-ID for an unknown event returns 410 event_aged_out
    When I attempt to subscribe to /api/v2/events with header "Last-Event-ID: 999"
    Then I receive a 410 response
    And the response body contains "event_aged_out"

  Scenario: Schema-violating push returns 400 invalid_event
    When I attempt to push an event:
      """
      {"type":"not.a.real.event","offset":"0","event":{}}
      """
    Then I receive a 400 response
    And the response body contains "invalid_event"

  Scenario: Unparseable from_timestamp returns 400 invalid_from_timestamp
    When I attempt to subscribe to /api/v2/events with query "?from_timestamp=not-a-timestamp"
    Then I receive a 400 response
    And the response body contains "invalid_from_timestamp"

  Scenario: Subscribing without a bearer returns 401
    When I subscribe to /api/v2/events without a bearer
    Then I receive a 401 response

  Scenario: Reset drains the replay buffer
    When I push an event:
      """
      {"type":"user.created","offset":"0","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_beforereset00000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I reset all mock state
    And I attempt to subscribe to /api/v2/events with header "Last-Event-ID: 0"
    Then I receive a 410 response
    And the response body contains "event_aged_out"

  Scenario: Reset leaves the hub functional for the next test
    When I reset all mock state
    And I subscribe to /api/v2/events
    And I push an event:
      """
      {"type":"user.created","offset":"0","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_postreset0000000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    Then the SSE stream delivers an event with id "0" within 3s

  Scenario: Expiring the replay buffer ages out a resume cursor
    When I push an event:
      """
      {"type":"user.created","offset":"0","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_expireall0000001","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I expire the events replay buffer
    Then the response JSON path "expired" equals "1"
    When I attempt to subscribe to /api/v2/events with header "Last-Event-ID: 0"
    Then I receive a 410 response
    And the response body contains "event_aged_out"

  Scenario: Expiring before a cursor keeps that cursor replayable
    When I push an event:
      """
      {"type":"user.created","offset":"0","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_expirebefore0001","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I push an event:
      """
      {"type":"user.created","offset":"1","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_expirebefore0002","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa","data":{"object":{"user_id":"u-2","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I push an event:
      """
      {"type":"user.created","offset":"2","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_expirebefore0003","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa","data":{"object":{"user_id":"u-3","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I expire the events replay buffer before "1"
    Then the response JSON path "expired" equals "1"
    When I subscribe to /api/v2/events with query "?from=1"
    Then the SSE stream delivers an event with id "2" within 3s

  Scenario: Expiring before a cursor ages out everything older
    When I push an event:
      """
      {"type":"user.created","offset":"0","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_expireolder00001","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I push an event:
      """
      {"type":"user.created","offset":"1","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_expireolder00002","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa","data":{"object":{"user_id":"u-2","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I expire the events replay buffer before "1"
    And I attempt to subscribe to /api/v2/events with header "Last-Event-ID: 0"
    Then I receive a 410 response
    And the response body contains "event_aged_out"

  Scenario: Expiring an unknown cursor is a no-op
    When I push an event:
      """
      {"type":"user.created","offset":"0","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_expireunknown001","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I expire the events replay buffer before "999"
    Then the response JSON path "expired" equals "0"
    When I push an event:
      """
      {"type":"user.created","offset":"1","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_expireunknown002","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa","data":{"object":{"user_id":"u-2","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I subscribe to /api/v2/events with query "?from=0"
    Then the SSE stream delivers an event with id "1" within 3s

  # Seed a cursor BEFORE subscribing so the expire has something real to
  # drop — expiring an empty buffer would pass even if expiry were a
  # no-op. Only one "delivers" assertion: that step closes the stream
  # when it returns, so it can't be used twice on one subscription.
  Scenario: Expiring the buffer leaves a connected subscriber streaming
    When I push an event:
      """
      {"type":"user.created","offset":"0","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_expirelive000001","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I subscribe to /api/v2/events
    And I expire the events replay buffer
    Then the response JSON path "expired" equals "1"
    When I push an event:
      """
      {"type":"user.created","offset":"1","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_expirelive000002","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa","data":{"object":{"user_id":"u-2","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    Then the SSE stream delivers an event with id "1" within 3s

  Scenario: An empty before parameter is rejected rather than expiring everything
    When I push an event:
      """
      {"type":"user.created","offset":"0","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_emptybefore00001","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I attempt to expire the events replay buffer with an empty before
    Then I receive a 400 response
    And the response body contains "invalid_before"
    When I subscribe to /api/v2/events with query "?from=0"
    Then the SSE stream delivers no event within 1s
