package auth0mocktest

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sergiught/auth0-mock/pkg/auth0mock"
)

// SSEEvent is a parsed SSE frame: the `id:`, `event:`, and `data:`
// fields, glued together for one logical event. Comment frames
// (`:keep-alive`) are filtered out by SubscribeEvents — callers see
// only real events.
type SSEEvent struct {
	ID   string
	Type string
	Data json.RawMessage
}

// SSEStream is an open SSE subscription. Read events with NextEvent
// (which blocks with a timeout). The connection is closed
// automatically on test cleanup; callers don't need to call Close
// directly unless they want to tear it down earlier (e.g. to test the
// disconnect behaviour).
type SSEStream struct {
	events  chan SSEEvent
	errc    chan error
	cancel  context.CancelFunc
	stopped chan struct{}
}

// SubscribeEvents opens an SSE connection to /api/v2/events on the
// mock with the given bearer and optional query string ("event_type=
// user.created", "from=evt_xxx", etc. — pass the value without the
// leading `?`). The stream filters out keep-alive comment frames and
// emits one SSEEvent per server frame.
//
// The subscription is canceled on t.Cleanup. On t.Failed, the cleanup
// is still respected.
//
// Calls t.Fatalf if the subscription fails to open (non-200 status,
// transport error, or missing text/event-stream content type).
func SubscribeEvents(t testing.TB, c *auth0mock.Client, bearer, query string) *SSEStream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	url := c.BaseURL() + "/api/v2/events"
	if query != "" {
		url += "?" + query
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		t.Fatalf("auth0mocktest: subscribe: build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req) //nolint:bodyclose // SSE body stays open; closed by the cleanup func.
	if err != nil {
		cancel()
		t.Fatalf("auth0mocktest: subscribe: do: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		cancel()
		t.Fatalf("auth0mocktest: subscribe: status %d (want 200)", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		_ = resp.Body.Close()
		cancel()
		t.Fatalf("auth0mocktest: subscribe: Content-Type %q (want text/event-stream)", ct)
	}

	s := &SSEStream{
		events:  make(chan SSEEvent, 16),
		errc:    make(chan error, 1),
		cancel:  cancel,
		stopped: make(chan struct{}),
	}
	go s.read(resp)
	t.Cleanup(func() {
		s.cancel()
		<-s.stopped
		_ = resp.Body.Close()
	})
	return s
}

// NextEvent blocks until the next event arrives, the timeout fires,
// or the connection errors. Calls t.Fatalf with context on timeout or
// error. Returns the parsed event on success.
func (s *SSEStream) NextEvent(t testing.TB, timeout time.Duration) SSEEvent {
	t.Helper()
	select {
	case e := <-s.events:
		return e
	case err := <-s.errc:
		t.Fatalf("auth0mocktest: NextEvent: stream error: %v", err)
		return SSEEvent{}
	case <-time.After(timeout):
		t.Fatalf("auth0mocktest: NextEvent: timeout after %s", timeout)
		return SSEEvent{}
	}
}

// Close ends the subscription early. Idempotent; safe to call from
// anywhere. The t.Cleanup registered by SubscribeEvents calls this
// automatically, so most tests don't need to.
func (s *SSEStream) Close() {
	s.cancel()
}

func (s *SSEStream) read(resp *http.Response) {
	defer close(s.stopped)
	r := bufio.NewReader(resp.Body)
	var cur SSEEvent
	hasContent := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				select {
				case s.errc <- err:
				default:
				}
			}
			return
		}
		// Strip trailing \n (and optional \r).
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			// End of frame.
			if hasContent {
				select {
				case s.events <- cur:
				case <-s.stopped:
					return
				}
			}
			cur = SSEEvent{}
			hasContent = false
		case strings.HasPrefix(line, ":"):
			// Comment frame (keep-alive); ignore.
		case strings.HasPrefix(line, "id:"):
			cur.ID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
			hasContent = true
		case strings.HasPrefix(line, "event:"):
			cur.Type = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			hasContent = true
		case strings.HasPrefix(line, "data:"):
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if len(cur.Data) > 0 {
				// Multi-line data: per the SSE spec, fields are
				// joined with "\n".
				cur.Data = append(cur.Data, '\n')
			}
			cur.Data = append(cur.Data, []byte(data)...)
			hasContent = true
		default:
			// Other SSE fields (retry:, etc.); ignore for now.
		}
	}
}

// WaitForActiveSubscribers polls GET /admin0/events/subscribers until
// the active count equals want, then returns. Calls t.Fatalf if it
// doesn't settle within timeout.
//
// Use it to anchor on the SSE connection lifecycle rather than sleeping
// a fixed guess: WaitForActiveSubscribers(t, c, 1, …) after subscribing
// blocks until the subscription has registered server-side, and
// WaitForActiveSubscribers(t, c, 0, …) after closing a stream blocks
// until the hub has observed the disconnect (active is
// eventually-consistent, so a bare read can race the close).
func WaitForActiveSubscribers(t testing.TB, c *auth0mock.Client, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		sc, err := c.Events.Subscribers(context.Background())
		if err != nil {
			t.Fatalf("auth0mocktest: WaitForActiveSubscribers: %v", err)
			return
		}
		if sc.Active == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("auth0mocktest: WaitForActiveSubscribers: active = %d, want %d after %s", sc.Active, want, timeout)
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// MustPush wraps Client.Events.Push with t.Fatalf on error. Pass an
// auth0mock.Event-shaped payload (the Auth0 event-stream envelope) as
// a JSON string for readability:
//
//	auth0mocktest.MustPush(t, c, `{
//	  "type":"user.created","offset":"0",
//	  "event":{"specversion":"1.0", ...}
//	}`)
func MustPush(t testing.TB, c *auth0mock.Client, payload string) {
	t.Helper()
	if err := c.Events.Push(context.Background(), json.RawMessage(payload)); err != nil {
		t.Fatalf("auth0mocktest: events: push: %v", err)
	}
}
