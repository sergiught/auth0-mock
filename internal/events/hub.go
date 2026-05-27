package events

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tmaxmax/go-sse"
)

// broadcastTopic is the dedicated fan-out topic for filterless
// subscribers. Publishers send every event to broadcastTopic AND
// evt.Type so filterless subscribers (subscribed to broadcastTopic)
// see everything while filtered subscribers (subscribed to specific
// event_types) see only matching events.
const broadcastTopic = "__broadcast__"

// keepAliveTopic is a separate fan-out topic used ONLY for keep-alive
// comments. Every subscriber — filtered or not — subscribes to it so
// that filtered subscribers behind idle-timeout proxies still receive
// heartbeats. Publishers of real events never target this topic, so
// keep-alives don't interfere with event filtering.
const keepAliveTopic = "__keep_alive__"

// DefaultKeepAliveInterval is the cadence at which a `:keep-alive`
// comment is broadcast to every connected subscriber. 15s matches
// what most SSE deployments use; the library doesn't auto-emit.
const DefaultKeepAliveInterval = 15 * time.Second

// keepAliveIntervalNS is the live cadence (in nanoseconds) used by
// runKeepAlive when it constructs its ticker. Atomic so tests that
// run in parallel can't race on the override.
var keepAliveIntervalNS atomic.Int64

func init() {
	keepAliveIntervalNS.Store(int64(DefaultKeepAliveInterval))
}

// keepAliveInterval returns the current cadence.
func keepAliveInterval() time.Duration {
	return time.Duration(keepAliveIntervalNS.Load())
}

// SetKeepAliveIntervalForTest changes the keep-alive cadence for the
// duration of a single test. Registers t.Cleanup to restore the
// original value, so tests can't accidentally bleed configuration
// between cases. Intended for use only from _test.go files.
//
// The new cadence only affects Hub instances constructed AFTER the
// call: runKeepAlive captures the duration in its time.Ticker at hub
// startup, so changing it later doesn't retro-actively shorten an
// already-running ticker. Build the hub inside the test (or right
// after SetKeepAliveIntervalForTest) to apply the override.
func SetKeepAliveIntervalForTest(t interface{ Cleanup(func()) }, d time.Duration) {
	prev := keepAliveIntervalNS.Swap(int64(d))
	t.Cleanup(func() { keepAliveIntervalNS.Store(prev) })
}

// Hub is the SSE fan-out the mock owns. One Hub per process; the
// HTTP handler at GET /events is hub.Handler(), and POST /admin0/events
// pushes via hub.Publish. Hub is safe for concurrent use; every
// underlying primitive (sse.Server, sse.Joe, recordingReplayer) is.
//
// Lifecycle:
//   - NewHub starts a keep-alive goroutine.
//   - Reset drains current subscribers and rebuilds the underlying
//     server + replay buffer, so /admin0/reset between tests is
//     non-destructive to the hub itself.
//   - Shutdown drains every subscriber permanently and stops the
//     keep-alive goroutine; intended for process shutdown.
type Hub struct {
	bufferSize int
	now        func() time.Time

	// Mu protects server / replayer swap. Read-locked by Publish and
	// Handler; write-locked by Reset and Shutdown.
	mu       sync.RWMutex
	server   *sse.Server
	replayer *recordingReplayer // Nil when buffer is disabled.
	closed   bool

	// ActiveMu protects active subscriber cancels. Reset/Shutdown
	// iterate this list to drain in-flight subscribers via context
	// cancellation, which lets sse.Joe's subscribe loop unwind
	// cleanly (no "provider is closed" error string in the wire body).
	activeMu sync.Mutex
	active   map[int]context.CancelFunc
	nextSub  int

	// LifecycleMu serialises Reset / Shutdown so two concurrent
	// callers don't both try to drain the same server. Without it
	// the second caller would race the first into drainAndShutdownOld
	// and observe sse.Joe's ErrProviderClosed from a second Shutdown
	// on the same instance.
	lifecycleMu sync.Mutex

	stop      chan struct{}
	stopped   sync.Once
	keepalive sync.WaitGroup
}

