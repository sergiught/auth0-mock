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

// newRingIndex builds a buffer of the given capacity, floored at 1.
// The constructor already rejects anything smaller, so the floor is
// belt-and-braces — but the failure it prevents is not local: a cap of 0
// makes the first put take the eviction branch and slice entries[1:] out
// of range, and go-sse answers a panic from a Replayer by setting the
// replayer to nil, which disables replay for every subscriber on the
// server rather than failing the one call.
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
//
// Stops at the first entry that doesn't predate t, which selects the
// longest strictly-predating PREFIX rather than the latest predating
// entry anywhere in the buffer. That distinction only shows up when
// timestamps aren't monotonic — the mock's clock is controllable and
// /admin0/clock/advance takes negative durations — and the prefix is
// the safe choice. A cursor names a suffix by position, so when
// position order and time order disagree no cursor can express
// "exactly the events at or after t"; stopping at the first entry that
// doesn't predate t guarantees the suffix still contains every such
// event. Scanning past it would pick a later cursor and silently drop
// the ones in between, which is the failure a resume exists to prevent.
// Re-sending an older event instead is benign; SSE consumers already
// tolerate replays.
//
// Only first copies are eligible. The answer is handed back as an id,
// and a resume resolves an id to its first copy — so offering the
// trailing half of a duplicated offset would send the subscriber back to
// the earlier one and replay events older than the instant it asked for.
func (r *ringIndex) idBefore(t time.Time) (string, bool) {
	var (
		bestID string
		found  bool
	)
	// Tracks ids already visited rather than calling firstIndex per
	// entry: that would rescan the prefix every step, making this O(n^2)
	// under the read lock for what one pass answers.
	seen := make(map[string]struct{}, len(r.entries))
	for _, e := range r.entries {
		if !e.at.Before(t) {
			break
		}
		if _, dup := seen[e.id]; dup {
			continue
		}
		seen[e.id] = struct{}{}
		bestID = e.id
		found = true
	}
	return bestID, found
}

// has reports whether id is currently in the buffer.
func (r *ringIndex) has(id string) bool {
	return r.firstIndex(id) >= 0
}

// hasMsg reports whether this exact message is still buffered.
//
// Identity rather than id, because the two can disagree: nothing
// upstream enforces unique offsets, and expireAll really removes
// entries so an offset may be reused afterwards. An id lookup then
// answers "still buffered" about a DIFFERENT entry that happens to
// share the offset, and the replay re-check would put an aged-out
// message on the wire the expire endpoint has already counted as
// dropped.
//
// What makes a dropped message unreachable is the length of the slice,
// not dropFront's clear — that exists to stop dropped entries pinning
// messages, and put's eviction re-slices without clearing at all. So
// this must scan entries, never cap(entries).
func (r *ringIndex) hasMsg(m *sse.Message) bool {
	return slices.ContainsFunc(r.entries, func(e indexEntry) bool { return e.msg == m })
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
	// Ok=false for the newest entry too, not just an unknown id: go-sse's
	// findIDInQueue reports not-found when the match is the queue tail, so
	// its Replay returns without touching the client. Matching that keeps
	// a resume-from-newest from flushing a subscriber it sent nothing to.
	if i < 0 || i == len(r.entries)-1 {
		return nil, false
	}
	return collect(r.entries[i+1:], topics), true
}

// all returns every buffered message whose topics intersect the
// subscription's, oldest first. Unlike after there is no cursor to
// start from: this answers "?from_timestamp predates the whole
// buffer", where the caller asked for everything and the oldest entry
// is part of that answer rather than the boundary excluded from it.
//
// It replays the buffer rather than filtering on the requested instant,
// which only diverges when timestamps aren't monotonic — the mock's
// clock is controllable and /admin0/clock/advance takes negative
// durations. IdBefore already reasons about that case and lands on the
// same rule: over-delivering an older event is benign, since SSE
// consumers tolerate replays, while dropping one is the failure a
// resume exists to prevent.
//
// Returns a snapshot for the same reason after does — see there.
func (r *ringIndex) all(topics []string) []*sse.Message {
	return collect(r.entries, topics)
}

// collect returns the messages among entries that the subscription's
// topics can see, in order. Shared by after and all so a change to what
// replay filtering means cannot land on the ?from path and miss the
// ?from_timestamp one.
func collect(entries []indexEntry, topics []string) []*sse.Message {
	out := make([]*sse.Message, 0, len(entries))
	for _, e := range entries {
		if topicsIntersect(topics, e.topics) {
			out = append(out, e.msg)
		}
	}
	return out
}

