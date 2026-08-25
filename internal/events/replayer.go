package events

import (
	"sync"
	"time"

	"github.com/tmaxmax/go-sse"
)

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

// expire drops entries from the front of the index and reports how
// many it dropped. An empty before clears the index; otherwise
// everything older than before goes and before itself stays, so it
// remains a valid resume point. A before that isn't indexed — already
// evicted, or never seen — drops nothing, which makes repeat calls
// idempotent.
//
// Matching is by exact id against the FIRST occurrence, which is what
// go-sse's own findIDInQueue does, so the index and the inner buffer
// agree on where a cursor sits. Nothing upstream enforces unique
// offsets, and expiry is ill-defined against a duplicated one: with
// entries [0, 1, 0], expire("0") matches at position 0 and drops
// nothing, and expire("1") leaves the trailing "0" resumable. Matching
// the last occurrence instead would only move the divergence into the
// inner replayer, which would still resume from the first. Push unique
// offsets.
//
// Only the index is truncated, never the inner FiniteReplayer, whose
// queue the library keeps unexported. The dropped messages therefore
// linger there until cap more events overwrite them; what makes them
// unresumable is that both readers of "is this cursor live" — the hub's
// 410 gate via Has, and recordingReplayer.Replay — consult this index.
// The index stays a suffix of the inner buffer, so it never names an id
// the inner replayer has already evicted.
func (r *ringIndex) expire(before string) int {
	drop := len(r.entries)
	if before != "" {
		drop = -1
		for i, e := range r.entries {
			if e.id == before {
				drop = i
				break
			}
		}
		if drop < 0 {
			return 0
		}
	}
	if drop == 0 {
		return 0
	}
	// Shift the survivors down in place rather than re-slicing, so the
	// index keeps its original backing array (and capacity) the way put
	// does when it evicts. Zero the vacated tail so the expired ids
	// aren't left reachable through the slice's capacity. This reclaims
	// the index entries only — the *sse.Message payloads stay referenced
	// by the inner FiniteReplayer's fixed-size queue until cap more
	// events overwrite them, so expiry is about resumability, not memory.
	kept := copy(r.entries, r.entries[drop:])
	clear(r.entries[kept:len(r.entries)])
	r.entries = r.entries[:kept]
	return drop
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
//
// Messages without an id are live-only: error control frames and
// keep-alive comments carry no resume cursor, so they're delivered to
// current subscribers but never stored for replay. Skip the inner
// FiniteReplayer for them — it requires an id (autoIDs is off) and would
// otherwise reject them with "message has no ID".
func (r *recordingReplayer) Put(msg *sse.Message, topics []string) (*sse.Message, error) {
	if !msg.ID.IsSet() {
		return msg, nil
	}
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

// Replay serves the resume, subject to the index still holding the
// cursor. Hub.Handler runs the same membership check up-front via Has,
// but that one exists to produce the 410 status BEFORE the SSE response
// is committed — it cannot be the only check, because it is not atomic
// with this call. Returning a non-nil error from this path would let
// go-sse propagate it via http.Error into the SSE wire body — invisible
// to the user but ugly when it lands — so an unresumable cursor here
// replays nothing instead. In the rare race where Has returned true but
// a concurrent Put evicted the ID (or a concurrent Expire dropped it)
// before Subscribe ran Replay, the subscriber simply joins live, which
// is observationally indistinguishable from a legitimate "buffer was
// empty for this ID" outcome.
func (r *recordingReplayer) Replay(sub sse.Subscription) error {
	// Expire truncates the index but cannot reach into the inner
	// FiniteReplayer, which still holds the messages — so an expired ID
	// that gets this far would be replayed. Hub.Handler's 410 gate runs
	// before Subscribe, not atomically with it, so a subscribe racing a
	// concurrent Expire lands exactly here; this check, not that gate, is
	// what actually enforces "an expired cursor is not resumable".
	//
	// It cannot turn into a 410: by the time Joe calls Replay the handler
	// has already committed a 200 and flushed the SSE headers. Such a
	// subscriber therefore joins live with nothing replayed. That window
	// is inherent to checking before committing a response, and it is the
	// same outcome the existing eviction race produces.
	if !sub.LastEventID.IsSet() {
		return r.inner.Replay(sub)
	}
	// Hold the read lock ACROSS the delegate. Checking membership and
	// then releasing would leave the same window open: Expire could land
	// between the check and the emit and the subscriber would still be
	// served aged-out events. Holding it makes the pair atomic against
	// Expire, so a concurrent expiry either fully precedes this replay or
	// fully follows it.
	//
	// No deadlock: Put and Replay are both driven by sse.Joe's single
	// goroutine, so they never nest, and the other readers (Has,
	// OldestID, IDBefore) take the same shared lock. Expire is the only
	// writer and simply waits out the replay. The cost is that Expire
	// blocks for as long as an in-flight replay takes to write to its
	// subscriber — acceptable because a subscriber slow enough to matter
	// has already stalled Joe itself, which blocks every publish too.
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.idx.has(sub.LastEventID.String()) {
		return nil
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