// NewHub constructs a Hub. BufferSize is the cap of the replay buffer
// (used for resume via Last-Event-ID / ?from / ?from_timestamp);
// values <= 0 disable replay entirely (sse.Joe accepts a nil
// Replayer); values of 1 are clamped to 2 because the library
// requires a count of at least 2. Now is the clock the replayer's
// timestamp index uses; nil falls back to time.Now. The caller should
// wire this to internal/clock.Clock.Now when a controllable clock is
// present so ?from_timestamp behaves deterministically in
// clock-controlled tests.
func NewHub(bufferSize int, now func() time.Time) (*Hub, error) {
	if bufferSize == 1 {
		// The library's NewFiniteReplayer enforces count >= 2;
		// clamp to that minimum rather than crashing the process
		// at startup over a one-off configuration choice.
		bufferSize = 2
	}
	h := &Hub{
		bufferSize: bufferSize,
		now:        now,
		active:     make(map[int]context.CancelFunc),
		stop:       make(chan struct{}),
	}
	if err := h.build(); err != nil {
		return nil, err
	}
	h.keepalive.Add(1)
	go h.runKeepAlive()
	return h, nil
}

// build creates a fresh *sse.Server + optional recordingReplayer.
// Caller must hold mu.Lock (or be the constructor before any goroutine
// can observe the Hub).
func (h *Hub) build() error {
	joe := &sse.Joe{}
	if h.bufferSize > 0 {
		rr, err := newRecordingReplayer(h.bufferSize, h.now)
		if err != nil {
			return err
		}
		joe.Replayer = rr
		h.replayer = rr
	} else {
		h.replayer = nil
	}
	srv := &sse.Server{Provider: joe}
	srv.OnSession = h.onSession
	h.server = srv
	return nil
}

// Publish broadcasts evt to every subscriber whose topic set
// intersects. The message is sent to broadcastTopic (reaches every
// filterless subscriber) and to evt.Type (reaches every filtered
// subscriber that listed this type). Keep-alives use a separate
// topic and never go through this path.
//
// The RLock is held across server.Publish so a concurrent Reset
// can't swap h.server underneath an in-flight publish and produce
// a spurious "provider is closed" error.
func (h *Hub) Publish(evt Event) error {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.closed || h.server == nil {
		return errors.New("events: hub is closed")
	}
	msg := &sse.Message{Type: sse.Type(evt.Type)}
	if evt.ID != "" {
		msg.ID = sse.ID(evt.ID)
	}
	if len(evt.Payload) > 0 {
		msg.AppendData(string(evt.Payload))
	}
	topics := []string{broadcastTopic}
	if evt.Type != "" {
		topics = append(topics, evt.Type)
	}
	return h.server.Publish(msg, topics...)
}

// Reset swaps in a fresh server + replay buffer (so any concurrent
// Publish atomically moves to the new instance), drains the
// subscribers that were attached to the old server, then shuts the
// old server down — all while the new server is already serving new
// subscribers and publishes. The swap-before-shutdown ordering is
// what closes the Publish/Reset race: a publisher that grabbed the
// RLock immediately before Reset's mu.Lock acquired sees the OLD
// server (and Reset's Lock blocks until the publish completes
// because Publish holds RLock across the call); every publisher
// after that sees the NEW server. The OLD server is then shut down
// with no concurrent publish in flight.
//
// Intended for the /admin0/reset control-plane hook between tests.
// Idempotent under concurrent callers (serialised via lifecycleMu).
func (h *Hub) Reset(ctx context.Context) error {
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	oldServer := h.server
	// Snapshot the active set BEFORE rebuilding so new subscribers
	// attaching to the freshly-built server don't get caught up in
	// the drain of the old one.
	oldActive := h.takeActiveLocked()
	if err := h.build(); err != nil {
		h.mu.Unlock()
		return err
	}
	h.mu.Unlock()

	// Subscribers attached to oldServer wake up via context cancel,
	// drainable suppresses any late library writes, ServeHTTP
	// returns. Done lock-free — nothing references oldActive's
	// cancels beyond this loop.
	for _, c := range oldActive {
		c()
	}

	if oldServer != nil {
		return flattenShutdownErr(oldServer.Shutdown(ctx))
	}
	return nil
}

// Shutdown drains every subscriber, stops the keep-alive goroutine,
// and marks the hub closed permanently. Intended for process
// shutdown. Idempotent — extra calls are no-ops.
//
// Uses the same swap-before-shutdown ordering as Reset to keep
// in-flight publishers race-free: the swap to a nil server happens
// atomically under mu.Lock, then the old server is shut down with no
// lock held.
func (h *Hub) Shutdown(ctx context.Context) error {
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	h.stopped.Do(func() { close(h.stop) })
	h.keepalive.Wait()

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	oldServer := h.server
	oldActive := h.takeActiveLocked()
	h.server = nil
	h.replayer = nil
	h.closed = true
	h.mu.Unlock()

	for _, c := range oldActive {
		c()
	}

	if oldServer != nil {
		return flattenShutdownErr(oldServer.Shutdown(ctx))
	}
	return nil
}

