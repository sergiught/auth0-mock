package events

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmaxmax/go-sse"
)

func TestRingIndex_PutAndIDBefore(t *testing.T) {
	idx := newRingIndex(3)
	base := time.Unix(1_700_000_000, 0).UTC()
	idx.put("a", base, nil, nil)
	idx.put("b", base.Add(10*time.Second), nil, nil)
	idx.put("c", base.Add(20*time.Second), nil, nil)

	// Strictly-less semantics: query exactly at b's timestamp returns
	// a (the latest event strictly before b).
	got, ok := idx.idBefore(base.Add(10 * time.Second))
	require.True(t, ok)
	assert.Equal(t, "a", got)

	got, ok = idx.idBefore(base.Add(15 * time.Second))
	require.True(t, ok)
	assert.Equal(t, "b", got)
}

func TestRingIndex_IDBefore_NothingPredates(t *testing.T) {
	idx := newRingIndex(3)
	base := time.Unix(1_700_000_000, 0).UTC()
	idx.put("a", base.Add(10*time.Second), nil, nil)

	_, ok := idx.idBefore(base)
	assert.False(t, ok, "no stored event predates t; caller should drop the hint")
}

func TestRingIndex_IDBefore_AfterAll(t *testing.T) {
	idx := newRingIndex(3)
	base := time.Unix(1_700_000_000, 0).UTC()
	idx.put("a", base, nil, nil)
	idx.put("b", base.Add(10*time.Second), nil, nil)

	got, ok := idx.idBefore(base.Add(time.Hour))
	require.True(t, ok)
	assert.Equal(t, "b", got, "t after every stored event returns the newest")
}

func TestRingIndex_EvictsOldest(t *testing.T) {
	idx := newRingIndex(2)
	base := time.Unix(1_700_000_000, 0).UTC()
	idx.put("a", base, nil, nil)
	idx.put("b", base.Add(10*time.Second), nil, nil)
	idx.put("c", base.Add(20*time.Second), nil, nil) // Evicts "a".

	// "a" is gone, so a query that would have matched "a" now matches
	// nothing strictly before "b" — either returns "b" if t is after
	// "b", or nothing.
	_, ok := idx.idBefore(base.Add(5 * time.Second))
	assert.False(t, ok)

	got, ok := idx.idBefore(base.Add(15 * time.Second))
	require.True(t, ok)
	assert.Equal(t, "b", got)
}

// newTestMessage builds a minimal sse.Message with optional id.
func newTestMessage(t *testing.T, id string) *sse.Message {
	t.Helper()
	m := &sse.Message{}
	if id != "" {
		m.ID = sse.ID(id)
	}
	m.Type = sse.Type("test.event")
	m.AppendData(`{"hello":"world"}`)
	return m
}

func TestRecordingReplayer_PutIndexesAndForwards(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UTC()
	calls := 0
	now := func() time.Time {
		ts := base.Add(time.Duration(calls) * 10 * time.Second)
		calls++
		return ts
	}
	r, err := newRecordingReplayer(3, now)
	require.NoError(t, err)

	// Put three messages with explicit IDs; the buffer never generates
	// them, because the /admin0/events handler enforces CloudEvent's
	// `id` requirement upstream.
	for _, id := range []string{"a", "b", "c"} {
		out, err := r.Put(newTestMessage(t, id), []string{"t1"})
		require.NoError(t, err)
		require.NotNil(t, out)
	}

	// Lookup at 15s: only "a" (t=0) and "b" (t=10) are strictly
	// before; latest of those is "b".
	got, ok := r.IDBefore(base.Add(15 * time.Second))
	require.True(t, ok)
	assert.Equal(t, "b", got)
}

func TestRingIndex_Expire_All(t *testing.T) {
	idx := newRingIndex(3)
	base := time.Unix(1_700_000_000, 0).UTC()
	idx.put("a", base, nil, nil)
	idx.put("b", base.Add(10*time.Second), nil, nil)
	idx.put("c", base.Add(20*time.Second), nil, nil)

	assert.Equal(t, 3, idx.expireAll(), "empty cursor expires the whole index")
	for _, id := range []string{"a", "b", "c"} {
		assert.Falsef(t, idx.has(id), "%q should be aged out", id)
	}
	_, ok := idx.idBefore(base.Add(time.Hour))
	assert.False(t, ok, "an empty index has no timestamp to resolve")
}

