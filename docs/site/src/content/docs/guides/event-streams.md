---
title: Event streams
description: Drive an event-stream consumer from a test.
sidebar:
  order: 4
---

## Drive an event-stream consumer from a test

Use this when your service consumes Auth0's event stream and you want
to assert it reacts correctly to specific events. The mock exposes
`GET /api/v2/events` as a real SSE endpoint and `POST /admin0/events`
as the producer side. The `auth0mocktest` helpers handle the SSE
framing so the test stays focused on intent.

```go
import (
    "testing"
    "time"

    "github.com/sergiught/auth0-mock/pkg/auth0mock"
    "github.com/sergiught/auth0-mock/pkg/auth0mock/auth0mocktest"
)

func TestConsumer_ReactsToUserCreated(t *testing.T) {
    c, err := auth0mock.NewClient("http://localhost:8080")
    if err != nil { t.Fatal(err) }
    auth0mocktest.ResetOnCleanup(t, c)

    bearer := mintBearerForYourConsumer(t) // however your test rig gets one

    // Open a subscription with an event-type filter. The helper
    // strips keep-alives and yields one SSEEvent per real frame.
    stream := auth0mocktest.SubscribeEvents(t, c, bearer, "event_type=user.created")

    // Block until the subscription has registered server-side, rather
    // than sleeping a fixed guess. active is eventually-consistent, so
    // this is the deterministic way to know a pushed event will be
    // fanned out to this stream.
    auth0mocktest.WaitForActiveSubscribers(t, c, 1, time.Second)

    // Push an event. The mock validates the envelope against the
    // OpenAPI text/event-stream schema; misshapen bodies surface
    // as APIError("invalid_event") with a one-line reason. Use
    // auth0mock.NewEventID() to get a fresh ID that conforms to the
    // `evt_` + 22-char pattern — the schema rejects literals of the
    // wrong length, and hand-counting is a common paste-and-go trap.
    eventID := auth0mock.NewEventID()
    streamID := auth0mock.NewStreamID()
    const offset = "0" // the resume cursor; surfaces as the SSE id
    auth0mocktest.MustPush(t, c, fmt.Sprintf(`{
        "type":"user.created","offset":%q,
        "event":{
            "specversion":"1.0","type":"user.created","source":"https://auth0.local/",
            "id":%q,"time":"2026-05-19T00:00:00Z",
            "a0tenant":"my-tenant","a0stream":%q,
            "data":{"object":{
                "user_id":"u-1",
                "created_at":"2026-05-19T00:00:00Z",
                "updated_at":"2026-05-19T00:00:00Z",
                "identities":[]
            }}
        }
    }`, offset, eventID, streamID))

    // Block until your consumer (downstream of the SSE stream)
    // observes the event, then assert it reacted as expected. Matching
    // Auth0's Events API, the SSE id is the offset (the resume cursor),
    // not the CloudEvent event.id.
    evt := stream.NextEvent(t, 3*time.Second)
    if evt.ID != offset {
        t.Fatalf("got id=%q want offset %s", evt.ID, offset)
    }
    // ... drive your consumer's assertion here ...
}
```

**Resume from a known offset** by passing `from=<offset>` or
`from_timestamp=<rfc3339>` as the query string — the offset is the value
a prior event delivered in its SSE `id:` line. The mock keeps a bounded
ring buffer (default 100 events, see `EVENTS_REPLAY_BUFFER`) and replays
missed events on reconnect.

**Simulate an in-band stream error** to exercise a consumer's reconnect
or cursor-fallback path by pushing an error frame:

```go
auth0mocktest.MustPush(t, c, `{"type":"error","error":{"code":"cursor_expired","message":"cursor expired"}}`)
```

Matching Auth0, error frames are control signals, not events: every
subscriber receives them regardless of `event_type` filter, they're
never buffered/replayed (no offset), and the stream **closes** right
after — which is what drives your consumer to reconnect.

**Advance the cursor during idle** with an `offset-only` progress marker —
exercise a consumer that persists offsets from non-event messages and
resumes after a quiet period:

```go
auth0mocktest.MustPush(t, c, `{"type":"offset-only","offset":"42"}`)
```

Like error frames, markers reach every subscriber regardless of
`event_type` filter. But they carry an offset, so (unlike errors) they
**are** buffered and `from=42` is a valid resume point.

**Assert a stream closed cleanly** by waiting for the active count to
drain after you (or your consumer-under-test) disconnect:

```go
stream.Close() // or let your consumer shut down
auth0mocktest.WaitForActiveSubscribers(t, c, 0, time.Second)
```

For lifetime counts — e.g. asserting a consumer reconnected — read
`c.Events.Subscribers(ctx)` and check `.Total` (connections since the
last reset; it never decreases within a window).

**Reset between tests** via `auth0mocktest.ResetOnCleanup(t, c)` —
this drains every open subscriber and clears the replay buffer
without permanently breaking the hub, so the next test starts from a
known-empty state.

**Force a cursor to age out** when you need to exercise a consumer's
unrecoverable-cursor-loss path. `ExpireEvents` expires every buffered
cursor, so the next reconnect presenting one gets `410 event_aged_out`;
`ExpireEventsBefore` expires only what is older than the cursor you
name, leaving that cursor and everything after it replayable. Both
report how many cursors they dropped.

Calling `ExpireEventsBefore` twice with the same cursor is safe — the
boundary survives its own expiry, so the second call drops 0. A cursor
the buffer doesn't hold at all is an error (`404 cursor_not_found`), so
a mistyped offset fails at the expire call rather than at the reconnect
several steps later.

