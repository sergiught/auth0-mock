package events

import (
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/tmaxmax/go-sse"
)

// ringIndex is the replay buffer: a bounded, insertion-ordered ring of
// the events available for resume, evicting oldest-first at capacity.
//
// It holds the messages themselves rather than mirroring a buffer
// somebody else owns. That matters for expiry. An earlier version kept
// this as an index beside sse.FiniteReplayer, whose queue is unexported
// and so could not be expired along with it; because go-sse resolves a
// Last-Event-ID to the FIRST copy of that id in its queue, every
// divergence between the two became a correctness bug — expired events
// replayed from a stale copy, or live events unreachable behind one.
// Owning the buffer makes expiry an actual eviction and leaves exactly
// one answer to "where does this cursor point".
//
// Lookups are O(n) over at most cap entries (cap == replay buffer size,
// default 100), which is fine for this workload.
type ringIndex struct {
	cap     int
	entries []indexEntry
}

// indexEntry is one buffered event: the resume cursor, when it was
// recorded (for ?from_timestamp), and the message plus the topics it
// was published to (for the replay itself).
type indexEntry struct {
	id     string
	at     time.Time
	msg    *sse.Message
	topics []string
}

func newRingIndex(capacity int) *ringIndex {
	if capacity < 1 {
		capacity = 1
	}
	return &ringIndex{cap: capacity, entries: make([]indexEntry, 0, capacity)}
}

// put appends an entry, evicting the oldest if the buffer is full.
// Uses copy() to shift in place rather than allocating a new slice,
// keeping the operation a single memmove instead of a copy plus an
// append-into-the-truncated-slice.
func (r *ringIndex) put(id string, at time.Time, msg *sse.Message, topics []string) {
	if len(r.entries) == r.cap {
		copy(r.entries, r.entries[1:])
		r.entries = r.entries[:r.cap-1]
	}
	r.entries = append(r.entries, indexEntry{id: id, at: at, msg: msg, topics: topics})
}

// firstIndex returns the position of the first entry with this id, or
// -1. First, not last, so that every question about an id — is it
// resumable, what replays after it, what does expiry count as older —
// is answered about the same entry. Nothing upstream enforces unique
// offsets, and this is what keeps a duplicated one merely redundant
// rather than contradictory.
func (r *ringIndex) firstIndex(id string) int {
	for i, e := range r.entries {
		if e.id == id {
			return i
		}
	}
	return -1
}

// idBefore returns the ID of the latest buffered event whose timestamp
// is strictly less than t. Ok=false means no buffered event predates
// t — the caller should drop any Last-Event-ID hint so the subscriber
// joins live. When every buffered event predates t, returns the newest
// (so Replay sends nothing from the buffer; subscriber joins live).
func (r *ringIndex) idBefore(t time.Time) (string, bool) {
	var (
		bestID string
		found  bool
	)
	for _, e := range r.entries {
		if e.at.Before(t) {
			bestID = e.id
			found = true
			continue
		}
		break
	}
	return bestID, found
}

// has reports whether id is currently in the buffer.
func (r *ringIndex) has(id string) bool {
	return r.firstIndex(id) >= 0
}

// after returns the messages to replay to a subscriber resuming from
// id: everything strictly after it whose topics intersect the
// subscription's, in order. Ok=false means the id isn't buffered, which
// the caller turns into "replay nothing".
//
// Returns a snapshot so the caller can write to the subscriber without
// holding a lock. A consumer that has stopped reading would otherwise
// park an expiry behind it, and Go's RWMutex would then queue every
// later reader behind that writer, hanging the aged-out check for
// unrelated requests.
func (r *ringIndex) after(id string, topics []string) ([]*sse.Message, bool) {
	i := r.firstIndex(id)
	if i < 0 {
		return nil, false
	}
	var out []*sse.Message
	for _, e := range r.entries[i+1:] {
		if topicsIntersect(topics, e.topics) {
			out = append(out, e.msg)
		}
	}
	return out, true
}

// expire drops entries from the front of the buffer and reports how
// many it dropped. An empty before expires everything; otherwise
// everything older than before is dropped and before itself stays, so
// it remains a valid resume point. A before the buffer doesn't hold
// drops nothing, which makes repeat calls idempotent — and means 0 does
// not distinguish "nothing was older" from "never seen".
//
// The entries are really removed, messages included, so an id may be
// reused afterwards: a test that expires the buffer and then renumbers
// its offsets from zero gets a clean buffer, not a shadowed one.
func (r *ringIndex) expire(before string) int {
	drop := len(r.entries)
	if before != "" {
		drop = r.firstIndex(before)
		if drop < 0 {
			return 0
		}
	}
	if drop == 0 {
		return 0
	}
	// Shift the survivors down in place rather than re-slicing, so the
	// buffer keeps its original backing array (and capacity) the way put
	// does when it evicts. Zero the vacated tail so the dropped entries
	// stop pinning their messages.
	kept := copy(r.entries, r.entries[drop:])
	clear(r.entries[kept:len(r.entries)])
	r.entries = r.entries[:kept]
	return drop
}