func TestRingIndex_Expire_Before(t *testing.T) {
	idx := newRingIndex(4)
	base := time.Unix(1_700_000_000, 0).UTC()
	for i, id := range []string{"a", "b", "c", "d"} {
		idx.put(id, base.Add(time.Duration(i)*10*time.Second), nil, nil)
	}

	dropped, found := idx.expireBefore("c")
	assert.Equal(t, 2, dropped, "everything older than c is dropped")
	assert.True(t, found)
	assert.False(t, idx.has("a"))
	assert.False(t, idx.has("b"))
	assert.True(t, idx.has("c"), "the boundary cursor itself stays resumable")
	assert.True(t, idx.has("d"))
}

func TestRingIndex_Expire_UnknownCursorIsNoOp(t *testing.T) {
	idx := newRingIndex(3)
	base := time.Unix(1_700_000_000, 0).UTC()
	idx.put("a", base, nil, nil)
	idx.put("b", base.Add(10*time.Second), nil, nil)

	dropped, found := idx.expireBefore("nope")
	assert.Equal(t, 0, dropped)
	assert.False(t, found, "an unknown cursor is reported as not found, not as a 0-drop success")
	assert.True(t, idx.has("a"))
	assert.True(t, idx.has("b"))

	dropped, found = idx.expireBefore("")
	assert.Equal(t, 0, dropped)
	assert.False(t, found, "the empty cursor is refused outright, never treated as expire-everything")
	assert.True(t, idx.has("a"))
}

func TestRingIndex_Expire_Idempotent(t *testing.T) {
	idx := newRingIndex(3)
	base := time.Unix(1_700_000_000, 0).UTC()
	for i, id := range []string{"a", "b", "c"} {
		idx.put(id, base.Add(time.Duration(i)*10*time.Second), nil, nil)
	}

	dropped, found := idx.expireBefore("c")
	assert.Equal(t, 2, dropped)
	assert.True(t, found)

	dropped, found = idx.expireBefore("c")
	assert.Equal(t, 0, dropped, "re-expiring the same cursor drops nothing")
	assert.True(t, found, "the boundary survives its own expiry, so it is still found")

	assert.Equal(t, 0, newRingIndex(3).expireAll(), "expiring an empty index drops nothing")
}

// Expiry removes entries outright, so it frees slots as well as
// cursors; the buffer keeps its capacity and goes on evicting
// oldest-first from wherever expiry left it.
func TestRingIndex_Expire_KeepsCapacityAndEvicts(t *testing.T) {
	idx := newRingIndex(3)
	base := time.Unix(1_700_000_000, 0).UTC()
	for i, id := range []string{"a", "b", "c"} {
		idx.put(id, base.Add(time.Duration(i)*10*time.Second), nil, nil)
	}
	require.Equal(t, 3, idx.expireAll())

	for i, id := range []string{"d", "e", "f", "g"} {
		idx.put(id, base.Add(time.Duration(30+i*10)*time.Second), nil, nil)
	}
	assert.False(t, idx.has("d"), "capacity is unchanged, so the oldest of four still evicts")
	assert.True(t, idx.has("e"))
	assert.True(t, idx.has("f"))
	assert.True(t, idx.has("g"))
}

