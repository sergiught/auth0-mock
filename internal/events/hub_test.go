package events_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/events"
)

// readOneEvent reads bytes from r until it sees a complete SSE event
// (terminated by a blank line). Returns the raw frame. Bails after
// d if nothing arrives.
func readOneEvent(t *testing.T, r *bufio.Reader, d time.Duration) string {
	t.Helper()
	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				done <- b.String()
				return
			}
			b.WriteString(line)
			if line == "\n" || line == "\r\n" {
				done <- b.String()
				return
			}
		}
	}()
	select {
	case f := <-done:
		return f
	case <-time.After(d):
		t.Fatalf("timeout waiting for SSE event")
		return ""
	}
}

// subscribe opens a GET /events request against srv and returns a
// (bufio.Reader, cancel) pair. The caller is responsible for cancel().
func subscribe(t *testing.T, srv *httptest.Server, query string) (*bufio.Reader, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+query, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "text/event-stream")
	r := bufio.NewReader(resp.Body)
	// Consume the connect announcement frame the handler sends, so callers
	// read events directly. Assert it really is that frame, so a future
	// regression can't silently swallow the first real event here.
	require.Contains(t, readOneEvent(t, r, 2*time.Second), ":connected")
	return r, cancel
}

func TestHub_NewHub_ZeroBufferDisablesReplayer(t *testing.T) {
	h, err := events.NewHub(0, nil)
	require.NoError(t, err)
	require.NotNil(t, h)
	// We can publish without panicking even with no replayer.
	err = h.Publish(events.Event{Type: "user.created", Payload: json.RawMessage(`{"type":"user.created"}`)})
	assert.NoError(t, err)
	require.NoError(t, h.Shutdown(context.Background()))
}

func TestHub_NewHub_NegativeBufferDisablesReplayer(t *testing.T) {
	h, err := events.NewHub(-5, nil)
	require.NoError(t, err)
	require.NotNil(t, h)
	require.NoError(t, h.Shutdown(context.Background()))
}

func TestHub_Publish_NoSubscribersDoesNotError(t *testing.T) {
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	err = h.Publish(events.Event{
		Type:    "user.created",
		ID:      "evt-1",
		Payload: json.RawMessage(`{"type":"user.created","id":"evt-1"}`),
	})
	assert.NoError(t, err)
}

func TestHub_Shutdown_IsIdempotent(t *testing.T) {
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, h.Shutdown(ctx))
	// Second call: behaviour is "don't blow up", not "return a specific
	// error". Sse.Server.Shutdown may return an error on a closed
	// server; we accept either nil or a non-panicking error.
	_ = h.Shutdown(ctx)
}

// firstFrame opens a raw subscription (bypassing the subscribe helper, which
// consumes the connect frame) and returns the first SSE frame.
func firstFrame(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusOK, resp.StatusCode)
	return readOneEvent(t, bufio.NewReader(resp.Body), 2*time.Second)
}

// TestHub_Handler_SendsConnectedAndRetryOnConnect verifies the first frame on
// a fresh stream is the connect announcement Auth0's Events API sends: a
// :connected readiness comment plus a retry: reconnect hint.
func TestHub_Handler_SendsConnectedAndRetryOnConnect(t *testing.T) {
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	frame := firstFrame(t, srv)
	assert.Contains(t, frame, ":connected")
	assert.Contains(t, frame, "retry: 3000")
}

// TestHub_Handler_ReconnectHintConfigurable verifies WithReconnectHint controls
// the retry: value, and that a non-positive value omits the hint entirely.
func TestHub_Handler_ReconnectHintConfigurable(t *testing.T) {
	t.Run("custom value", func(t *testing.T) {
		h, err := events.NewHub(10, nil, events.WithReconnectHint(5*time.Second))
		require.NoError(t, err)
		t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
		srv := httptest.NewServer(h.Handler())
		t.Cleanup(srv.Close)

		assert.Contains(t, firstFrame(t, srv), "retry: 5000")
	})

	t.Run("zero omits the hint", func(t *testing.T) {
		h, err := events.NewHub(10, nil, events.WithReconnectHint(0))
		require.NoError(t, err)
		t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
		srv := httptest.NewServer(h.Handler())
		t.Cleanup(srv.Close)

		frame := firstFrame(t, srv)
		assert.Contains(t, frame, ":connected")
		assert.NotContains(t, frame, "retry:")
	})

	// A sub-millisecond hint would round to `retry: 0` (reconnect
	// immediately); it's omitted instead.
	t.Run("sub-millisecond omits the hint", func(t *testing.T) {
		h, err := events.NewHub(10, nil, events.WithReconnectHint(500*time.Microsecond))
		require.NoError(t, err)
		t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
		srv := httptest.NewServer(h.Handler())
		t.Cleanup(srv.Close)

		frame := firstFrame(t, srv)
		assert.Contains(t, frame, ":connected")
		assert.NotContains(t, frame, "retry:")
	})
}

func TestHub_Handler_FilterlessSubscriberSeesAllEvents(t *testing.T) {
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	r, cancel := subscribe(t, srv, "")
	defer cancel()

	// Give the subscription a moment to register before publishing.
	time.Sleep(50 * time.Millisecond)

	require.NoError(t, h.Publish(events.Event{
		Type:    "user.created",
		ID:      "evt-1",
		Payload: json.RawMessage(`{"type":"user.created","id":"evt-1"}`),
	}))

	frame := readOneEvent(t, r, 2*time.Second)
	assert.Contains(t, frame, "id: evt-1")
	assert.Contains(t, frame, "event: user.created")
	assert.Contains(t, frame, `data: {"type":"user.created","id":"evt-1"}`)
}

