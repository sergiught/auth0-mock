package events

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/tmaxmax/go-sse"
)

// Hub is the SSE fan-out the mock owns. One Hub per process; the
// HTTP handler at GET /events is hub.Handler(), and POST /admin0/events
// pushes via hub.Publish. Hub is safe for concurrent use — every
// underlying primitive (sse.Server, sse.Joe, recordingReplayer's
// internals) is.
type Hub struct {
	server   *sse.Server
	replayer *recordingReplayer // Nil when buffer is disabled.
}

// NewHub constructs a Hub. BufferSize is the cap of the replay buffer
// (used for resume via Last-Event-ID / ?from / ?from_timestamp);
// values <= 0 disable replay entirely (sse.Joe accepts a nil Replayer).
// Now is the clock the replayer's timestamp index uses; nil falls back
// to time.Now. The caller should wire this to internal/clock.Clock.Now
// when a controllable clock is present so ?from_timestamp behaves
// deterministically in clock-controlled tests.
func NewHub(bufferSize int, now func() time.Time) (*Hub, error) {
	h := &Hub{}
	joe := &sse.Joe{}
	if bufferSize > 0 {
		rr, err := newRecordingReplayer(bufferSize, now)
		if err != nil {
			return nil, err
		}
		joe.Replayer = rr
		h.replayer = rr
	}
	h.server = &sse.Server{Provider: joe}
	return h, nil
}

// broadcastTopic is the dedicated topic filterless subscribers join.
// Publishers send every event to broadcastTopic AND evt.Type so:
//   - Filterless subscribers (subscribed only to broadcastTopic)
//     receive every event.
//   - Filtered subscribers (subscribed only to their requested
//     event_type list) receive only matching events.
//
// Using sse.DefaultTopic for both purposes (the previous design) made
// every filtered subscriber implicitly receive every event because the
// DefaultTopic intersected on both sides. A distinct broadcast topic
// keeps the two cases isolated.
const broadcastTopic = "__broadcast__"

// Publish broadcasts evt to every subscriber whose topic set
// intersects. The message is sent to broadcastTopic (reaches every
// filterless subscriber) and to evt.Type (reaches every filtered
// subscriber that listed this type).
func (h *Hub) Publish(evt Event) error {
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

// Handler returns the HTTP handler for GET /events. Wire it under
// bearer middleware at mount time. The handler delegates to the
// underlying *sse.Server, which uses an OnSession callback to parse
// `?event_type=...` into the subscriber's topic list. Filterless
// subscribers (no event_type query) subscribe to sse.DefaultTopic;
// filtered subscribers subscribe to the requested types AND
// sse.DefaultTopic so they still receive untyped publishes (the
// library deduplicates per subscriber).
//
// The wrapper also pre-flushes the SSE response headers. The library
// otherwise defers writing headers until the first Send, which keeps
// http.Client.Do blocked until an event lands — surprising for test
// rigs and reactive consumers that want to confirm the connection is
// live before publishing.
//
// A later task adds an adapter that promotes ?from / ?from_timestamp
// to Last-Event-ID; today it just pre-flushes and delegates.
func (h *Hub) Handler() http.Handler {
	h.server.OnSession = h.onSession
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		if f, ok := w.(http.Flusher); ok {
			w.WriteHeader(http.StatusOK)
			f.Flush()
		}
		h.server.ServeHTTP(w, r)
	})
}

func (h *Hub) onSession(_ http.ResponseWriter, r *http.Request) (topics []string, allowed bool) {
	requested := r.URL.Query()["event_type"]
	if len(requested) == 0 {
		// Filterless: only the broadcast topic. Publishers send every
		// event there.
		return []string{broadcastTopic}, true
	}
	// Filtered: ONLY the requested types. NOT broadcastTopic — that
	// would defeat the filter.
	return requested, true
}

// Shutdown drains every open subscription. Called from server shutdown
// and from POST /admin0/reset so tests don't leak SSE state across each
// other. Errors from the library on a second call are tolerated by the
// admin0 reset wiring — by the time reset fires the test verdict is
// usually decided and we'd rather not mask the original failure.
func (h *Hub) Shutdown(ctx context.Context) error {
	if h.server == nil {
		return nil
	}
	err := h.server.Shutdown(ctx)
	// Sse.Server.Shutdown returns an error when called on an already-
	// shut server (depending on library version); flatten that to nil
	// so reset wiring stays idempotent.
	if err != nil && errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
