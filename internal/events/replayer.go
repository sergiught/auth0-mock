package events

import (
	"sync"
	"time"

	"github.com/tmaxmax/go-sse"
)

// ringIndex mirrors the contents of the underlying FiniteReplayer: the
// same ids, in the same order, evicted oldest-first at the same
// capacity. Staying in lock-step is what makes it safe to answer
// questions about the inner buffer without being able to read it —
// go-sse keeps its queue unexported.
//
// Used by recordingReplayer to translate ?from_timestamp into a
// Last-Event-ID, to answer "is this cursor still resumable", and to
// expire cursors on demand. Lookups are O(n) over at most cap entries
// (cap == replay buffer size, default 100), which is fine for this
// workload.
type ringIndex struct {
	cap     int
	entries []indexEntry
}

// indexEntry mirrors one message in the inner buffer. Expired marks a
// cursor aged out by expire: the entry stays in place — removing it
// would break lock-step with the inner buffer, which still holds the
// message — and is dropped only when eviction reaches it.
type indexEntry struct {
	id      string
	at      time.Time
	expired bool
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

// firstIndex returns the position of the first entry with this id, or
// -1. First, not last, because that is what go-sse's findIDInQueue
// resolves a Last-Event-ID to — every id question has to be answered
// about the same copy the library would replay from, or the two
// disagree and aged-out events go out on the wire.
func (r *ringIndex) firstIndex(id string) int {
	for i, e := range r.entries {
		if e.id == id {
			return i
		}
	}
	return -1
}

// resumable reports whether position i is a cursor a subscriber may
// resume from: live, and the copy the library would resolve its id to.
// A live entry whose id also appears earlier is NOT resumable — the
// library would start from that earlier copy instead.
func (r *ringIndex) resumable(i int) bool {
	return !r.entries[i].expired && r.firstIndex(r.entries[i].id) == i
}

// idBefore returns the ID of the latest resumable event whose timestamp
// is strictly less than t. Ok=false means nothing usable predates t —
// the caller should drop any Last-Event-ID hint so the subscriber joins
// live rather than resuming from an aged-out cursor.
func (r *ringIndex) idBefore(t time.Time) (string, bool) {
	var (
		bestID string
		found  bool
	)
	for i, e := range r.entries {
		if !e.at.Before(t) {
			break
		}
		if r.resumable(i) {
			bestID = e.id
			found = true
		}
	}
	return bestID, found
}

// has reports whether id is currently resumable — present, and not
// expired. Answered about the first copy, per firstIndex.
func (r *ringIndex) has(id string) bool {
	i := r.firstIndex(id)
	return i >= 0 && !r.entries[i].expired
}

// oldestResumableID returns the oldest cursor a subscriber can still
// resume from, or "" if there is none.
func (r *ringIndex) oldestResumableID() string {
	for i := range r.entries {
		if r.resumable(i) {
			return r.entries[i].id
		}
	}
	return ""
}

// expire marks cursors aged out and reports how many it newly marked.
// An empty before expires everything; otherwise everything older than
// before is expired and before itself stays resumable. A before the
// index doesn't hold expires nothing, which makes repeat calls
// idempotent — and means 0 does not distinguish "nothing was older"
// from "never seen".
//
// Entries are marked rather than removed. Truncating would desync this
// mirror from the inner FiniteReplayer, which cannot be truncated with
// it: the expired messages stay in the library's queue, so a later
// event reusing an expired id — a test that expires and then numbers
// its offsets from 0 again — would resolve to the old copy and replay
// everything after it. Marking keeps the two aligned, so every id
// question is answered about the copy the library would actually use.
func (r *ringIndex) expire(before string) int {
	limit := len(r.entries)
	if before != "" {
		limit = r.firstIndex(before)
		if limit < 0 {
			return 0
		}
	}
	n := 0
	for i := range limit {
		if !r.entries[i].expired {
			r.entries[i].expired = true
			n++
		}
	}
	return n
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

// expiryFilter drops any message whose cursor is no longer resumable on
// its way to the subscriber. It is what makes expiry safe mid-replay:
// Hub.Handler's 410 gate and Replay's own check both run before the
// first byte goes out, so without this an Expire landing during a
// replay would still let the rest of the buffer through.
//
// Each check takes the read lock only for the lookup, never across the
// Send — holding it across the write would let a subscriber that has
// stopped reading (the handler clears the write deadline for SSE) park
// a pending Expire writer, and Go's RWMutex would then queue every new
// reader behind that writer, hanging the 410 gate for unrelated
// requests. The go-sse Replayer contract asks the same: block for as
// little as possible.
type expiryFilter struct {
	inner sse.MessageWriter
	r     *recordingReplayer
}

func (f *expiryFilter) Send(m *sse.Message) error {
	if m.ID.IsSet() && !f.r.Has(m.ID.String()) {
		return nil
	}
	return f.inner.Send(m)
}

func (f *expiryFilter) Flush() error { return f.inner.Flush() }

// Replay serves the resume, subject to the cursor still being
// resumable. Hub.Handler runs the same check up-front via Has, but that
// one exists to produce the 410 status BEFORE the SSE response is
// committed; it cannot be the only check, because it is not atomic with
// this call. Returning a non-nil error from this path would let go-sse
// propagate it via http.Error into the SSE wire body — invisible to the
// user but ugly when it lands — so an unresumable cursor replays
// nothing instead.
//
// A subscriber whose cursor is expired between the gate and here
// therefore joins live with nothing replayed, rather than getting a
// 410: by this point the handler has already committed a 200 and
// flushed the SSE headers. That window is inherent to checking before
// committing a response, and it is the same outcome the pre-existing
// eviction race produces.
func (r *recordingReplayer) Replay(sub sse.Subscription) error {
	if sub.LastEventID.IsSet() && !r.Has(sub.LastEventID.String()) {
		return nil
	}
	sub.Client = &expiryFilter{inner: sub.Client, r: r}
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
	return r.idx.oldestResumableID()
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