func TestHub_Handler_TypelessEventBroadcastsToFilterlessSubscriber(t *testing.T) {
	// An event with no Type is published to broadcastTopic only (it has
	// no type topic to also target). A filterless subscriber still
	// receives it, rendered as an id+data frame with no `event:` line.
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	r, cancel := subscribe(t, srv, "")
	defer cancel()
	time.Sleep(50 * time.Millisecond)

	require.NoError(t, h.Publish(events.Event{
		ID:      "evt-typeless",
		Payload: json.RawMessage(`{"id":"evt-typeless"}`),
	}))

	frame := readOneEvent(t, r, 2*time.Second)
	assert.Contains(t, frame, "id: evt-typeless")
	assert.Contains(t, frame, `data: {"id":"evt-typeless"}`)
}

// TestHub_Handler_ErrorFrameReachesFilteredSubscriberAndCloses verifies an
// error control frame reaches every subscriber regardless of event_type
// filter, then the stream closes — matching Auth0's Events API.
func TestHub_Handler_ErrorFrameReachesFilteredSubscriberAndCloses(t *testing.T) {
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	// A filtered subscriber that would never match a "user.created" event
	// still receives the error control frame.
	r, cancel := subscribe(t, srv, "?event_type=user.created")
	defer cancel()
	time.Sleep(50 * time.Millisecond)

	require.NoError(t, h.Publish(events.Event{
		Type:    "error",
		Payload: json.RawMessage(`{"type":"error","error":{"code":"cursor_expired","message":"boom"}}`),
	}))

	frame := readOneEvent(t, r, 2*time.Second)
	assert.Contains(t, frame, "event: error")
	assert.Contains(t, frame, `"code":"cursor_expired"`)

	// The stream closes after an error frame: the subscriber drains out
	// of the active set.
	require.Eventually(t, func() bool { return h.ActiveSubscribers() == 0 },
		2*time.Second, 10*time.Millisecond, "stream should close after an error frame")
}

// TestHub_Handler_ErrorFrameIsNotReplayed verifies error frames are never
// stored in the replay buffer: resuming past one replays only the real events.
func TestHub_Handler_ErrorFrameIsNotReplayed(t *testing.T) {
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	require.NoError(t, h.Publish(events.Event{Type: "user.created", ID: "0", Payload: json.RawMessage(`{"type":"user.created","offset":"0"}`)}))
	require.NoError(t, h.Publish(events.Event{Type: "error", Payload: json.RawMessage(`{"type":"error","error":{"code":"timeout","message":"x"}}`)}))
	require.NoError(t, h.Publish(events.Event{Type: "user.created", ID: "1", Payload: json.RawMessage(`{"type":"user.created","offset":"1"}`)}))

	// Resume from offset "0" → only the real event at "1" replays; the
	// error frame between them was never buffered.
	r, cancel := subscribe(t, srv, "?from=0")
	defer cancel()

	frame := readOneEvent(t, r, 2*time.Second)
	assert.Contains(t, frame, "id: 1")
	assert.NotContains(t, frame, "event: error")
}

// TestHub_Handler_OffsetOnlyReachesFilteredSubscriber verifies an offset-only
// progress marker reaches every subscriber regardless of event_type filter
// (it advances the whole stream's cursor), without closing the stream.
func TestHub_Handler_OffsetOnlyReachesFilteredSubscriber(t *testing.T) {
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	// Filtered on user.created — would never match an offset-only marker by
	// type, yet must still receive it.
	r, cancel := subscribe(t, srv, "?event_type=user.created")
	t.Cleanup(cancel)
	time.Sleep(50 * time.Millisecond)

	require.NoError(t, h.Publish(events.Event{
		Type:    "offset-only",
		ID:      "5",
		Payload: json.RawMessage(`{"type":"offset-only","offset":"5"}`),
	}))

	frame := readOneEvent(t, r, 2*time.Second)
	assert.Contains(t, frame, "event: offset-only")
	assert.Contains(t, frame, "id: 5")

	// Unlike an error frame, the stream stays open.
	assert.Equal(t, 1, h.ActiveSubscribers(), "offset-only must not close the stream")
}

// TestHub_Handler_OffsetOnlyIsBufferedAndReplayable verifies a marker's offset
// is a valid resume cursor: resuming from before it replays the marker, and
// resuming from it replays only what follows.
func TestHub_Handler_OffsetOnlyIsBufferedAndReplayable(t *testing.T) {
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	require.NoError(t, h.Publish(events.Event{Type: "user.created", ID: "4", Payload: json.RawMessage(`{"type":"user.created","offset":"4"}`)}))
	require.NoError(t, h.Publish(events.Event{Type: "offset-only", ID: "5", Payload: json.RawMessage(`{"type":"offset-only","offset":"5"}`)}))
	require.NoError(t, h.Publish(events.Event{Type: "user.created", ID: "6", Payload: json.RawMessage(`{"type":"user.created","offset":"6"}`)}))

	// Resume from "4" → the marker at "5" replays (it's buffered).
	r1, cancel1 := subscribe(t, srv, "?from=4")
	t.Cleanup(cancel1)
	assert.Contains(t, readOneEvent(t, r1, 2*time.Second), "event: offset-only")

	// Resume from the marker's own offset "5" → only the event at "6"
	// follows (the marker is a valid cursor, no 410).
	r2, cancel2 := subscribe(t, srv, "?from=5")
	t.Cleanup(cancel2)
	assert.Contains(t, readOneEvent(t, r2, 2*time.Second), "id: 6")
}

