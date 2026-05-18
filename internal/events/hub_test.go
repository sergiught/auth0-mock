package events_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
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
