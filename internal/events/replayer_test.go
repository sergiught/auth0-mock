package events

import (
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
	require.Equal(t, "a", r.OldestID())

	dropped, found := r.ExpireBefore("c")
	assert.Equal(t, 2, dropped)
	assert.True(t, found)
	assert.False(t, r.Has("a"), "an expired cursor is what the 410 gate keys on")
	assert.True(t, r.Has("c"))
	assert.Equal(t, "c", r.OldestID(), "?from_timestamp now resolves to the surviving window")

	assert.Equal(t, 1, r.ExpireAll())
	assert.Empty(t, r.OldestID(), "an expired buffer has no oldest cursor to fall back to")
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
	assert.Equal(t, "0", r.OldestID())

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

// An expiry that lands mid-replay must stop the rest of the buffer
// going out: otherwise the endpoint answers {"expired":N} while the
// events it just aged out are still being written to a subscriber.
func TestRecordingReplayer_Replay_StopsWhenExpiredMidFlight(t *testing.T) {
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
		"only the write already in flight goes out; the rest were expired")
}

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