func TestHub_Handler_EventTypeFilterSelectsMatchingOnly(t *testing.T) {
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	r, cancel := subscribe(t, srv, "?event_type=user.created")
	defer cancel()
	time.Sleep(50 * time.Millisecond)

	// Publish two events; only user.created should reach the
	// filtered subscriber.
	require.NoError(t, h.Publish(events.Event{
		Type: "user.deleted", ID: "evt-skip",
		Payload: json.RawMessage(`{"type":"user.deleted","id":"evt-skip"}`),
	}))
	require.NoError(t, h.Publish(events.Event{
		Type: "user.created", ID: "evt-keep",
		Payload: json.RawMessage(`{"type":"user.created","id":"evt-keep"}`),
	}))

	frame := readOneEvent(t, r, 2*time.Second)
	assert.Contains(t, frame, "id: evt-keep")
	assert.NotContains(t, frame, "evt-skip")
}

func TestHub_Handler_LastEventIDHeaderReplays(t *testing.T) {
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	for i, id := range []string{"evt-1", "evt-2", "evt-3"} {
		require.NoError(t, h.Publish(events.Event{
			Type: "user.created", ID: id,
			Payload: json.RawMessage(`{"type":"user.created","id":"` + id + `","seq":` + strconv.Itoa(i) + `}`),
		}))
	}

	// Subscribe with Last-Event-ID: evt-1 → should replay evt-2, evt-3.
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	req.Header.Set("Last-Event-ID", "evt-1")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusOK, resp.StatusCode)

	r := bufio.NewReader(resp.Body)
	readOneEvent(t, r, 2*time.Second) // Consume the connect announcement frame.
	f1 := readOneEvent(t, r, 2*time.Second)
	f2 := readOneEvent(t, r, 2*time.Second)
	assert.Contains(t, f1, "id: evt-2")
	assert.Contains(t, f2, "id: evt-3")
}

func TestHub_Handler_FromQueryParamPromotedToHeader(t *testing.T) {
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	for _, id := range []string{"evt-1", "evt-2", "evt-3"} {
		require.NoError(t, h.Publish(events.Event{
			Type: "x.y", ID: id,
			Payload: json.RawMessage(`{"type":"x.y","id":"` + id + `"}`),
		}))
	}

	r, cancel := subscribe(t, srv, "?from=evt-2")
	defer cancel()
	f := readOneEvent(t, r, 2*time.Second)
	assert.Contains(t, f, "id: evt-3")
}

func TestHub_Handler_FromTimestampResolvedToID(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UTC()
	step := 0
	now := func() time.Time {
		ts := base.Add(time.Duration(step) * 10 * time.Second)
		step++
		return ts
	}
	h, err := events.NewHub(10, now)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	// Three events at t=0, t=10s, t=20s.
	for _, id := range []string{"evt-1", "evt-2", "evt-3"} {
		require.NoError(t, h.Publish(events.Event{
			Type: "x.y", ID: id,
			Payload: json.RawMessage(`{"type":"x.y","id":"` + id + `"}`),
		}))
	}

	// From_timestamp at 15s strictly-after-evt-2: ringIndex.idBefore
	// returns evt-2 → library replays everything with ID > evt-2 → evt-3.
	ts := base.Add(15 * time.Second).Format(time.RFC3339)
	r, cancel := subscribe(t, srv, "?from_timestamp="+ts)
	defer cancel()
	f := readOneEvent(t, r, 2*time.Second)
	assert.Contains(t, f, "id: evt-3")
}

func TestHub_Handler_FromTimestampBeforeAllReplaysFromOldest(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UTC()
	step := 0
	now := func() time.Time {
		ts := base.Add(time.Duration(step) * 10 * time.Second)
		step++
		return ts
	}
	h, err := events.NewHub(10, now)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	for _, id := range []string{"evt-1", "evt-2", "evt-3"} {
		require.NoError(t, h.Publish(events.Event{
			Type: "x.y", ID: id,
			Payload: json.RawMessage(`{"type":"x.y","id":"` + id + `"}`),
		}))
	}

	// From_timestamp before everything → adapter injects oldest stored
	// ID (evt-1) → library replays strictly after, i.e. evt-2 + evt-3.
	// The oldest event itself is skipped; see recordingReplayer.OldestID
	// for the rationale.
	old := base.Add(-time.Hour).Format(time.RFC3339)
	r, cancel := subscribe(t, srv, "?from_timestamp="+old)
	defer cancel()
	f1 := readOneEvent(t, r, 2*time.Second)
	f2 := readOneEvent(t, r, 2*time.Second)
	assert.Contains(t, f1, "id: evt-2")
	assert.Contains(t, f2, "id: evt-3")
}

