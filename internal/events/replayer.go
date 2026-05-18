package events

import (
	"errors"
	"sync"
	"time"

	"github.com/tmaxmax/go-sse"
)

// ErrAgedOut is returned by recordingReplayer.Replay when a subscriber
// presents a Last-Event-ID (or a resolved ?from / ?from_timestamp) that
// the buffer no longer carries. The Hub.Handler translates it into a
// 410 Gone response — matching the `410` declared in the OpenAPI spec
// for GET /events.
var ErrAgedOut = errors.New("events: requested event ID is no longer in the replay buffer")

// ringIndex keeps the timestamp of every event currently held by the
// underlying FiniteReplayer, in insertion order. It is sized to match
// the replayer's capacity and evicts oldest-first so the two stay in
// lock-step. Used only by recordingReplayer.IDBefore to translate
// ?from_timestamp into a Last-Event-ID; lookups are O(n) over at most
// cap entries (cap == replay buffer size, default 100), which is fine
// for this workload.
type ringIndex struct {
	cap     int
	entries []indexEntry
}

type indexEntry struct {
	id string
	at time.Time
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
func (r *ringIndex) put(id string, at time.Time) {
	if len(r.entries) == r.cap {
		copy(r.entries, r.entries[1:])
		r.entries = r.entries[:r.cap-1]
	}
	r.entries = append(r.entries, indexEntry{id: id, at: at})
}

// idBefore returns the ID of the latest indexed event whose timestamp
// is strictly less than t. Ok=false means no stored event predates t —
// caller should drop any Last-Event-ID hint so the replayer streams
// the whole buffer. When every stored event predates t, returns the
// newest (so Replay sends nothing from the buffer; subscriber joins
// live).
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

// has reports whether id is currently in the index.
func (r *ringIndex) has(id string) bool {
	for _, e := range r.entries {
		if e.id == id {
			return true
		}
	}
	return false
}

// recordingReplayer wraps sse.FiniteReplayer with a same-capacity
// ringIndex so the hub can translate ?from_timestamp into a
// Last-Event-ID before delegating resume to the inner replayer. All
// Put / Replay calls pass straight through; the only added work is one
// ringIndex.put per Put and one mutex acquisition per access.
//
// Concurrency: sse.Joe serialises Put calls, but Hub.Handler reads
// IDBefore / OldestID / has from arbitrary request goroutines
// concurrent with those Put calls. The mutex makes those reads safe.
type recordingReplayer struct {
	mu    sync.RWMutex
	inner *sse.FiniteReplayer
	idx   *ringIndex
	now   func() time.Time
}

// newRecordingReplayer constructs a recordingReplayer wrapping a fresh
// FiniteReplayer of the given capacity. AutoIDs is hard-wired to false
// because the /admin0/events handler enforces the CloudEvent schema's
// `id` requirement upstream — every message arrives with an explicit
// ID, and autoIDs=true would actively reject those. Now defaults to
// time.Now when nil.
func newRecordingReplayer(capacity int, now func() time.Time) (*recordingReplayer, error) {
	inner, err := sse.NewFiniteReplayer(capacity, false)
	if err != nil {
		return nil, err
	}
	if now == nil {
		now = time.Now
	}
	return &recordingReplayer{inner: inner, idx: newRingIndex(capacity), now: now}, nil
}

// Put records the event in the index and forwards to the inner
// replayer.
func (r *recordingReplayer) Put(msg *sse.Message, topics []string) (*sse.Message, error) {
	out, err := r.inner.Put(msg, topics)
	if err != nil {
		return nil, err
	}
	if out != nil {
		r.mu.Lock()
		r.idx.put(out.ID.String(), r.now())
		r.mu.Unlock()
	}
	return out, nil
}

// Replay delegates to the inner FiniteReplayer, but first checks our
// index: if the subscriber's Last-Event-ID is set and is NOT in the
// buffer, the request has aged out. We return ErrAgedOut so the hub's
// handler can translate to HTTP 410. The library's FiniteReplayer
// silently returns nil in this case (it just skips the replay), which
// would leave the subscriber joined live and unaware they missed
// events.
func (r *recordingReplayer) Replay(sub sse.Subscription) error {
	if sub.LastEventID.IsSet() {
		r.mu.RLock()
		ok := r.idx.has(sub.LastEventID.String())
		r.mu.RUnlock()
		if !ok {
			return ErrAgedOut
		}
	}
	return r.inner.Replay(sub)
}

// OldestID returns the ID of the oldest event currently in the index,
// or "" if the index is empty. Used by Hub.Handler to translate
// ?from_timestamp-predates-everything into a Last-Event-ID hint —
// injecting the oldest ID makes the library replay everything strictly
// after it. The trade-off: the oldest stored event itself is skipped.
// In practice the buffer's default cap is 100 and the typical
// from_timestamp-before-all case is "subscriber wants the whole
// session" — a single missed event at the oldest edge is acceptable
// (and far cheaper than mirroring the library's payload queue).
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

// Has reports whether id is currently in the buffer. Used by the hub
// adapter to surface 410 Gone for unknown ?from values up-front,
// rather than waiting for Replay to do the same lookup.
func (r *recordingReplayer) Has(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.idx.has(id)
}