```go
auth0mocktest.MustPush(t, c, `{"type":"user.created","offset":"1", ...}`)
auth0mocktest.MustPush(t, c, `{"type":"user.created","offset":"2", ...}`)

// Offset 1 is now unresumable; offset 2 still replays what follows it.
expired, err := c.Events.ExpireEventsBefore(ctx, "2")
require.NoError(t, err)
require.Equal(t, 1, expired)
```

Prefer this over pushing past `EVENTS_REPLAY_BUFFER` to force natural
eviction (slow, and couples the test to the buffer's capacity) or over
a full reset (which drops every other store and disconnects
subscribers). Expiry touches the replay buffer only: subscribers that
are already streaming keep receiving events.

### Stub a failure the stream can't produce on its own

Some Auth0 failures — the concurrent-connection limit, a rate limit, a
5xx — have no natural trigger in the mock. Register an error stub for
`GET /api/v2/events` and matching subscribe requests get it instead of
a stream:

```go
_, err := c.ExpectGet("/api/v2/events").
    Respond(http.StatusForbidden).
    JSON(map[string]any{
        "statusCode": 403,
        "error":      "Forbidden",
        "message":    "Maximum concurrent event stream connections reached. Please close existing connections.",
    }).
    Apply(ctx)
require.NoError(t, err)
```

Add a matcher to fail only some subscribers — `WithHeader("Last-Event-ID", "42")`
returns the stub to a consumer resuming from `42`, while everyone else
still gets the live stream. Stubs are checked before the stream's own
resume checks, so one can stand in for a real `410`.

Only error statuses (`>= 400`) are accepted for this route. A `2xx` stub
is refused at registration: the success answer is the live stream, fed
by `Events.Push` / `POST /admin0/events`.

**Common errors** the mock returns:

| Status | `errorCode` | Cause |
|---|---|---|
| 400 | `invalid_event` | Schema violation. The `message` field lists each failed `/json/pointer: reason` on a single line. |
| 400 | `invalid_from_timestamp` | `?from_timestamp` isn't valid RFC 3339, or was supplied empty or padded with whitespace. It is validated even when `?from` or `Last-Event-ID` already names the cursor, so a malformed value can't ride along unread. |
| 400 | `invalid_from` | `?from` was supplied empty or padded with whitespace. Omit the parameter to join live — an empty value is refused rather than read as "no cursor", because a client templating `?from=${cursor}` with an unset variable would otherwise get a `200` and silently miss everything buffered since its cursor. |
| 400 | `invalid_event_type` | `?event_type` was supplied empty, padded with whitespace, or naming one of the mock's internal topics (`__broadcast__`, `__keep_alive__`, `__barrier__`, `__replay_all__`). Each value becomes a topic name verbatim: an empty or padded one subscribed the stream to a topic nothing publishes to, so it connected and then never delivered; an internal one collected the unfiltered fan-out, turning a filtered subscription into the firehose the filter exists to avoid. Omit the parameter to receive every event. |
| 400 | `invalid_last_event_id` | The `Last-Event-ID` header was sent but empty, or sent more than once. Omit the header to join live — a cursor can be named three ways (this header, `?from`, `?from_timestamp`) and present-but-empty is refused for all three. |
| 400 | `invalid_before` | `POST /admin0/events/expire?before=` was present but empty, or padded with whitespace. Omit the parameter to expire the whole buffer — an empty value is refused rather than treated as expire-everything, and a padded one is refused rather than reported as a cursor the buffer doesn't hold. |
| 400 | `invalid_query` | The query string wouldn't parse — a stray `%` that isn't a valid escape, or an unencoded `;`, which Go refuses as a query separator — or a resume cursor was named twice (`?from`, `?from_timestamp`). A repeated `Last-Event-ID` header reports `invalid_last_event_id` instead, since nothing about the query string is wrong. |
| 404 | `cursor_not_found` | `POST /admin0/events/expire?before=` named a cursor the replay buffer doesn't hold — mistyped, already evicted, or the mock runs with `EVENTS_REPLAY_BUFFER=0`. Reported rather than folded into a `200` with `expired: 0`, which couldn't say whether nothing was older or the cursor was never there. |
| 410 | `event_aged_out` | `Last-Event-ID` / `?from` references an event the ring buffer no longer holds — through natural eviction, `POST /admin0/events/expire`, or `EVENTS_REPLAY_BUFFER=0`, where there is no buffer to hold it and joining live would silently skip the window the caller asked to resume. A `?from_timestamp` resolves against the buffer instead, so it never 410s: it replays whatever survives, and joins live only when nothing does. |
| 401 | _bearer envelope_ | No / invalid bearer on `/api/v2/events`. |

On `GET /api/v2/events`, one unparseable pair rejects the whole request. Go's parser keeps the pairs it managed and discards both the error and the pair it choked on, so a `?from` that vanished would join live instead of 410-ing, and a vanished `?event_type` would turn a filtered subscription into a firehose — answering `200` on the strength of the pairs that survived would serve a request nobody made. Percent-encode a `%` or `;` that a cursor genuinely contains (`%25`, `%3B`); the `message` field carries the parser's own reason. Unknown parameters are ignored, as the real Auth0 API ignores them.

`POST /admin0/events/expire` is stricter still: it also rejects any parameter other than `before`, and a repeated `before`, because omitting `before` means "expire everything" and a typo must not be indistinguishable from omission.