func TestHub_Handler_FromTimestampWithEmptyBufferJoinsLive(t *testing.T) {
	// Empty buffer + from_timestamp predates anything → no replay
	// possible; subscriber just joins the live stream.
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	old := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	r, cancel := subscribe(t, srv, "?from_timestamp="+old)
	defer cancel()

	time.Sleep(50 * time.Millisecond)
	require.NoError(t, h.Publish(events.Event{
		Type: "x.y", ID: "live-1",
		Payload: json.RawMessage(`{"type":"x.y","id":"live-1"}`),
	}))

	f := readOneEvent(t, r, 2*time.Second)
	assert.Contains(t, f, "id: live-1")
}

func TestHub_EmitsKeepAliveComments(t *testing.T) {
	events.SetKeepAliveIntervalForTest(t, 50*time.Millisecond)

	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	r, cancel := subscribe(t, srv, "")
	defer cancel()

	// Read a keep-alive frame (comment-only frame: leading `:`).
	frame := readOneEvent(t, r, 1*time.Second)
	assert.True(t,
		strings.HasPrefix(frame, ":") || strings.Contains(frame, "\n:"),
		"expected a comment line starting with ':', got %q", frame,
	)
}

func TestHub_KeepAlive_FanOutsOncePerSubscriber(t *testing.T) {
	// With two subscribers, a per-Hub keep-alive goroutine publishes
	// once per tick and each subscriber sees that one publish.
	// A per-session goroutine bug would double up: subscriber 1's
	// goroutine publishes to all (including subscriber 2), and vice
	// versa, so each subscriber would see N=subscriber-count
	// keep-alives per tick. Asserting equal counts across subscribers
	// catches the bug regardless of how many ticks happen to fire.
	events.SetKeepAliveIntervalForTest(t, 50*time.Millisecond)

	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	counts := make([]int, 2)
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Go(func() {
			r, cancel := subscribe(t, srv, "")
			defer cancel()
			// Read for 175ms: 3 ticks at 50ms intervals.
			done := time.After(175 * time.Millisecond)
			lines := make(chan string, 32)
			go func() {
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						close(lines)
						return
					}
					lines <- line
				}
			}()
		Loop:
			for {
				select {
				case <-done:
					break Loop
				case line, ok := <-lines:
					if !ok {
						break Loop
					}
					if strings.HasPrefix(line, ":") {
						counts[i]++
					}
				}
			}
		})
	}
	wg.Wait()
	require.Greater(t, counts[0], 0, "subscriber 0 should have seen at least one keep-alive")
	assert.Equal(t, counts[0], counts[1],
		"per-Hub fan-out means every subscriber sees the same number of keep-alives; "+
			"unequal counts would indicate per-session goroutine stacking")
}

func TestHub_Handler_FromTimestampUnparseable_400(t *testing.T) {
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "?from_timestamp=not-a-timestamp")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHub_Handler_MalformedEscapeInFrom_400(t *testing.T) {
	// A `?from` that won't unescape used to vanish from the parsed
	// query, so no Last-Event-ID was synthesised and the 410 gate never
	// ran: the subscriber joined live and silently missed everything
	// between its cursor and now — the exact outcome the 410 prevents.
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "?from=evt-1%2")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "invalid_query")
}

func TestHub_Handler_MalformedEscapeInFromTimestamp_400(t *testing.T) {
	// Same silent join-live as `?from`, plus the invalid_from_timestamp
	// guard is skipped: the pair is gone before there is a value to
	// validate.
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "?from_timestamp=2020-01-01T00:00:00Z%2")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "invalid_query")
}

func TestHub_Handler_MalformedEscapeInEventType_400(t *testing.T) {
	// A `?event_type` that won't unescape left onSession with no
	// requested types, so the filtered subscription silently joined
	// broadcastTopic and became a firehose.
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "?event_type=user.created%2")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "invalid_query")
}

func TestHub_Handler_UnencodedSemicolonInFrom_400(t *testing.T) {
	// Go refuses `;` as a query separator (it stopped honouring the
	// legacy `a=1;b=2` form in 1.17), so an unencoded semicolon inside a
	// cursor value fails the same parse a bad escape does. That is a
	// rejection of a character RFC 3986 permits in a query, so pin it:
	// it is a deliberate consequence of parsing strictly, not an
	// oversight. Callers percent-encode it (`%3B`) — which url.Values
	// and every Go SDK path already do. The old code dropped the pair
	// and joined live here too, so 400 replaces a silent miss rather
	// than a working resume.
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "?from=a;b")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "invalid_query")
}

// TestHub_Handler_RejectedQueryShapes covers the query shapes that used
// to be accepted and then quietly do the wrong thing. Each one parses
// cleanly, so the invalid_query gate above lets it through; what makes
// them wrong is what the handler then does with the value.
//
//   - An empty `?from` / `?from_timestamp` fails the `!= ""` guard, so
//     no Last-Event-ID is set, the 410 gate is skipped, and the
//     subscriber joins live — the same missed window a bad escape
//     caused. A client templating `?from=${cursor}` with an unset
//     variable hits exactly this.
//   - An empty `?event_type` leaves onSession subscribing to the ""
//     topic. Nothing ever publishes there (Publish targets
//     broadcastTopic and evt.Type, and the push schema requires a
//     non-empty type), so the stream is 200, connected, and incapable
//     of ever delivering an event.
//   - A repeated `?from` / `?from_timestamp` silently resolves to the
//     first value, so a caller can be resumed from a cursor it did not
//     ask for and gets no 410 saying so.
//
// POST /admin0/events/expire already refuses an empty and a repeated
// `before` for the same reason; see internal/admin0/events.go.
func TestHub_Handler_RejectedQueryShapes(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		errorCode string
	}{
		{"empty from", "?from=", "invalid_from"},
		{"empty from_timestamp", "?from_timestamp=", "invalid_from_timestamp"},
		{"empty event_type", "?event_type=", "invalid_event_type"},
		{"one empty among several event_types", "?event_type=user.created&event_type=", "invalid_event_type"},
		{"repeated from", "?from=3&from=1", "invalid_query"},
		{"repeated from_timestamp", "?from_timestamp=2020-01-01T00:00:00Z&from_timestamp=2021-01-01T00:00:00Z", "invalid_query"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h, err := events.NewHub(10, nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
			srv := httptest.NewServer(h.Handler())
			t.Cleanup(srv.Close)

			resp, err := http.Get(srv.URL + test.query)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()

			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
			assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
			body, _ := io.ReadAll(resp.Body)
			assert.Contains(t, string(body), test.errorCode)
		})
	}
}

