package events_test

import (
	"context"
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
