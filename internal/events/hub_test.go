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
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+query, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "text/event-stream")
	return bufio.NewReader(resp.Body), cancel
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	req.Header.Set("Last-Event-ID", "evt-1")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusOK, resp.StatusCode)

	r := bufio.NewReader(resp.Body)
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
	wg.Add(2)
	for i := range 2 {
		go func() {
			defer wg.Done()
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
		}()
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

func TestHub_Handler_MultipleSubscribersEachReceiveOnce(t *testing.T) {
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	var wg sync.WaitGroup
	wg.Add(2)
	received := make([]string, 2)

	openAndRead := func(idx int, query string) {
		defer wg.Done()
		r, cancel := subscribe(t, srv, query)
		defer cancel()
		received[idx] = readOneEvent(t, r, 2*time.Second)
	}

	go openAndRead(0, "")
	go openAndRead(1, "?event_type=user.created")
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
	require.NoError(t, h.Reset(context.Background()))

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

	require.NoError(t, h.Reset(context.Background()))

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

func TestNewHub_BufferSizeOneClampedToTwo(t *testing.T) {
	// Library requires count >= 2; we used to crash at startup
	// instead of clamping.
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
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
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
		}()
	}

	// 1 reset goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
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
	}()

	time.Sleep(500 * time.Millisecond)
	close(stop)
	wg.Wait()

	assert.Equal(t, int64(0), publishErrs.Load(),
		"no Publish should fail; the swap-before-shutdown ordering must guarantee publishers always see a live server. ok=%d resets=%d",
		publishOK.Load(), resets.Load())
	assert.Greater(t, publishOK.Load(), int64(0))
	assert.Greater(t, resets.Load(), int64(0))
}