func TestHub_Handler_RepeatedEventTypeStillFilters(t *testing.T) {
	// Guard against over-rejecting: repeating `?event_type` is how a
	// caller asks for several types at once, so unlike a repeated
	// `?from` it must keep working. Only an empty value is refused.
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "?event_type=user.created&event_type=user.deleted")
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusOK, resp.StatusCode)

	time.Sleep(50 * time.Millisecond)
	require.NoError(t, h.Publish(events.Event{
		Type:    "user.deleted",
		ID:      "d1",
		Payload: json.RawMessage(`{"type":"user.deleted","id":"d1"}`),
	}))

	got := make(chan string, 4)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			if line := scanner.Text(); strings.HasPrefix(line, "id:") {
				got <- line
			}
		}
	}()
	select {
	case line := <-got:
		assert.Contains(t, line, "d1")
	case <-time.After(2 * time.Second):
		t.Fatal("second event_type filter never delivered its event")
	}
}

func TestHub_Handler_MalformedEscapeRejectsWholeQuery_400(t *testing.T) {
	// Url.ParseQuery keeps the pairs it could parse and reports an error
	// for the one it couldn't. Answering 200 on the strength of the
	// survivors would serve a request the caller never made, so one bad
	// pair rejects the request even when the rest parsed cleanly.
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "?event_type=user.created&from=evt-1%2")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "invalid_query")
}

func TestHub_Handler_MultipleSubscribersEachReceiveOnce(t *testing.T) {
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	var wg sync.WaitGroup
	received := make([]string, 2)

	openAndRead := func(idx int, query string) {
		r, cancel := subscribe(t, srv, query)
		defer cancel()
		received[idx] = readOneEvent(t, r, 2*time.Second)
	}

	wg.Go(func() { openAndRead(0, "") })
	wg.Go(func() { openAndRead(1, "?event_type=user.created") })
	time.Sleep(100 * time.Millisecond)

	require.NoError(t, h.Publish(events.Event{
		Type: "user.created", ID: "evt-x",
		Payload: json.RawMessage(`{"type":"user.created","id":"evt-x"}`),
	}))

	wg.Wait()
	for i, frame := range received {
		assert.Contains(t, frame, "id: evt-x", "subscriber %d missed the event", i)
	}
}

func TestHub_Reset_RebuildsHub(t *testing.T) {
	// Regression for the blocker: /admin0/reset must NOT permanently
	// destroy the hub. After Reset the hub should accept fresh
	// subscribers and publishes again.
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	// Open a subscriber, prove it works.
	r1, cancel1 := subscribe(t, srv, "")
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, h.Publish(events.Event{
		Type: "x.y", ID: "before-reset",
		Payload: json.RawMessage(`{"type":"x.y","id":"before-reset"}`),
	}))
	frame := readOneEvent(t, r1, 2*time.Second)
	assert.Contains(t, frame, "id: before-reset")
	cancel1()

	// Reset and verify the hub is still functional.
	require.NoError(t, h.Reset(t.Context()))

	r2, cancel2 := subscribe(t, srv, "")
	defer cancel2()
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, h.Publish(events.Event{
		Type: "x.y", ID: "after-reset",
		Payload: json.RawMessage(`{"type":"x.y","id":"after-reset"}`),
	}))
	frame = readOneEvent(t, r2, 2*time.Second)
	assert.Contains(t, frame, "id: after-reset")
}

func TestHub_Reset_DoesNotLeakErrorTextToWire(t *testing.T) {
	// Regression: shutting down the sse.Server while subscribers are
	// connected writes "go-sse.server: provider is closed" into the
	// SSE wire body. Reset must drain via context cancellation so
	// the subscriber sees a clean close instead.
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	r, cancel := subscribe(t, srv, "")
	defer cancel()
	time.Sleep(50 * time.Millisecond)

	// Drain ANY buffered content from the connection up to this point.
	doneInit := make(chan struct{})
	read := make(chan string, 16)
	go func() {
		defer close(doneInit)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			read <- line
		}
	}()

	require.NoError(t, h.Reset(t.Context()))

	// Collect for 150ms then inspect.
	collected := []string{}
	timeout := time.After(150 * time.Millisecond)
Loop:
	for {
		select {
		case line := <-read:
			collected = append(collected, line)
		case <-timeout:
			break Loop
		}
	}
	joined := strings.Join(collected, "")
	assert.NotContains(t, joined, "provider is closed",
		"library error string must not leak into the SSE wire body")
}