func TestRecordingReplayer_Expire(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UTC()
	calls := 0
	now := func() time.Time {
		ts := base.Add(time.Duration(calls) * 10 * time.Second)
		calls++
		return ts
	}
	r, err := newRecordingReplayer(3, now)
	require.NoError(t, err)
	for _, id := range []string{"a", "b", "c"} {
		_, err := r.Put(newTestMessage(t, id), []string{"t1"})
		require.NoError(t, err)
	}
	require.Equal(t, []string{"a", "b", "c"}, replayAllIDs(t, r, "t1"))

	dropped, found := r.ExpireBefore("c")
	assert.Equal(t, 2, dropped)
	assert.True(t, found)
	assert.False(t, r.Has("a"), "an expired cursor is what the 410 gate keys on")
	assert.True(t, r.Has("c"))
	assert.Equal(t, []string{"c"}, replayAllIDs(t, r, "t1"),
		"?from_timestamp now replays only the surviving window")

	assert.Equal(t, 1, r.ExpireAll())
	assert.Empty(t, replayAllIDs(t, r, "t1"), "an expired buffer has nothing left to replay")
}

// captureWriter records every message a Replay call emits, and how many
// times it was flushed.
type captureWriter struct {
	sent    []string
	flushes int
}

func (c *captureWriter) Send(m *sse.Message) error {
	c.sent = append(c.sent, m.ID.String())
	return nil
}

func (c *captureWriter) Flush() error {
	c.flushes++
	return nil
}

// The gate in Hub.Handler runs before Replay, not atomically with it,
// so a subscribe racing a concurrent Expire arrives here with a cursor
// that is no longer buffered. Replay has to answer that itself rather
// than trusting the gate.
func TestRecordingReplayer_Replay_RefusesExpiredCursor(t *testing.T) {
	r, err := newRecordingReplayer(5, nil)
	require.NoError(t, err)
	for _, id := range []string{"a", "b", "c"} {
		_, err := r.Put(newTestMessage(t, id), []string{"t1"})
		require.NoError(t, err)
	}
	require.Equal(t, 3, r.ExpireAll())

	w := &captureWriter{}
	require.NoError(t, r.Replay(sse.Subscription{
		Client:      w,
		LastEventID: sse.ID("a"),
		Topics:      []string{"t1"},
	}))
	assert.Empty(t, w.sent, "an expired cursor must not replay, even though the inner buffer still holds the messages")
}

// The guard must not disturb the ordinary resume path.
func TestRecordingReplayer_Replay_LiveCursorStillReplays(t *testing.T) {
	r, err := newRecordingReplayer(5, nil)
	require.NoError(t, err)
	for _, id := range []string{"a", "b", "c"} {
		_, err := r.Put(newTestMessage(t, id), []string{"t1"})
		require.NoError(t, err)
	}
	dropped, found := r.ExpireBefore("b")
	require.Equal(t, 1, dropped)
	require.True(t, found)

	w := &captureWriter{}
	require.NoError(t, r.Replay(sse.Subscription{
		Client:      w,
		LastEventID: sse.ID("b"),
		Topics:      []string{"t1"},
	}))
	assert.Equal(t, []string{"c"}, w.sent)
}

// Reusing an offset after an expire is ordinary test behaviour: push a
// few events, expire the buffer, start numbering from 0 again. Resuming
// from the reused id must see only what followed the new copy.
func TestRecordingReplayer_ReusedIDAfterExpire(t *testing.T) {
	r, err := newRecordingReplayer(10, nil)
	require.NoError(t, err)
	for _, id := range []string{"0", "1", "2"} {
		_, err := r.Put(newTestMessage(t, id), []string{"t1"})
		require.NoError(t, err)
	}
	require.Equal(t, 3, r.ExpireAll())

	// The test starts numbering again from 0.
	_, err = r.Put(newTestMessage(t, "0"), []string{"t1"})
	require.NoError(t, err)

	w := &captureWriter{}
	require.NoError(t, r.Replay(sse.Subscription{
		Client:      w,
		LastEventID: sse.ID("0"),
		Topics:      []string{"t1"},
	}))
	assert.NotContains(t, w.sent, "1", "expired event 1 must never be replayed")
	assert.NotContains(t, w.sent, "2", "expired event 2 must never be replayed")
}

