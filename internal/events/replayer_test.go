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

	assert.Equal(t, 3, idx.expire(""), "empty cursor expires the whole index")
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

	assert.Equal(t, 2, idx.expire("c"), "everything older than c is dropped")
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

	assert.Equal(t, 0, idx.expire("nope"))
	assert.True(t, idx.has("a"))
	assert.True(t, idx.has("b"))
}

func TestRingIndex_Expire_Idempotent(t *testing.T) {
	idx := newRingIndex(3)
	base := time.Unix(1_700_000_000, 0).UTC()
	for i, id := range []string{"a", "b", "c"} {
		idx.put(id, base.Add(time.Duration(i)*10*time.Second), nil, nil)
	}

	assert.Equal(t, 2, idx.expire("c"))
	assert.Equal(t, 0, idx.expire("c"), "re-expiring the same cursor drops nothing")
	assert.Equal(t, 0, newRingIndex(3).expire(""), "expiring an empty index drops nothing")
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
	require.Equal(t, 3, idx.expire(""))

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

	assert.Equal(t, 2, r.Expire("c"))
	assert.False(t, r.Has("a"), "an expired cursor is what the 410 gate keys on")
	assert.True(t, r.Has("c"))
	assert.Equal(t, "c", r.OldestID(), "?from_timestamp now resolves to the surviving window")

	assert.Equal(t, 1, r.Expire(""))
	assert.Empty(t, r.OldestID(), "an expired buffer has no oldest cursor to fall back to")
}

// captureWriter records every message a Replay call emits.
type captureWriter struct{ sent []string }

func (c *captureWriter) Send(m *sse.Message) error {
	c.sent = append(c.sent, m.ID.String())
	return nil
}
func (c *captureWriter) Flush() error { return nil }

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
	require.Equal(t, 3, r.Expire(""))

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
	require.Equal(t, 1, r.Expire("b"))

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
	require.Equal(t, 3, r.Expire(""))

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
	require.Equal(t, 2, r.Expire("b"), "a and x are older than b")

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
	require.Equal(t, 3, r.Expire(""))

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