func TestHub_Handler_AgedOutLastEventID_Returns410(t *testing.T) {
	// Cap=2 so we can force the buffer to evict.
	h, err := events.NewHub(2, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	for _, id := range []string{"old", "newer", "newest"} {
		require.NoError(t, h.Publish(events.Event{
			Type: "x.y", ID: id,
			Payload: json.RawMessage(`{"type":"x.y","id":"` + id + `"}`),
		}))
	}
	// "old" has been evicted; "newer" and "newest" remain.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	req.Header.Set("Last-Event-ID", "old")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	assert.Equal(t, http.StatusGone, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "event_aged_out")
}

func TestHub_Handler_FromTimestampUnencodedPlus_Accepted(t *testing.T) {
	// Regression: a client that pastes an RFC 3339 timestamp without
	// URL-encoding the `+` in `+00:00` would previously hit 400
	// because Go's URL form decoder turns `+` into space.
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	// Use a base URL string with the raw `+`; the test sends it
	// verbatim, simulating a paste-and-go client.
	rawTS := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	// Force the +00:00 form by re-formatting; UTC's "Z" wouldn't
	// exercise the path.
	rawTS = strings.Replace(rawTS, "Z", "+00:00", 1)

	r, cancel := subscribe(t, srv, "?from_timestamp="+rawTS)
	defer cancel()
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, h.Publish(events.Event{
		Type: "x.y", ID: "after",
		Payload: json.RawMessage(`{"type":"x.y","id":"after"}`),
	}))
	frame := readOneEvent(t, r, 2*time.Second)
	assert.Contains(t, frame, "id: after")
}

func TestHub_Handler_FromTimestamp400UsesMgmtEnvelope(t *testing.T) {
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "?from_timestamp=not-a-timestamp")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "invalid_from_timestamp")
}

func TestHub_KeepAlive_ReachesFilteredSubscribers(t *testing.T) {
	// Regression: filtered subscribers used to be excluded from
	// keep-alives because they subscribed only to their event-type
	// topics. They should also receive heartbeats so reverse-proxy
	// idle timeouts don't tear them down.
	events.SetKeepAliveIntervalForTest(t, 50*time.Millisecond)

	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	r, cancel := subscribe(t, srv, "?event_type=user.created")
	defer cancel()

	// First frame within 150ms should be a keep-alive (no matching
	// event published).
	frame := readOneEvent(t, r, 300*time.Millisecond)
	assert.True(t, strings.HasPrefix(frame, ":") || strings.Contains(frame, "\n:"),
		"filtered subscriber must receive keep-alive comments; got %q", frame)
}

func TestNewHub_BufferSizeOneIsAccepted(t *testing.T) {
	// One is a valid buffer size. It used to be widened to 2 because
	// sse.FiniteReplayer refused anything smaller; the hub owns its
	// buffer now, so the size is taken at face value — see
	// TestHub_BufferSizeOne_RetainsExactlyOneEvent for the behaviour.
	h, err := events.NewHub(1, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
}

// TestHub_ConcurrentPushAndReset is the regression for the
// third-review finding: even after Publish was made to hold mu.RLock
// across server.Publish, the Reset path called server.Shutdown on
// the old server lock-free, so a concurrent publisher whose RLock
// had just been released (or hadn't acquired yet) could land on a
// shut-down server and get ErrProviderClosed.
//
// We hammer Publish on one goroutine and Reset on another for ~500ms
// and assert zero publish errors.
func TestHub_ConcurrentPushAndReset(t *testing.T) {
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	stop := make(chan struct{})
	var wg sync.WaitGroup
	var publishErrs atomic.Int64
	var publishOK atomic.Int64
	var resets atomic.Int64

	// 4 publisher goroutines.
	for i := range 4 {
		wg.Go(func() {
			for n := 0; ; n++ {
				select {
				case <-stop:
					return
				default:
				}
				id := fmt.Sprintf("evt_%016x", n*10+i)
				err := h.Publish(events.Event{
					Type: "x.y", ID: id,
					Payload: json.RawMessage(`{"type":"x.y","id":"` + id + `"}`),
				})
				if err != nil {
					publishErrs.Add(1)
				} else {
					publishOK.Add(1)
				}
			}
		})
	}

	// 1 reset goroutine.
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			if err := h.Reset(context.Background()); err == nil {
				resets.Add(1)
			}
			time.Sleep(5 * time.Millisecond)
		}
	})

	time.Sleep(500 * time.Millisecond)
	close(stop)
	wg.Wait()

	assert.Equal(t, int64(0), publishErrs.Load(),
		"no Publish should fail; the swap-before-shutdown ordering must guarantee publishers always see a live server. ok=%d resets=%d",
		publishOK.Load(), resets.Load())
	assert.Greater(t, publishOK.Load(), int64(0))
	assert.Greater(t, resets.Load(), int64(0))
}

// publishSeq publishes one event per id, in order, so expiry tests have
// a buffer to age out. The payload carries the position as "seq".
func publishSeq(t *testing.T, h *events.Hub, ids ...string) {
	t.Helper()
	for i, id := range ids {
		require.NoError(t, h.Publish(events.Event{
			Type: "user.created", ID: id,
			Payload: json.RawMessage(`{"type":"user.created","id":"` + id + `","seq":` + strconv.Itoa(i) + `}`),
		}))
	}
}