// A duplicated offset spanning the expiry boundary: because the buffer
// is really truncated, the expired copy is gone and the surviving one
// is an ordinary cursor. Nothing older can be reached through it.
func TestRecordingReplayer_DuplicateIDSpanningExpiry(t *testing.T) {
	r, err := newRecordingReplayer(10, nil)
	require.NoError(t, err)
	for _, id := range []string{"a", "x", "b", "a", "c"} {
		_, err := r.Put(newTestMessage(t, id), []string{"t1"})
		require.NoError(t, err)
	}
	dropped, found := r.ExpireBefore("b")
	require.Equal(t, 2, dropped, "a and x are older than b")
	require.True(t, found)

	assert.False(t, r.Has("x"), "x was expired")
	assert.True(t, r.Has("a"), "a's surviving copy is newer than the boundary")

	w := &captureWriter{}
	require.NoError(t, r.Replay(sse.Subscription{
		Client:      w,
		LastEventID: sse.ID("a"),
		Topics:      []string{"t1"},
	}))
	assert.Equal(t, []string{"c"}, w.sent,
		"resume starts at the surviving copy; the expired prefix is unreachable")
}

// An expire really empties the buffer, so a test that renumbers its
// offsets from zero afterwards gets a clean slate. The mirror-based
// design failed here: the old copies lingered in a queue it could not
// truncate, and every reused id resolved to the expired copy, leaving
// the whole post-expire buffer unresumable.
func TestRecordingReplayer_ReusedIDsAfterExpireAreResumable(t *testing.T) {
	r, err := newRecordingReplayer(10, nil)
	require.NoError(t, err)
	for _, id := range []string{"0", "1", "2"} {
		_, err := r.Put(newTestMessage(t, id), []string{"t1"})
		require.NoError(t, err)
	}
	require.Equal(t, 3, r.ExpireAll())

	for _, id := range []string{"0", "1", "2"} {
		_, err := r.Put(newTestMessage(t, id), []string{"t1"})
		require.NoError(t, err)
	}

	assert.True(t, r.Has("1"), "an event pushed after the expire must be resumable")
	assert.Equal(t, []string{"0", "1", "2"}, replayAllIDs(t, r, "t1"))

	w := &captureWriter{}
	require.NoError(t, r.Replay(sse.Subscription{
		Client:      w,
		LastEventID: sse.ID("1"),
		Topics:      []string{"t1"},
	}))
	assert.Equal(t, []string{"2"}, w.sent)
}

// putOn publishes one message under the given topics, so topic-filtered
// replay can be exercised.
func putOn(t *testing.T, r *recordingReplayer, id string, topics ...string) {
	t.Helper()
	_, err := r.Put(newTestMessage(t, id), topics)
	require.NoError(t, err)
}

// replayAllIDs returns the ids a whole-buffer replay delivers to a
// subscription on the given topics — what ?from_timestamp asks for when
// it predates every buffered event.
func replayAllIDs(t *testing.T, r *recordingReplayer, topics ...string) []string {
	t.Helper()
	w := &captureWriter{}
	require.NoError(t, r.Replay(sse.Subscription{
		Client: w,
		// Concat rather than append: topics is the caller's variadic
		// slice, and appending into its spare capacity would overwrite
		// an element the caller still owns.
		Topics: slices.Concat(topics, []string{replayAllTopic}),
	}))
	return w.sent
}

// replayAllTopic carries no cursor, so it has to be answered before the
// LastEventID gate — and it includes the oldest entry, which is the one
// a resume-from-oldest could never deliver.
func TestRecordingReplayer_ReplayAll_IncludesOldestAndCarriesNoCursor(t *testing.T) {
	r, err := newRecordingReplayer(10, nil)
	require.NoError(t, err)
	for _, id := range []string{"0", "1", "2"} {
		putOn(t, r, id, "t1")
	}

	w := &captureWriter{}
	require.NoError(t, r.Replay(sse.Subscription{
		Client: w,
		Topics: []string{"t1", replayAllTopic},
	}))
	assert.Equal(t, []string{"0", "1", "2"}, w.sent)
	assert.Equal(t, 1, w.flushes)
}