// topicsIntersect reports whether two topic sets share a member. Same
// rule as go-sse's own unexported helper, which is what decides whether
// a buffered message is visible to a given subscription.
func topicsIntersect(a, b []string) bool {
	for _, at := range a {
		if slices.Contains(b, at) {
			return true
		}
	}
	return false
}

// recordingReplayer is the sse.Replayer the hub installs on sse.Joe. It
// owns the bounded replay buffer behind resume via Last-Event-ID /
// ?from / ?from_timestamp, and the on-demand expiry behind
// POST /admin0/events/expire.
//
// Concurrency: sse.Joe serialises Put and Replay onto one goroutine,
// but Hub.Handler reads Has / IDBefore / OldestID from arbitrary
// request goroutines and Expire writes from another. The mutex makes
// those safe, and is never held across a write to a subscriber.
type recordingReplayer struct {
	mu  sync.RWMutex
	idx *ringIndex
	now func() time.Time
}

// newRecordingReplayer constructs a recordingReplayer of the given
// capacity. Now defaults to time.Now when nil.
func newRecordingReplayer(capacity int, now func() time.Time) (*recordingReplayer, error) {
	// Same minimum sse.NewFiniteReplayer enforced when this type wrapped
	// it, so NewHub's clamping contract is unchanged.
	if capacity < 2 {
		return nil, errors.New("events: replay buffer capacity must be at least 2")
	}
	if now == nil {
		now = time.Now
	}
	return &recordingReplayer{idx: newRingIndex(capacity), now: now}, nil
}

// Put buffers the event for replay.
//
// Messages without an id are live-only: error control frames and
// keep-alive comments carry no resume cursor, so they're delivered to
// current subscribers but never stored. They're returned untouched so
// Joe still fans them out.
func (r *recordingReplayer) Put(msg *sse.Message, topics []string) (*sse.Message, error) {
	if !msg.ID.IsSet() {
		return msg, nil
	}
	if len(topics) == 0 {
		return nil, sse.ErrNoTopic
	}
	r.mu.Lock()
	// Clone the topics: the slice belongs to the caller, who may reuse it.
	r.idx.put(msg.ID.String(), r.now(), msg, slices.Clone(topics))
	r.mu.Unlock()
	return msg, nil
}

// Replay sends everything buffered after the subscriber's cursor.
//
// Hub.Handler runs the same membership check up-front via Has, but that
// one exists to produce the 410 status BEFORE the SSE response is
// committed; it cannot be the only check, because it is not atomic with
// this call. Returning a non-nil error from here would let go-sse
// propagate it via http.Error into the SSE wire body — invisible to the
// user but ugly when it lands — so an unknown or expired cursor replays
// nothing instead.
//
// A subscriber whose cursor is expired between the gate and here
// therefore joins live with nothing replayed, rather than getting a
// 410: by this point the handler has already committed a 200 and
// flushed the SSE headers. That window is inherent to checking before
// committing a response, and it is the same outcome the eviction race
// produces.
func (r *recordingReplayer) Replay(sub sse.Subscription) error {
	if !sub.LastEventID.IsSet() {
		return nil
	}
	r.mu.RLock()
	msgs, ok := r.idx.after(sub.LastEventID.String(), sub.Topics)
	r.mu.RUnlock()
	if !ok {
		return nil
	}
	for _, m := range msgs {
		if err := sub.Client.Send(m); err != nil {
			return err
		}
	}
	return sub.Client.Flush()
}

// OldestID returns the ID of the oldest buffered event, or "" if the
// buffer is empty. Used by Hub.Handler to translate a ?from_timestamp
// that predates everything into a Last-Event-ID hint — injecting the
// oldest ID makes the buffer replay everything strictly after it. The
// trade-off: the oldest stored event itself is skipped. In practice the
// buffer's default cap is 100 and the typical from_timestamp-before-all
// case is "subscriber wants the whole session", so a single missed
// event at the oldest edge is acceptable.
func (r *recordingReplayer) OldestID() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.idx.entries) == 0 {
		return ""
	}
	return r.idx.entries[0].id
}

// IDBefore is the timestamp→ID lookup the hub uses to translate
// ?from_timestamp into a Last-Event-ID. See ringIndex.idBefore for the
// semantics.
func (r *recordingReplayer) IDBefore(t time.Time) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.idx.idBefore(t)
}

// Has reports whether id is currently in the buffer. Used by the hub to
// surface 410 Gone for aged-out ?from values up-front, rather than
// waiting for Replay to do the same lookup.
func (r *recordingReplayer) Has(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.idx.has(id)
}

// Expire ages out buffered cursors on demand and reports how many it
// dropped. See ringIndex.expire for the before semantics. Backs
// POST /admin0/events/expire so a test can provoke the 410 aged-out
// path without pushing past the buffer's capacity or resetting the
// whole mock.
func (r *recordingReplayer) Expire(before string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.idx.expire(before)
}
