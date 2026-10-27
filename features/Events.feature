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
          "id":"evt_happypath0000000000000",
          "time":"2026-05-19T00:00:00Z",
          "a0tenant":"my-tenant",
          "a0stream":"est_aaaaaaaaaaaaaaaa000000",
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
      {"type":"user.deleted","offset":"0","event":{"specversion":"1.0","type":"user.deleted","source":"x","id":"evt_skipdelete000000000000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa000000","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","deleted_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I push an event:
      """
      {"type":"user.created","offset":"1","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_keepcreated00000000000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa000000","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    Then the SSE stream delivers an event with id "1" within 3s

  Scenario: event_type filter excludes non-matching events
    When I subscribe to /api/v2/events with query "?event_type=user.created"
    And I push an event:
      """
      {"type":"user.deleted","offset":"0","event":{"specversion":"1.0","type":"user.deleted","source":"x","id":"evt_excludenoise0000000000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa000000","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","deleted_at":"2026-05-19T00:00:00Z","identities":[]}}}}
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
      {"type":"user.created","offset":"0","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_resume0000000001000000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa000000","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I push an event:
      """
      {"type":"user.created","offset":"1","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_resume0000000002000000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa000000","data":{"object":{"user_id":"u-2","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I subscribe to /api/v2/events with header "Last-Event-ID: 0"
    Then the SSE stream delivers an event with id "1" within 3s

  Scenario: ?from query promotes to Last-Event-ID and replays
    When I push an event:
      """
      {"type":"user.created","offset":"0","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_fromquery0000001000000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa000000","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I push an event:
      """
      {"type":"user.created","offset":"1","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_fromquery0000002000000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa000000","data":{"object":{"user_id":"u-2","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I subscribe to /api/v2/events with query "?from=0"
    Then the SSE stream delivers an event with id "1" within 3s

  # ?from_timestamp names an instant, and an instant predating every
  # buffered event asks for the whole buffer. Resolving it to the oldest
  # buffered cursor and replaying strictly after that dropped event "0" —
  # the one event such a caller is certain to want — and at
  # EVENTS_REPLAY_BUFFER=1 dropped the stream entirely.
  Scenario: ?from_timestamp before every buffered event replays the oldest one too
    When I push an event:
      """
      {"type":"user.created","offset":"0","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_fromts0000000001000000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa000000","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I push an event:
      """
      {"type":"user.created","offset":"1","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_fromts0000000002000000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa000000","data":{"object":{"user_id":"u-2","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I subscribe to /api/v2/events with query "?from_timestamp=2020-01-01T00:00:00Z"
    Then the SSE stream delivers events with ids "0,1" within 3s

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

  # A ?from that won't unescape used to be dropped from the parsed query
  # rather than rejected, so the subscriber joined live and missed every
  # event since its cursor — a consumer testing its resume path got a
  # green run that proved nothing.
  Scenario: Unparseable query string returns 400 invalid_query
    When I attempt to subscribe to /api/v2/events with query "?from=0%2"
    Then I receive a 400 response
    And the response body contains "invalid_query"

  # These parse cleanly; what makes them wrong is what the handler used
  # to do with the value. An empty ?from read as "no cursor" and joined
  # live with no 410; an empty ?event_type subscribed the stream to a
  # topic nothing publishes to, so it connected and then never
  # delivered; a repeated ?from silently kept the first value.
  Scenario: An empty from parameter is rejected rather than joining live
    When I attempt to subscribe to /api/v2/events with query "?from="
    Then I receive a 400 response
    And the response body contains "invalid_from"

  Scenario: An empty event_type parameter is rejected rather than subscribing to nothing
    When I attempt to subscribe to /api/v2/events with query "?event_type="
    Then I receive a 400 response
    And the response body contains "invalid_event_type"

  Scenario: A repeated from parameter is rejected rather than silently using the first
    When I attempt to subscribe to /api/v2/events with query "?from=1&from=0"
    Then I receive a 400 response
    And the response body contains "invalid_query"

  Scenario: Subscribing without a bearer returns 401
    When I subscribe to /api/v2/events without a bearer
    Then I receive a 401 response

  Scenario: Reset drains the replay buffer
    When I push an event:
      """
      {"type":"user.created","offset":"0","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_beforereset00000000000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa000000","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
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
      {"type":"user.created","offset":"0","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_postreset0000000000000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa000000","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    Then the SSE stream delivers an event with id "0" within 3s

  Scenario: Expiring the replay buffer ages out a resume cursor
    When I push an event:
      """
      {"type":"user.created","offset":"0","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_expireall0000001000000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa000000","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I expire the events replay buffer
    Then the response JSON path "expired" equals "1"
    When I attempt to subscribe to /api/v2/events with header "Last-Event-ID: 0"
    Then I receive a 410 response
    And the response body contains "event_aged_out"

  Scenario: Expiring before a cursor keeps that cursor replayable
    When I push an event:
      """
      {"type":"user.created","offset":"0","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_expirebefore0001000000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa000000","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I push an event:
      """
      {"type":"user.created","offset":"1","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_expirebefore0002000000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa000000","data":{"object":{"user_id":"u-2","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I push an event:
      """
      {"type":"user.created","offset":"2","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_expirebefore0003000000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa000000","data":{"object":{"user_id":"u-3","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I expire the events replay buffer before "1"
    Then the response JSON path "expired" equals "1"
    When I subscribe to /api/v2/events with query "?from=1"
    Then the SSE stream delivers an event with id "2" within 3s

  Scenario: Expiring before a cursor ages out everything older
    When I push an event:
      """
      {"type":"user.created","offset":"0","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_expireolder00001000000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa000000","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I push an event:
      """
      {"type":"user.created","offset":"1","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_expireolder00002000000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa000000","data":{"object":{"user_id":"u-2","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I expire the events replay buffer before "1"
    And I attempt to subscribe to /api/v2/events with header "Last-Event-ID: 0"
    Then I receive a 410 response
    And the response body contains "event_aged_out"

  # A mistyped cursor must fail here rather than be reported as a
  # successful expiry that dropped nothing — otherwise the scenario
  # carries on and fails several steps later, on a reconnect that came
  # back 200 where it asserted 410.
  Scenario: Expiring an unknown cursor is rejected and leaves the buffer intact
    When I push an event:
      """
      {"type":"user.created","offset":"0","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_expireunknown001000000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa000000","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I attempt to expire the events replay buffer before "999"
    Then I receive a 404 response
    And the response body contains "cursor_not_found"
    When I push an event:
      """
      {"type":"user.created","offset":"1","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_expireunknown002000000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa000000","data":{"object":{"user_id":"u-2","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
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
      {"type":"user.created","offset":"0","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_expirelive000001000000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa000000","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I subscribe to /api/v2/events
    And I expire the events replay buffer
    Then the response JSON path "expired" equals "1"
    When I push an event:
      """
      {"type":"user.created","offset":"1","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_expirelive000002000000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa000000","data":{"object":{"user_id":"u-2","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    Then the SSE stream delivers an event with id "1" within 3s

  Scenario: An empty before parameter is rejected rather than expiring everything
    When I push an event:
      """
      {"type":"user.created","offset":"0","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_emptybefore00001000000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa000000","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I push an event:
      """
      {"type":"user.created","offset":"1","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_emptybefore00002000000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa000000","data":{"object":{"user_id":"u-2","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    And I attempt to expire the events replay buffer with an empty before
    Then I receive a 400 response
    And the response body contains "invalid_before"
    # Prove the buffer survived: resuming from offset 0 must still replay
    # offset 1. Asserting "no event" here would pass either way, since a
    # resume from the newest entry replays nothing regardless.
    When I subscribe to /api/v2/events with query "?from=0"
    Then the SSE stream delivers an event with id "1" within 3s

  # Error stubs let a consumer test its handling of failures the mock
  # can't otherwise produce, such as Auth0's connection limit.
  Scenario: A registered error stub answers in place of the stream
    Given I register an expectation for "GET /api/v2/events" with response:
      """
      {"status":403,"body":{"statusCode":403,"error":"Forbidden","message":"Maximum concurrent event stream connections reached. Please close existing connections."}}
      """
    When I attempt to subscribe to /api/v2/events
    Then I receive a 403 response
    And the response JSON path "message" equals "Maximum concurrent event stream connections reached. Please close existing connections."

  Scenario: A request-matched error stub leaves other subscribers on the stream
    Given I register the expectation "GET /api/v2/events":
      """
      {"request":{"headers":{"Last-Event-ID":"42"}},"response":{"status":410,"body":{"statusCode":410,"error":"Gone","message":"stubbed cursor loss","errorCode":"event_aged_out"}}}
      """
    When I attempt to subscribe to /api/v2/events with header "Last-Event-ID: 42"
    Then I receive a 410 response
    And the response JSON path "message" equals "stubbed cursor loss"
    When I subscribe to /api/v2/events
    And I push an event:
      """
      {"type":"user.created","offset":"0","event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_stubbedstream001000000","time":"2026-05-19T00:00:00Z","a0tenant":"t-1","a0stream":"est_aaaaaaaaaaaaaaaa000000","data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}}
      """
    Then the SSE stream delivers an event with id "0" within 3s

  Scenario: A success stub for the stream is rejected
    When I attempt to register an expectation for "GET /api/v2/events" with response:
      """
      {"status":200}
      """
    Then I receive a 400 response
    And the response body contains "POST /admin0/events"