// A single buffered event is the case that used to deliver nothing at
// all: it is both oldest and newest, and ringIndex.after reports
// not-found for the tail entry.
func TestRecordingReplayer_ReplayAll_SingleEntryBuffer(t *testing.T) {
	r, err := newRecordingReplayer(1, nil)
	require.NoError(t, err)
	putOn(t, r, "only", "t1")

	assert.Equal(t, []string{"only"}, replayAllIDs(t, r, "t1"))
}

// Asking for the whole buffer must not widen the event_type filter: the
// marker rides the subscription's topic list, and topicsIntersect is
// what keeps it from matching messages it shouldn't.
func TestRecordingReplayer_ReplayAll_RespectsTopicFilter(t *testing.T) {
	r, err := newRecordingReplayer(10, nil)
	require.NoError(t, err)
	putOn(t, r, "0", "t1")
	putOn(t, r, "1", "t2")
	putOn(t, r, "2", "t1")

	assert.Equal(t, []string{"0", "2"}, replayAllIDs(t, r, "t1"))
}

// Nothing to send means nothing to flush, matching what the after path
// does for a resume-from-newest. A flush on a client we sent nothing to
// is a write go-sse's own replayers never make.
func TestRecordingReplayer_ReplayAll_EmptyBufferDoesNotFlush(t *testing.T) {
	r, err := newRecordingReplayer(10, nil)
	require.NoError(t, err)

	w := &captureWriter{}
	require.NoError(t, r.Replay(sse.Subscription{
		Client: w,
		Topics: []string{"t1", replayAllTopic},
	}))
	assert.Empty(t, w.sent)
	assert.Zero(t, w.flushes)
}

// Topic filtering on the replay path is this package's own code now
// (it used to be go-sse's), and a filtered subscriber resuming with
// ?from must not be handed events of other types.
func TestRecordingReplayer_Replay_RespectsTopicFilter(t *testing.T) {
	r, err := newRecordingReplayer(10, nil)
	require.NoError(t, err)
	putOn(t, r, "1", "user.created")
	putOn(t, r, "2", "user.deleted")
	putOn(t, r, "3", "user.created")

	w := &captureWriter{}
	require.NoError(t, r.Replay(sse.Subscription{
		Client:      w,
		LastEventID: sse.ID("1"),
		Topics:      []string{"user.created"},
	}))
	assert.Equal(t, []string{"3"}, w.sent, "the user.deleted event must not reach a filtered subscriber")
}

// A subscriber already at the newest cursor has nothing to replay. Match
// go-sse's FiniteReplayer, which returns without touching the client at
// all in that case: flushing anyway would surface a client-side flush
// error from a resume that sent nothing, and go-sse turns a Replay error
// into http.Error text inside the SSE body.
func TestRecordingReplayer_Replay_NewestCursorDoesNotTouchClient(t *testing.T) {
	r, err := newRecordingReplayer(10, nil)
	require.NoError(t, err)
	putOn(t, r, "1", "t1")
	putOn(t, r, "2", "t1")

	w := &captureWriter{}
	require.NoError(t, r.Replay(sse.Subscription{
		Client:      w,
		LastEventID: sse.ID("2"),
		Topics:      []string{"t1"},
	}))
	assert.Empty(t, w.sent)
	assert.Zero(t, w.flushes, "nothing was replayed, so the client should not be flushed")
}

// The topic filter can empty a replay just as thoroughly as a tail
// cursor can, and the rule is the same: a client we sent nothing to is
// a client we have no reason to flush.
//
// This path used to flush — after reports ok=true and the loop simply
// wrote nothing — and dropping that is deliberate, not inherited from
// the whole-buffer path. Nothing depended on it: serveHTTP has already
// flushed the headers and the :connected frame before delegating, and
// go-sse's own FiniteReplayer does not touch the client at all when it
// has nothing to replay.
func TestRecordingReplayer_Replay_EmptyAfterTopicFilterDoesNotFlush(t *testing.T) {
	r, err := newRecordingReplayer(10, nil)
	require.NoError(t, err)
	putOn(t, r, "1", "t1")
	putOn(t, r, "2", "t2")

	w := &captureWriter{}
	require.NoError(t, r.Replay(sse.Subscription{
		Client:      w,
		LastEventID: sse.ID("1"),
		Topics:      []string{"t1"},
	}))
	assert.Empty(t, w.sent)
	assert.Zero(t, w.flushes, "the topic filter emptied the replay, so there is nothing to flush")
}