// expireAll drops every buffered cursor and reports how many it
// dropped. Kept separate from expireBefore rather than folded into one
// method with an empty-string sentinel: an id threaded through from a
// variable that happens to be empty must not be able to destroy the
// buffer and report it as a success.
//
// Repeat calls are idempotent: 0 means the buffer was already empty.
//
// The entries are really removed, messages included, so an id may be
// reused afterwards: a test that expires the buffer and then renumbers
// its offsets from zero gets a clean buffer, not a shadowed one.
func (r *ringIndex) expireAll() int {
	return r.dropFront(len(r.entries))
}

// expireBefore drops everything older than before, keeping before itself
// resumable. It reports how many it dropped and whether the buffer held
// before at all. Both are needed: dropping 0 means either that nothing
// was older than the cursor or that the cursor was never buffered, and
// only found tells those apart.
//
// Before is resolved to its first copy, the same entry a resume from
// that cursor would start at. If an offset is duplicated inside the
// buffer, "older than before" therefore means older than the FIRST
// copy, and expiring by that offset trims less than a caller holding
// the later copy expects. Resolving to the last copy instead would put
// expiry and resume back in disagreement, which is the class of bug
// this buffer is shaped to avoid; push unique offsets.
//
// An id the buffer doesn't hold drops nothing, so there is no value
// that turns this into expire-everything by accident. The empty string
// is refused outright rather than left to firstIndex: sse.ID("") is a
// SET event id, so an entry could in principle be buffered under it,
// and the whole point of splitting expireAll out is that no threaded-
// through empty value can ever mean "expire everything".
func (r *ringIndex) expireBefore(before string) (dropped int, found bool) {
	if before == "" {
		return 0, false
	}
	i := r.firstIndex(before)
	if i < 0 {
		return 0, false
	}
	return r.dropFront(i), true
}