// resumeStatus subscribes with Last-Event-ID: id and reports the status
// code plus body, without consuming the stream.
func resumeStatus(t *testing.T, srv *httptest.Server, id string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	req.Header.Set("Last-Event-ID", id)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}
	// Close the stream before returning: leaving it open would park a
	// live subscriber on the hub for the rest of the test, which any
	// later subscriber-count or keep-alive assertion would then see.
	_ = resp.Body.Close()
	return resp.StatusCode, ""
}

func TestHub_Expire_AllAgesOutEveryCursor(t *testing.T) {
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	publishSeq(t, h, "evt-1", "evt-2", "evt-3")
	assert.Equal(t, 3, h.ExpireAll())

	for _, id := range []string{"evt-1", "evt-2", "evt-3"} {
		status, body := resumeStatus(t, srv, id)
		assert.Equalf(t, http.StatusGone, status, "resume from %q should be aged out", id)
		assert.Contains(t, body, "event_aged_out")
	}
}

func TestHub_Expire_BeforeKeepsCursorAndNewer(t *testing.T) {
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	publishSeq(t, h, "evt-1", "evt-2", "evt-3")
	dropped, found := h.ExpireBefore("evt-2")
	assert.Equal(t, 1, dropped)
	assert.True(t, found)

	status, body := resumeStatus(t, srv, "evt-1")
	assert.Equal(t, http.StatusGone, status)
	assert.Contains(t, body, "event_aged_out")

	// The boundary cursor survives, so resuming from it still replays
	// what came after.
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	req.Header.Set("Last-Event-ID", "evt-2")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusOK, resp.StatusCode)
	r := bufio.NewReader(resp.Body)
	readOneEvent(t, r, 2*time.Second) // Consume the connect announcement frame.
	assert.Contains(t, readOneEvent(t, r, 2*time.Second), "id: evt-3")
}

func TestHub_Expire_UnknownCursorIsNoOp(t *testing.T) {
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	publishSeq(t, h, "evt-1", "evt-2")
	dropped, found := h.ExpireBefore("never-seen")
	assert.Equal(t, 0, dropped)
	assert.False(t, found, "the handler needs this to answer 404 rather than a 0-drop success")

	status, _ := resumeStatus(t, srv, "evt-1")
	assert.Equal(t, http.StatusOK, status, "an unknown cursor must not age out the buffer")
}

// Expiry is a control-plane operation over the resume index only: it
// must not disturb subscribers that are already streaming.
func TestHub_Expire_LeavesConnectedSubscribersStreaming(t *testing.T) {
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	r, cancel := subscribe(t, srv, "")
	defer cancel()
	// Give the subscription a moment to register with Joe: subscribe()
	// returns once the :connected frame lands, which the handler writes
	// before it delegates to the server.
	time.Sleep(50 * time.Millisecond)

	publishSeq(t, h, "evt-1")
	assert.Contains(t, readOneEvent(t, r, 2*time.Second), "id: evt-1")

	require.Equal(t, 1, h.ExpireAll())

	publishSeq(t, h, "evt-2")
	assert.Contains(t, readOneEvent(t, r, 2*time.Second), "id: evt-2",
		"the live stream should be untouched by an expiry")
}

// ?from_timestamp resolves through the same index, so an expired
// buffer has nothing to resolve against and the subscriber joins live
// rather than 410-ing (it never named a cursor of its own).
func TestHub_Expire_FromTimestampJoinsLive(t *testing.T) {
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	publishSeq(t, h, "evt-1", "evt-2")
	require.Equal(t, 2, h.ExpireAll())

	r, cancel := subscribe(t, srv, "?from_timestamp=2020-01-01T00:00:00Z")
	defer cancel()
	time.Sleep(50 * time.Millisecond) // Let the subscription register; see above.

	publishSeq(t, h, "evt-3")
	assert.Contains(t, readOneEvent(t, r, 2*time.Second), "id: evt-3")
}