// Before anything has been written there is no cursor to strand, so an
// aged-out prefix is skipped rather than ending the replay. Eviction is
// front-only, so that prefix is all the snapshot can have lost — and a
// whole-buffer replay starts at index 0, exactly what expiry removes
// first, so stopping there emptied the stream instead of trimming it.
func TestRecordingReplayer_Send_SkipsAnAgedOutPrefixBeforeWriting(t *testing.T) {
	r, err := newRecordingReplayer(10, nil)
	require.NoError(t, err)
	for _, id := range []string{"0", "1", "2", "3"} {
		putOn(t, r, id, "t1")
	}

	// Snapshot the way Replay does, then let an expiry land before any
	// of it reaches the client.
	msgs := r.idx.all([]string{"t1"})
	dropped, found := r.ExpireBefore("2")
	require.Equal(t, 2, dropped)
	require.True(t, found)

	w := &captureWriter{}
	require.NoError(t, r.send(sse.Subscription{Client: w, Topics: []string{"t1"}}, msgs))
	assert.Equal(t, []string{"2", "3"}, w.sent, "the survivors still go out")
}

// The mid-replay re-check has to identify the ENTRY, not the offset.
// Nothing upstream enforces unique offsets, and an offset may be reused
// after an expiry, so an id lookup answers "still buffered" for a
// different entry that happens to share it — putting an aged-out
// message on the wire the endpoint has already counted as expired.
func TestRecordingReplayer_Send_SkipsTheExpiredCopyOfADuplicatedOffset(t *testing.T) {
	r, err := newRecordingReplayer(10, nil)
	require.NoError(t, err)
	putOn(t, r, "A", "t1")
	putOn(t, r, "B", "t1")
	putOn(t, r, "C", "t1")
	putOn(t, r, "B", "t1")

	msgs := r.idx.all([]string{"t1"})
	dropped, found := r.ExpireBefore("C")
	require.Equal(t, 2, dropped, "A and the FIRST copy of B age out")
	require.True(t, found)

	w := &captureWriter{}
	require.NoError(t, r.send(sse.Subscription{Client: w, Topics: []string{"t1"}}, msgs))
	assert.Equal(t, []string{"C", "B"}, w.sent,
		"the expired first copy of B must not ride out on the surviving copy's offset")
}

// The same aged-out prefix on a CURSOR resume must stop the replay
// rather than skip past it. The consumer already holds a position, and
// everything older than the skipped entry is gone — that position
// included — so delivering the survivors would advance its cursor past
// the hole and turn the 410 its reconnect should get into a 200.
// Writing nothing leaves the stale cursor in place to be rejected.
func TestRecordingReplayer_Send_StopsOnAnAgedOutPrefixWhenACursorIsHeld(t *testing.T) {
	r, err := newRecordingReplayer(10, nil)
	require.NoError(t, err)
	for _, id := range []string{"1", "2", "3", "4", "5"} {
		putOn(t, r, id, "t1")
	}

	msgs, ok := r.idx.after("1", []string{"t1"})
	require.True(t, ok)
	dropped, found := r.ExpireBefore("4")
	require.Equal(t, 3, dropped, "the consumer's own cursor 1 goes with the prefix")
	require.True(t, found)

	w := &captureWriter{}
	require.NoError(t, r.send(sse.Subscription{
		Client:      w,
		LastEventID: sse.ID("1"),
		Topics:      []string{"t1"},
	}, msgs))
	assert.Empty(t, w.sent, "nothing goes out, so the reconnect still resolves cursor 1 to a 410")
	assert.Zero(t, w.flushes)
}

// An unset or unknown cursor likewise must not flush.
func TestRecordingReplayer_Replay_UnknownCursorDoesNotTouchClient(t *testing.T) {
	r, err := newRecordingReplayer(10, nil)
	require.NoError(t, err)
	putOn(t, r, "1", "t1")

	w := &captureWriter{}
	require.NoError(t, r.Replay(sse.Subscription{
		Client:      w,
		LastEventID: sse.ID("nope"),
		Topics:      []string{"t1"},
	}))
	assert.Zero(t, w.flushes)
}