// takeActiveLocked returns the current active-subscriber cancel set
// and clears the map. Caller must hold mu.Lock (or be otherwise sole
// owner). The activeMu lock here is conservative — mu.Lock alone
// already excludes Handler's register/unregister paths, but holding
// activeMu keeps the invariant local to this method.
func (h *Hub) takeActiveLocked() []context.CancelFunc {
	h.activeMu.Lock()
	defer h.activeMu.Unlock()
	out := make([]context.CancelFunc, 0, len(h.active))
	for _, c := range h.active {
		out = append(out, c)
	}
	h.active = make(map[int]context.CancelFunc)
	// Restart the lifetime counter so TotalSubscribers reports per
	// window: each Reset (the /admin0/reset between-test hook) begins
	// a fresh count. Safe to reset the id allocator too — the map was
	// just cleared, so ids restarting at 0 collide with nothing.
	h.nextSub = 0
	return out
}

// ActiveSubscribers reports how many subscribers are connected to
// GET /events right now. A subscriber leaves the active set only when
// the server's read loop notices its connection closed, so a reading
// taken immediately after a client disconnects may briefly lag.
func (h *Hub) ActiveSubscribers() int {
	h.activeMu.Lock()
	defer h.activeMu.Unlock()
	return len(h.active)
}

// TotalSubscribers reports how many subscriptions have connected since
// the hub was created or last Reset. It increments on every connect
// and never decrements within a window; Reset zeroes it.
func (h *Hub) TotalSubscribers() int {
	h.activeMu.Lock()
	defer h.activeMu.Unlock()
	return h.nextSub
}

// flattenShutdownErr collapses the benign cases of sse.Server.Shutdown
// — already-shut-down ("provider is closed") and context.Canceled —
// to nil so back-to-back lifecycle calls stay idempotent.
func flattenShutdownErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return nil
	}
	if strings.Contains(err.Error(), "provider is closed") {
		return nil
	}
	return err
}

func (h *Hub) runKeepAlive() {
	defer h.keepalive.Done()
	t := time.NewTicker(keepAliveInterval())
	defer t.Stop()
	for {
		select {
		case <-h.stop:
			return
		case <-t.C:
			h.mu.RLock()
			server, closed := h.server, h.closed
			h.mu.RUnlock()
			if closed || server == nil {
				continue
			}
			msg := &sse.Message{}
			msg.AppendComment("keep-alive")
			// Publish to keepAliveTopic only. Every subscriber
			// (filterless or filtered) subscribes to keepAliveTopic
			// in addition to their content topics, so each one
			// receives exactly one heartbeat per tick.
			_ = server.Publish(msg, keepAliveTopic)
		}
	}
}

// Handler returns the HTTP handler for GET /events. Wire it under
// bearer middleware at mount time.
//
// The handler:
//  1. Disables the http.Server WriteTimeout for this connection (SSE
//     is long-lived; the server default would tear down healthy
//     subscribers after the configured timeout).
//  2. Promotes Auth0's ?from and ?from_timestamp query parameters to
//     the SSE-spec Last-Event-ID header so the library's native
//     replay path picks them up. ?from wins over ?from_timestamp.
//     ?from_timestamp accepts RFC 3339; clients that send the
//     timezone `+` unencoded (which URL-decodes to space) are
//     tolerated by retrying with the space restored.
//  3. Surfaces aged-out resume requests as 410 Gone (matching the
//     OpenAPI declaration). Unparseable ?from_timestamp returns 400
//     with the standard mgmt error envelope.
//  4. Pre-flushes the SSE response headers so http.Client.Do returns
//     immediately rather than waiting for the first event.
//  5. Tracks the request context in the active set so Reset /
//     Shutdown can drain in-flight subscribers cleanly.
//  6. Delegates to the underlying *sse.Server, which uses an
//     OnSession callback to parse `?event_type=...` into the
//     subscriber's topic list.
func (h *Hub) Handler() http.Handler {
	return http.HandlerFunc(h.serveHTTP)
}