func TestHub_Expire_DisabledBufferReportsZero(t *testing.T) {
	h, err := events.NewHub(0, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	assert.Equal(t, 0, h.ExpireAll())

	dropped, found := h.ExpireBefore("evt-1")
	assert.Equal(t, 0, dropped)
	assert.False(t, found, "with no buffer at all, no cursor is in it")

	dropped, found = h.ExpireBefore("")
	assert.Equal(t, 0, dropped, "an empty cursor is never expire-everything")
	assert.False(t, found)
}

// The delicate part of the expiry feature is lock scope: neither Replay
// nor Hub.expire may hold a lock across a subscriber write, or a
// consumer that stops reading parks an expiry and Go's RWMutex then
// queues every later reader behind it. That reasoning is invisible to a
// single-threaded test, so drive expiry against publishes, subscribes
// and resumes at once and let -race and the deadlock detector judge.
// The assertions are liveness ones: nothing wedges, and every publish
// still finds a live server.
func TestHub_ConcurrentExpirePublishAndSubscribe(t *testing.T) {
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	var publishErrs, publishOK, expires, subscribes, subErrs atomic.Int64

	for i := range 3 {
		wg.Go(func() {
			for n := 0; ; n++ {
				select {
				case <-stop:
					return
				default:
				}
				id := fmt.Sprintf("evt_%016x", n*10+i)
				if err := h.Publish(events.Event{
					Type: "x.y", ID: id,
					Payload: json.RawMessage(`{"type":"x.y","id":"` + id + `"}`),
				}); err != nil {
					publishErrs.Add(1)
				} else {
					publishOK.Add(1)
				}
				time.Sleep(time.Millisecond)
			}
		})
	}

	// Expirers alternate between the whole buffer and a partial trim.
	for i := range 2 {
		wg.Go(func() {
			for n := 0; ; n++ {
				select {
				case <-stop:
					return
				default:
				}
				if (n+i)%2 == 0 {
					h.ExpireAll()
				} else {
					_, _ = h.ExpireBefore(fmt.Sprintf("evt_%016x", n))
				}
				expires.Add(1)
				time.Sleep(time.Millisecond)
			}
		})
	}

	// Subscribers connect and disconnect, some presenting a resume
	// cursor so they exercise the Replay path against a live expirer.
	wg.Go(func() {
		for n := 0; ; n++ {
			select {
			case <-stop:
				return
			default:
			}
			query := ""
			if n%2 == 0 {
				query = fmt.Sprintf("?from=evt_%016x", n)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+query, nil)
			if reqErr != nil {
				cancel()
				continue
			}
			resp, doErr := http.DefaultClient.Do(req) //nolint:bodyclose // Closed below; the 410 path returns a short body.
			if doErr != nil {
				subErrs.Add(1)
			} else {
				// Read one frame, then drop the connection — the point is
				// churning subscribe/unsubscribe under a live expirer, not
				// consuming the stream.
				_, _ = io.CopyN(io.Discard, resp.Body, 32)
				_ = resp.Body.Close()
				subscribes.Add(1)
			}
			cancel()
			// Throttle: an unbounded dial loop can exhaust ephemeral ports
			// on a busy runner, and every failed dial is a subscribe that
			// never raced an expiry.
			time.Sleep(2 * time.Millisecond)
		}
	})

	time.Sleep(500 * time.Millisecond)
	close(stop)
	wg.Wait()

	assert.Equal(t, int64(0), publishErrs.Load(),
		"no Publish should fail while expiry runs; ok=%d expires=%d subs=%d",
		publishOK.Load(), expires.Load(), subscribes.Load())
	assert.Greater(t, publishOK.Load(), int64(0))
	assert.Greater(t, expires.Load(), int64(0))
	// A floor, not "at least one": a run where nearly every dial failed
	// would otherwise report green having raced almost nothing. Kept low,
	// and counting failures rather than forbidding them, because a
	// contended runner under -race can legitimately drop a connection —
	// this test exists to catch a wedge, not to measure throughput.
	total := subscribes.Load() + subErrs.Load()
	assert.Greater(t, total, int64(10),
		"too few subscribe attempts completed to have exercised the race")
	assert.Greater(t, subscribes.Load(), int64(0),
		"every subscribe failed (errs=%d); the race was never exercised", subErrs.Load())
}

// Hub.expire documents that it refuses a closed hub the way Publish
// does. The two conditions share a line, so without this a future edit
// dropping the closed half would leave the whole suite green while
// expiry ran against a replayer Shutdown had torn down.
func TestHub_Expire_ClosedHubReportsZero(t *testing.T) {
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	publishSeq(t, h, "evt-1", "evt-2")
	require.NoError(t, h.Shutdown(context.Background()))

	assert.Equal(t, 0, h.ExpireAll())

	dropped, found := h.ExpireBefore("evt-2")
	assert.Equal(t, 0, dropped)
	assert.False(t, found, "a torn-down replayer holds nothing, cursors included")
}

// EVENTS_REPLAY_BUFFER=1 means one event retained. It used to be
// silently widened to 2 — a floor sse.FiniteReplayer imposed, which the
// hub no longer uses — so a resume from the second-oldest cursor
// succeeded where the operator expected 410.
func TestHub_BufferSizeOne_RetainsExactlyOneEvent(t *testing.T) {
	h, err := events.NewHub(1, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	publishSeq(t, h, "evt-1", "evt-2")

	status, body := resumeStatus(t, srv, "evt-1")
	assert.Equal(t, http.StatusGone, status, "only one event is retained, so evt-1 has aged out")
	assert.Contains(t, body, "event_aged_out")

	status, _ = resumeStatus(t, srv, "evt-2")
	assert.Equal(t, http.StatusOK, status)
}

// The partial-expire case is described in the OpenAPI fragment, the
// README, the cookbook and the SDK godoc, and it runs through the
// IDBefore-fails → OldestID-fallback branch in the handler. Without
// this, deleting that fallback would leave the suite green while every
// ?from_timestamp subscriber after a partial expire silently joined
// live instead of replaying the surviving window.
func TestHub_Expire_BeforeThenFromTimestampResumesSurvivingWindow(t *testing.T) {
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	publishSeq(t, h, "evt-1", "evt-2", "evt-3")
	dropped, found := h.ExpireBefore("evt-2")
	require.Equal(t, 1, dropped)
	require.True(t, found)

	// Every surviving event postdates this instant, so IDBefore finds
	// nothing and the handler falls back to the oldest survivor.
	r, cancel := subscribe(t, srv, "?from_timestamp=2020-01-01T00:00:00Z")
	defer cancel()

	// The resolved cursor is evt-2, and replay starts strictly after it,
	// so the surviving window delivers evt-3 — the documented trade-off.
	assert.Contains(t, readOneEvent(t, r, 2*time.Second), "id: evt-3")
}