// Put must answer ErrNoTopic before anything else, per the sse.Replayer
// contract — including for the id-less control frames this replayer
// otherwise passes straight through.
func TestRecordingReplayer_Put_NoTopicBeatsMissingID(t *testing.T) {
	r, err := newRecordingReplayer(10, nil)
	require.NoError(t, err)

	_, err = r.Put(&sse.Message{}, nil)
	assert.ErrorIs(t, err, sse.ErrNoTopic)
}

// idBefore hands its answer back as an id, and Replay resolves an id to
// its FIRST copy. With a reused offset the two disagreed: idBefore
// picked the newest copy, Replay then started from the oldest one and
// delivered events older than the requested instant.
func TestRingIndex_IDBefore_DuplicateResolvesConsistently(t *testing.T) {
	idx := newRingIndex(10)
	base := time.Unix(1_700_000_000, 0).UTC()
	idx.put("0", base, nil, nil)
	idx.put("1", base.Add(10*time.Second), nil, nil)
	idx.put("0", base.Add(20*time.Second), nil, nil)

	// Everything predates this, so the hint must be a cursor that
	// resolves to a position with nothing after it worth replaying.
	got, ok := idx.idBefore(base.Add(time.Hour))
	require.True(t, ok)
	assert.Equal(t, "1", got,
		"the trailing duplicate is not usable as a cursor: resuming from it would start at the first copy")
}

// The trailing copy of a duplicated offset has no cursor of its own —
// any id naming it resolves to the earlier copy — so a ?from_timestamp
// landing there cannot be expressed exactly, and one event may still be
// re-sent. What must not happen is a restart from the first copy, which
// replays the whole span between them.
func TestRecordingReplayer_IDBefore_DuplicateDoesNotRestartFromFirstCopy(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UTC()
	n := 0
	now := func() time.Time {
		n++
		return base.Add(time.Duration(n) * time.Second)
	}
	r, err := newRecordingReplayer(10, now)
	require.NoError(t, err)
	putOn(t, r, "0", "t1")
	putOn(t, r, "1", "t1")
	putOn(t, r, "0", "t1")

	id, ok := r.IDBefore(base.Add(time.Hour))
	require.True(t, ok)

	w := &captureWriter{}
	require.NoError(t, r.Replay(sse.Subscription{
		Client:      w,
		LastEventID: sse.ID(id),
		Topics:      []string{"t1"},
	}))
	assert.NotContains(t, w.sent, "1",
		"resuming must not restart from the first copy of the reused offset")
	assert.Equal(t, []string{"0"}, w.sent)
}

// The mock's clock is controllable and /admin0/clock/advance takes
// negative durations, so buffered timestamps need not be
// non-decreasing. Position order and time order then disagree, and no
// single cursor can express "exactly the events at or after t" — a
// cursor names a suffix by position. The rule that matters for a resume
// is never to LOSE an event at or after t; re-sending an older one is
// benign, since SSE consumers already tolerate replays.
func TestRingIndex_IDBefore_NonMonotonicNeverSkipsNewerEvents(t *testing.T) {
	idx := newRingIndex(10)
	base := time.Unix(1_700_000_000, 0).UTC()
	idx.put("A", base, nil, nil)
	idx.put("B", base.Add(600*time.Second), nil, nil)
	// Clock stepped backwards before C was published.
	idx.put("C", base.Add(120*time.Second), nil, nil)

	got, ok := idx.idBefore(base.Add(300 * time.Second))
	require.True(t, ok)
	assert.Equal(t, "A", got,
		"resuming after A still delivers B, which postdates the requested instant; "+
			"picking C would silently drop it")
}

