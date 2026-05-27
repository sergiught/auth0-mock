package events_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/events"
)

// requireActiveEventually polls h.ActiveSubscribers until it equals
// want or the deadline elapses. A subscriber is removed from the active
// set only when the server's read loop observes the closed connection,
// so the count after a client disconnect is eventually-consistent — a
// straight assert right after cancel() would flake.
func requireActiveEventually(t *testing.T, h *events.Hub, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h.ActiveSubscribers() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.Equal(t, want, h.ActiveSubscribers())
}

func TestHub_SubscriberCounts_TrackConnectAndDisconnect(t *testing.T) {
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	require.Equal(t, 0, h.ActiveSubscribers())
	require.Equal(t, 0, h.TotalSubscribers())

	_, cancel1 := subscribe(t, srv, "")
	requireActiveEventually(t, h, 1)
	assert.Equal(t, 1, h.TotalSubscribers())

	_, cancel2 := subscribe(t, srv, "")
	requireActiveEventually(t, h, 2)
	assert.Equal(t, 2, h.TotalSubscribers())

	cancel1()
	cancel2()
	requireActiveEventually(t, h, 0)
	assert.Equal(t, 2, h.TotalSubscribers(), "total is monotonic within a reset window")
}

func TestHub_Reset_DoesNotEvictNewSubscriber(t *testing.T) {
	// A subscriber connecting during the reset drain must survive in the
	// active set. The hazard this guards: Reset must not recycle the
	// subscriber id allocator, or a still-draining subscriber's deferred
	// cleanup (which deletes by its captured id) could evict a post-Reset
	// subscriber that reused the id. The eviction is a narrow race the
	// draining cleanup almost always wins, so this exercises the
	// concurrent reset-vs-subscribe path (most useful under -race) rather
	// than forcing the eviction deterministically.
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	for i := range 30 {
		_, cancelOld := subscribe(t, srv, "")
		requireActiveEventually(t, h, 1)

		// Race a fresh subscription against the reset drain: the new
		// connection registers around the moment Reset cancels the old
		// one, so the old subscriber's id (which Reset used to recycle)
		// can land on the newcomer.
		newCtx, cancelNew := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			req, _ := http.NewRequestWithContext(newCtx, http.MethodGet, srv.URL, nil)
			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				t.Cleanup(func() { _ = resp.Body.Close() })
			}
		}()
		require.NoError(t, h.Reset(context.Background()))
		<-done

		// The new connection is still open, so once everything settles
		// the active set must contain exactly it — never 0.
		requireActiveEventually(t, h, 1)
		time.Sleep(5 * time.Millisecond)
		require.Equal(t, 1, h.ActiveSubscribers(),
			"iteration %d: live subscriber evicted by a draining subscriber's stale cleanup", i)

		cancelOld()
		cancelNew()
		requireActiveEventually(t, h, 0)
	}
}

func TestHub_Reset_ZeroesTotalSubscribers(t *testing.T) {
	h, err := events.NewHub(10, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	_, cancel := subscribe(t, srv, "")
	requireActiveEventually(t, h, 1)
	require.Equal(t, 1, h.TotalSubscribers())
	cancel()

	require.NoError(t, h.Reset(context.Background()))
	requireActiveEventually(t, h, 0)
	assert.Equal(t, 0, h.TotalSubscribers(), "reset starts a fresh counting window")

	_, cancel2 := subscribe(t, srv, "")
	defer cancel2()
	requireActiveEventually(t, h, 1)
	assert.Equal(t, 1, h.TotalSubscribers())
}
