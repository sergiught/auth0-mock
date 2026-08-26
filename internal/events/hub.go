package events

import (
	"context"
	"errors"
	"net/http"
	"strconv"
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

// keepAliveTopic is the every-subscriber fan-out topic. Every
// subscriber — filtered or not — subscribes to it, so it carries
// messages that must reach all of them regardless of event_type filter:
// keep-alive comments (so filtered subscribers behind idle-timeout
// proxies still get heartbeats), error control frames (which Auth0
// delivers to every consumer), and offset-only progress markers (which
// advance the whole stream's cursor). Regular events never target this
// topic, so they don't bypass event filtering.
const keepAliveTopic = "__keep_alive__"

// errorEventType is the CloudEvent discriminator for an in-band error
// message. Error frames are terminal control signals, not regular
// events: delivered to every subscriber, never buffered for replay, and
// followed by the stream closing — see Publish.
const errorEventType = "error"

// offsetOnlyEventType is the CloudEvent discriminator for a progress
// marker that advances the cursor without carrying event data. Like
// error frames it reaches every subscriber regardless of filter, but it
// carries an offset, so it IS buffered and is a valid resume point — see
// Publish.
const offsetOnlyEventType = "offset-only"

// barrierTopic is an internal topic no subscriber ever joins. Publishing
// to it is a no-op delivery whose only purpose is the serialisation
// barrier in Publish's error path — see the comment there.
const barrierTopic = "__barrier__"

// DefaultKeepAliveInterval is the cadence at which a `:keep-alive`
// comment is broadcast to every connected subscriber. 15s matches
// what most SSE deployments use; the library doesn't auto-emit.
const DefaultKeepAliveInterval = 15 * time.Second

// DefaultReconnectHint is the value sent in the SSE `retry:` field on
// connect, telling clients how long to wait before reconnecting after a
// disconnect (Auth0's Events API sends one too). A value <= 0 omits the
// hint, so clients fall back to their built-in default.
const DefaultReconnectHint = 3 * time.Second

// HubOption customises a Hub at construction.
type HubOption func(*Hub)

// WithReconnectHint sets the SSE `retry:` reconnect-delay hint sent on
// connect; <= 0 omits it. Wire it to the EVENTS_RECONNECT_HINT config.
// Defaults to DefaultReconnectHint when unset.
func WithReconnectHint(d time.Duration) HubOption {
	return func(h *Hub) { h.reconnectHint = d }
}

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

	// ReconnectHint is the SSE `retry:` value sent on connect; <= 0
	// omits it. ConnectFrame is the precomputed first frame (a
	// :connected comment plus the retry hint), built once in NewHub.
	reconnectHint time.Duration
	connectFrame  []byte

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
	// NextSub allocates active-map keys and is monotonic for the life
	// of the Hub — it is NOT reset by Reset. Recycling keys would let a
	// pre-Reset subscriber's deferred cleanup (which deletes by its
	// captured id) evict a post-Reset subscriber that happened to reuse
	// the id. The per-window lifetime count lives in totalSubs instead,
	// which is what Reset zeroes.
	nextSub   int
	totalSubs atomic.Int64

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
// Replayer). Now is the clock the replayer's
// timestamp index uses; nil falls back to time.Now. The caller should
// wire this to internal/clock.Clock.Now when a controllable clock is
// present so ?from_timestamp behaves deterministically in
// clock-controlled tests. Opts customise the hub — e.g.
// WithReconnectHint sets the SSE retry: value sent on connect.
func NewHub(bufferSize int, now func() time.Time, opts ...HubOption) (*Hub, error) {
	h := &Hub{
		bufferSize:    bufferSize,
		now:           now,
		reconnectHint: DefaultReconnectHint,
		active:        make(map[int]context.CancelFunc),
		stop:          make(chan struct{}),
	}
	for _, opt := range opts {
		opt(h)
	}
	h.connectFrame = buildConnectFrame(h.reconnectHint)
	if err := h.build(); err != nil {
		return nil, err
	}
	h.keepalive.Add(1)
	go h.runKeepAlive()
	return h, nil
}

// buildConnectFrame is the first SSE frame sent to a new subscriber,
// matching Auth0's Events API: a `:connected` readiness comment plus an
// optional `retry:` reconnect hint. Both are non-events, so SSE readers
// that skip comments and `retry:` ignore the frame.
func buildConnectFrame(reconnectHint time.Duration) []byte {
	// Anything under a millisecond rounds to `retry: 0` ("reconnect
	// immediately"), which is a hot-loop footgun — worse than the intended
	// "no hint, use your default". So omit the field for sub-ms values too.
	if reconnectHint < time.Millisecond {
		return []byte(":connected\n\n")
	}
	return []byte(":connected\nretry: " + strconv.FormatInt(reconnectHint.Milliseconds(), 10) + "\n\n")
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
// intersects. A regular event is sent to broadcastTopic (reaches every
// filterless subscriber) and to evt.Type (reaches every filtered
// subscriber that listed this type). Error frames and offset-only
// progress markers are stream-wide control signals routed to the
// every-subscriber topic — see the branches below. Keep-alives use a
// separate topic and never go through this method.
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
	if evt.Type == errorEventType {
		return h.publishError(msg)
	}
	if evt.Type == offsetOnlyEventType {
		// A progress marker advances the whole stream's cursor, so it
		// reaches every subscriber regardless of event_type filter (via
		// the every-subscriber topic). It carries an offset id, so the
		// replayer buffers it like a real event and a consumer can resume
		// from its offset.
		return h.server.Publish(msg, keepAliveTopic)
	}
	if evt.Type != "" {
		return h.server.Publish(msg, broadcastTopic, evt.Type)
	}
	return h.server.Publish(msg, broadcastTopic)
}

// publishError delivers an in-band error control frame the way Auth0's
// Events API does: to every connected subscriber regardless of
// event_type filter, never stored for replay, and followed by the
// stream closing. Caller holds mu.RLock.
//
//   - All subscribers join keepAliveTopic, so it's the fan-out topic
//     that reaches filtered and filterless subscribers alike. The frame
//     carries no id (errors aren't a resume point), so recordingReplayer
//     skips buffering it.
//   - go-sse's Joe runs a single goroutine and Publish returns once a
//     message is queued, before its fan-out completes. Closing the
//     subscribers immediately would race that fan-out and could truncate
//     the error frame. A throwaway publish to a topic no one subscribes
//     to acts as a barrier: because Joe processes messages in order on
//     one goroutine, the barrier's Publish only returns after the error
//     frame's fan-out has finished — so the close below is safe.
func (h *Hub) publishError(msg *sse.Message) error {
	if err := h.server.Publish(msg, keepAliveTopic); err != nil {
		return err
	}
	barrier := &sse.Message{}
	barrier.AppendComment("barrier")
	_ = h.server.Publish(barrier, barrierTopic)
	h.closeActiveSubscribers()
	return nil
}

// closeActiveSubscribers cancels every active subscriber, closing their
// SSE connections. Unlike takeActiveLocked (Reset/Shutdown) it leaves
// the hub serving: each subscriber's own cleanup removes it from the
// active set as it tears down.
func (h *Hub) closeActiveSubscribers() {
	h.activeMu.Lock()
	cancels := make([]context.CancelFunc, 0, len(h.active))
	for _, c := range h.active {
		cancels = append(cancels, c)
	}
	h.activeMu.Unlock()
	for _, c := range cancels {
		c()
	}
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

// ExpireAll ages out every buffered resume cursor and reports how many
// it dropped. Returns 0 when replay is disabled.
//
// This is the deterministic way to provoke the aged-out path: a
// subscriber that later resumes from an expired cursor gets 410 Gone /
// event_aged_out, without having to push past the buffer's capacity or
// reset the whole mock. Unlike Reset it touches nothing else — live
// subscribers keep streaming, the server is not rebuilt, and counters
// are left alone.
//
// Both this and ExpireBefore hold mu across the call, the way Publish
// does, so the expiry and the replayer it reads are not torn apart by a
// concurrent Reset swapping the buffer in between. Neither can stop a
// Reset that lands immediately afterwards from discarding what was just
// expired; the count is only ever true as of the moment it was taken.
//
// Holding mu costs nothing extra: the replayer's own write lock is held
// only for the truncation, because Replay writes to its subscriber
// unlocked. It is NOT a guarantee that an expiry can never block — a
// subscriber that stops reading stalls Joe, which leaves Publish
// holding mu.RLock, and a Reset queued behind it then blocks every
// later RLock including this one. That hazard predates expiry and is
// shared with the keep-alive and the handler's aged-out lookup.
//
// Both report 0 on a closed hub rather than erroring as Publish does:
// nothing was expired, and the hub only reaches that state during
// shutdown. ExpireBefore additionally reports the cursor as not found
// there and when replay is disabled, which is literally true — there is
// no buffer holding it — and gives the endpoint a 404 to answer with
// instead of a success that expired nothing.
func (h *Hub) ExpireAll() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.closed || h.replayer == nil {
		return 0
	}
	return h.replayer.ExpireAll()
}

// ExpireBefore ages out every cursor older than cursor, leaving cursor
// itself — and everything after it — resumable. It reports how many it
// dropped and whether the buffer held cursor at all; a cursor it
// doesn't hold drops nothing. Found is false when replay is disabled.
//
// Repeating the call is therefore safe but not silent: the second one
// drops 0 and reports found, because cursor is still buffered — it is
// the boundary, not one of the casualties. It only stops being found
// once something else evicts it.
//
// An empty cursor drops nothing rather than falling through to
// expire-everything: ExpireAll is how you say that, all the way down to
// the buffer, so no caller can widen the blast radius by passing
// through an unset value.
func (h *Hub) ExpireBefore(cursor string) (dropped int, found bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.closed || h.replayer == nil {
		return 0, false
	}
	return h.replayer.ExpireBefore(cursor)
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
// and clears the map. Caller must hold mu.Lock. The activeMu lock here
// is required, not just conservative: the Handler register/unregister
// paths synchronise on activeMu alone (never mu), so without it the
// snapshot could race a concurrent registerSub or cleanup.
func (h *Hub) takeActiveLocked() []context.CancelFunc {
	h.activeMu.Lock()
	defer h.activeMu.Unlock()
	out := make([]context.CancelFunc, 0, len(h.active))
	for _, c := range h.active {
		out = append(out, c)
	}
	h.active = make(map[int]context.CancelFunc)
	// Restart the lifetime counter so TotalSubscribers reports per
	// window: each Reset (the /admin0/reset between-test hook) begins a
	// fresh count. Deliberately do NOT reset nextSub — see its field
	// comment: a drained subscriber's cleanup still deletes by its
	// captured id after this returns, so recycled ids could evict a
	// post-Reset subscriber.
	h.totalSubs.Store(0)
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
	return int(h.totalSubs.Load())
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
	if errors.Is(err, sse.ErrProviderClosed) {
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
//  2. Parses the query string strictly. A string that will not
//     unescape is 400 invalid_query rather than a 200 that silently
//     dropped the pair it choked on — Go's parser keeps the pairs it
//     managed and discards both the error and the bad pair, so a lost
//     ?from would join live instead of 410-ing and a lost ?event_type
//     would turn a filtered subscription into a firehose.
//  3. Refuses the resume shapes that parse cleanly and then mean
//     something the caller did not ask for, each with its own code:
//     an empty (or whitespace-only) ?from, ?from_timestamp,
//     ?event_type or Last-Event-ID is 400 invalid_from /
//     invalid_from_timestamp / invalid_event_type /
//     invalid_last_event_id, because present-but-empty is a different
//     request from omitted; a repeated ?from or ?from_timestamp is
//     400 invalid_query, since a cursor can only name one position.
//     Repeating ?event_type stays legal — that is how a caller asks
//     for several types. Unknown parameters are ignored, as the real
//     Auth0 API ignores them.
//  4. Promotes Auth0's ?from and ?from_timestamp query parameters to
//     the SSE-spec Last-Event-ID header so the replay buffer resolves
//     them on the normal resume path. ?from wins over ?from_timestamp,
//     and an explicit Last-Event-ID wins over both — but all three are
//     validated first, so a malformed ?from_timestamp is rejected even
//     when a winning ?from means it would never be read.
//     ?from_timestamp accepts RFC 3339; clients that send the
//     timezone `+` unencoded (which URL-decodes to space) are
//     tolerated by retrying with the space restored.
//  5. Surfaces aged-out resume requests as 410 Gone (matching the
//     OpenAPI declaration). Every 400 above uses the standard mgmt
//     error envelope.
//  6. Pre-flushes the SSE response headers so http.Client.Do returns
//     immediately rather than waiting for the first event.
//  7. Tracks the request context in the active set so Reset /
//     Shutdown can drain in-flight subscribers cleanly.
//  8. Delegates to the underlying *sse.Server, which uses an
//     OnSession callback to parse `?event_type=...` into the
//     subscriber's topic list.
func (h *Hub) Handler() http.Handler {
	return http.HandlerFunc(h.serveHTTP)
}