// Once a replay has written something, an expiry that lands under it
// stops the rest. The consumer is then left holding a cursor that just
// aged out, so its next reconnect answers 410 event_aged_out and it
// learns it lost data. Carrying on to the survivors would advance its
// cursor past the hole instead, and the reconnect would answer 200 with
// the loss invisible for good.
func TestRecordingReplayer_Replay_ExpiryAfterAWriteStopsToPreserveThe410(t *testing.T) {
	r, err := newRecordingReplayer(10, nil)
	require.NoError(t, err)
	for _, id := range []string{"1", "2", "3", "4", "5"} {
		putOn(t, r, id, "t1")
	}

	// Once "2" is on the wire, age out everything before "4" — so "3"
	// is gone, and so is the "2" the consumer now holds as its cursor.
	w := &expireBeforeWriter{r: r, after: 1, before: "4"}
	require.NoError(t, r.Replay(sse.Subscription{
		Client:      w,
		LastEventID: sse.ID("1"),
		Topics:      []string{"t1"},
	}))
	assert.Equal(t, []string{"2"}, w.sent,
		"stops at the aged-out 3, leaving the consumer on cursor 2, which is gone too")
}

// A subscription that names a position gets the suffix it asked for,
// even if the whole-buffer marker is somehow also present.
// PromoteResumeHint never sets both, but recordingReplayer implements a
// published interface, and replaying everything to a caller that named
// a position would also slip past the handler's 410 gate.
func TestRecordingReplayer_ReplayAll_CursorWinsOverTheMarker(t *testing.T) {
	r, err := newRecordingReplayer(10, nil)
	require.NoError(t, err)
	for _, id := range []string{"0", "1", "2"} {
		putOn(t, r, id, "t1")
	}

	w := &captureWriter{}
	require.NoError(t, r.Replay(sse.Subscription{
		Client:      w,
		LastEventID: sse.ID("1"),
		Topics:      []string{"t1", replayAllTopic},
	}))
	assert.Equal(t, []string{"2"}, w.sent, "a named position wins over the marker")
}

// An expiry that lands mid-replay must keep the events it aged out off
// the wire: otherwise the endpoint answers {"expired":N} while those
// same events are still being written to a subscriber. Here the whole
// buffer goes, so nothing survives to send — see
// TestRecordingReplayer_Replay_ExpiryAfterAWriteStopsToPreserveThe410
// for why it stops rather than skipping on.
func TestRecordingReplayer_Replay_WithholdsEventsExpiredMidFlight(t *testing.T) {
	r, err := newRecordingReplayer(10, nil)
	require.NoError(t, err)
	for _, id := range []string{"1", "2", "3", "4"} {
		putOn(t, r, id, "t1")
	}

	// Expire from inside the first Send, i.e. between the snapshot and
	// the writes — the exact window the re-check covers.
	w := &expiringWriter{r: r, after: 1}
	require.NoError(t, r.Replay(sse.Subscription{
		Client:      w,
		LastEventID: sse.ID("1"),
		Topics:      []string{"t1"},
	}))
	assert.Equal(t, []string{"2"}, w.sent,
		"only the write already in flight goes out; every survivor behind it aged out")
}

// expireBeforeWriter ages out everything older than `before` once it has
// accepted `after` messages, simulating a concurrent
// POST /admin0/events/expire?before= that lands mid-replay.
type expireBeforeWriter struct {
	r      *recordingReplayer
	after  int
	before string
	sent   []string
}

func (e *expireBeforeWriter) Send(m *sse.Message) error {
	e.sent = append(e.sent, m.ID.String())
	if len(e.sent) == e.after {
		e.r.ExpireBefore(e.before)
	}
	return nil
}

func (e *expireBeforeWriter) Flush() error { return nil }

// expiringWriter expires the whole buffer once it has accepted `after`
// messages, simulating a concurrent POST /admin0/events/expire.
type expiringWriter struct {
	r     *recordingReplayer
	after int
	sent  []string
}

func (e *expiringWriter) Send(m *sse.Message) error {
	e.sent = append(e.sent, m.ID.String())
	if len(e.sent) == e.after {
		e.r.ExpireAll()
	}
	return nil
}

func (e *expiringWriter) Flush() error { return nil }