// dropFront is the only way entries leave the buffer other than put's
// eviction, and both go from the FRONT. Replay depends on that: send
// skips an entry the buffer no longer holds and keeps going, which is
// only safe because the entries it skips are a prefix of its snapshot.
// An expiry that could remove from the middle would need send to
// answer for the interior hole it left.
//
// DropFront removes the first n entries and reports how many went. A
// negative n (an id that isn't buffered) or zero drops nothing, and n
// is clamped to the buffer length: slicing past the end would panic,
// and go-sse answers a panic from a Replayer by disabling replay for
// every subscriber on the server — the same reason newRingIndex keeps
// its floor.
func (r *ringIndex) dropFront(drop int) int {
	if drop <= 0 {
		return 0
	}
	if drop > len(r.entries) {
		drop = len(r.entries)
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
// but Hub.Handler reads Has / IDBefore from arbitrary request
// goroutines and Expire writes from another. The mutex makes those
// safe, and is never held across a write to a subscriber.
type recordingReplayer struct {
	mu  sync.RWMutex
	idx *ringIndex
	now func() time.Time
}

// newRecordingReplayer constructs a recordingReplayer of the given
// capacity. Now defaults to time.Now when nil.
func newRecordingReplayer(capacity int, now func() time.Time) (*recordingReplayer, error) {
	// One is a legitimate buffer size: a caller setting
	// EVENTS_REPLAY_BUFFER=1 wants exactly one event retained, and
	// silently giving them two would make a resume succeed where they
	// expected 410. The old floor of 2 was sse.FiniteReplayer's
	// constraint, and that dependency is gone.
	if capacity < 1 {
		return nil, errors.New("events: replay buffer capacity must be at least 1")
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
	// Topics first, per the sse.Replayer contract. Unreachable through
	// sse.Joe, which rejects an empty topic set before consulting the
	// replayer — kept because this type implements a published interface
	// and answering ErrNoTopic only for id-carrying messages would make
	// the deviation silent to anyone who does drive it directly.
	if len(topics) == 0 {
		return nil, sse.ErrNoTopic
	}
	if !msg.ID.IsSet() {
		return msg, nil
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
// nothing instead of erroring. That is the only case this suppresses: a
// Send or Flush that fails partway through a replay still returns its
// error, exactly as the FiniteReplayer this replaced did, because at
// that point the subscriber's connection is already broken.
//
// A subscriber whose cursor is expired between the gate and here
// therefore joins live with nothing replayed, rather than getting a
// 410: by this point the handler has already committed a 200 and
// flushed the SSE headers. That window is inherent to checking before
// committing a response, and it is the same outcome the eviction race
// produces.
func (r *recordingReplayer) Replay(sub sse.Subscription) error {
	// ReplayAllTopic means the subscriber asked for the whole buffer
	// rather than a suffix of it — a `?from_timestamp` that predates
	// every buffered event. It carries no cursor, so this is checked
	// before the LastEventID gate below. See replayAllTopic.
	// The cursor check is belt-and-braces: promoteResumeHint never sets
	// both, since the marker exists precisely for the case no cursor can
	// express. It is kept because recordingReplayer implements a
	// published interface — the same reason Put answers ErrNoTopic on a
	// path sse.Joe cannot reach — and because silently replaying the
	// whole buffer to a subscriber that named a position would also slip
	// past the handler's 410 gate.
	if slices.Contains(sub.Topics, replayAllTopic) && !sub.LastEventID.IsSet() {
		r.mu.RLock()
		msgs := r.idx.all(sub.Topics)
		r.mu.RUnlock()
		return r.send(sub, msgs)
	}
	if !sub.LastEventID.IsSet() {
		return nil
	}
	r.mu.RLock()
	msgs, ok := r.idx.after(sub.LastEventID.String(), sub.Topics)
	r.mu.RUnlock()
	if !ok {
		return nil
	}
	return r.send(sub, msgs)
}

// send writes a replay snapshot to the subscriber and flushes.
//
// Nothing to send means nothing to flush: a snapshot can be empty
// because the buffer is, or because the subscription's topics filtered
// every entry out, and in both cases flushing would touch a client this
// replay never wrote to. That is what go-sse's own FiniteReplayer does
// for a resume-from-newest, and why after reports ok=false for the tail.
func (r *recordingReplayer) send(sub sse.Subscription, msgs []*sse.Message) error {
	var sent int
	for _, m := range msgs {
		// Re-check before each write. The snapshot is taken under the
		// read lock and the writes happen without it — deliberately, so a
		// subscriber that stops reading cannot park an expiry — which
		// leaves a window where an expiry lands mid-replay. Without this
		// the endpoint would answer {"expired":N} while the events it
		// just aged out were still going out on the wire.
		//
		// Skip the aged-out entry rather than stopping: the ring only
		// ever evicts from the front, so a missing id means "this one
		// aged out", never "everything behind it did". Breaking here
		// dropped the survivors too, and a whole-buffer replay starts at
		// index 0 — exactly what an expiry removes first — so a
		// concurrent expire emptied the stream instead of trimming it.
		if !r.holds(m) {
			continue
		}
		if err := sub.Client.Send(m); err != nil {
			return err
		}
		sent++
	}
	if sent == 0 {
		return nil
	}
	return sub.Client.Flush()
}

// IDBefore is the timestamp→ID lookup the hub uses to translate
// ?from_timestamp into a Last-Event-ID. See ringIndex.idBefore for the
// semantics.
func (r *recordingReplayer) IDBefore(t time.Time) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.idx.idBefore(t)
}

// holds reports whether this exact message is still buffered. Used by
// the mid-replay re-check; see ringIndex.hasMsg for why the replay path
// asks about identity where the 410 gate asks about an id.
func (r *recordingReplayer) holds(m *sse.Message) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.idx.hasMsg(m)
}

// Has reports whether id is currently in the buffer. Used by the hub to
// surface 410 Gone for aged-out ?from values up-front, rather than
// waiting for Replay to do the same lookup.
func (r *recordingReplayer) Has(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.idx.has(id)
}

// ExpireAll ages out every buffered cursor and reports how many it
// dropped. Backs POST /admin0/events/expire so a test can provoke the
// 410 aged-out path without pushing past the buffer's capacity or
// resetting the whole mock.
func (r *recordingReplayer) ExpireAll() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.idx.expireAll()
}

// ExpireBefore ages out cursors older than before, keeping before itself
// resumable. It reports how many it dropped and whether before was in
// the buffer to begin with. See ringIndex.expireBefore.
func (r *recordingReplayer) ExpireBefore(before string) (dropped int, found bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.idx.expireBefore(before)
}
