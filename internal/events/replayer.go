package events

import (
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
func (r *ringIndex) put(id string, at time.Time) {
	if len(r.entries) == r.cap {
		r.entries = append(r.entries[:0], r.entries[1:]...)
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

// recordingReplayer wraps sse.FiniteReplayer with a same-capacity
// ringIndex so the hub can translate ?from_timestamp into a
// Last-Event-ID before delegating resume to the inner replayer. All
// Put / Replay calls pass straight through; the only added work is one
// ringIndex.put per Put.
type recordingReplayer struct {
	inner *sse.FiniteReplayer
	idx   *ringIndex
	now   func() time.Time
}

// newRecordingReplayer constructs a recordingReplayer wrapping a fresh
// FiniteReplayer of the given capacity. AutoIDs is hard-wired to true
// so events pushed without an explicit ID still get one (which the
// index needs to be useful). Now defaults to time.Now when nil.
func newRecordingReplayer(capacity int, now func() time.Time) (*recordingReplayer, error) {
	// AutoIDs is false: the /admin0/events handler enforces the
	// CloudEvent schema's `id` requirement before Hub.Publish ever
	// reaches us, so every message arrives with an ID already set.
	// AutoIDs=true would actively reject messages with explicit IDs
	// ("message already has an ID, can't use generated ID"), which is
	// the opposite of what we want.
	inner, err := sse.NewFiniteReplayer(capacity, false)
	if err != nil {
		return nil, err
	}
	if now == nil {
		now = time.Now
	}
	return &recordingReplayer{inner: inner, idx: newRingIndex(capacity), now: now}, nil
}

// Put records the event in the index and forwards to the inner replayer.
// The returned message is whatever the inner replayer returns (an
// ID-stamped copy when autoIDs=true). We index using the returned
// message's ID so the auto-generated value matches what subscribers
// will see and pass back via Last-Event-ID.
func (r *recordingReplayer) Put(msg *sse.Message, topics []string) (*sse.Message, error) {
	out, err := r.inner.Put(msg, topics)
	if err != nil {
		return nil, err
	}
	if out != nil {
		r.idx.put(out.ID.String(), r.now())
	}
	return out, nil
}

// Replay delegates to the inner FiniteReplayer.
func (r *recordingReplayer) Replay(sub sse.Subscription) error {
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
	if len(r.idx.entries) == 0 {
		return ""
	}
	return r.idx.entries[0].id
}

// IDBefore is the timestamp→ID lookup the hub uses to translate
// ?from_timestamp into a Last-Event-ID. See ringIndex.idBefore for the
// semantics.
func (r *recordingReplayer) IDBefore(t time.Time) (string, bool) {
	return r.idx.idBefore(t)
}
