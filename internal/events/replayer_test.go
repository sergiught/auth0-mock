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
	idx.put("a", base)
	idx.put("b", base.Add(10*time.Second))
	idx.put("c", base.Add(20*time.Second))

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
	idx.put("a", base.Add(10*time.Second))

	_, ok := idx.idBefore(base)
	assert.False(t, ok, "no stored event predates t; caller should drop the hint")
}

func TestRingIndex_IDBefore_AfterAll(t *testing.T) {
	idx := newRingIndex(3)
	base := time.Unix(1_700_000_000, 0).UTC()
	idx.put("a", base)
	idx.put("b", base.Add(10*time.Second))

	got, ok := idx.idBefore(base.Add(time.Hour))
	require.True(t, ok)
	assert.Equal(t, "b", got, "t after every stored event returns the newest")
}

func TestRingIndex_EvictsOldest(t *testing.T) {
	idx := newRingIndex(2)
	base := time.Unix(1_700_000_000, 0).UTC()
	idx.put("a", base)
	idx.put("b", base.Add(10*time.Second))
	idx.put("c", base.Add(20*time.Second)) // Evicts "a".

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

	// Put three messages with explicit IDs; FiniteReplayer is configured
	// with autoIDs=false because the /admin0/events handler enforces
	// CloudEvent's `id` requirement upstream.
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
	idx.put("a", base)
	idx.put("b", base.Add(10*time.Second))
	idx.put("c", base.Add(20*time.Second))

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
		idx.put(id, base.Add(time.Duration(i)*10*time.Second))
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
	idx.put("a", base)
	idx.put("b", base.Add(10*time.Second))

	assert.Equal(t, 0, idx.expire("nope"))
	assert.True(t, idx.has("a"))
	assert.True(t, idx.has("b"))
}

func TestRingIndex_Expire_Idempotent(t *testing.T) {
	idx := newRingIndex(3)
	base := time.Unix(1_700_000_000, 0).UTC()
	for i, id := range []string{"a", "b", "c"} {
		idx.put(id, base.Add(time.Duration(i)*10*time.Second))
	}

	assert.Equal(t, 2, idx.expire("c"))
	assert.Equal(t, 0, idx.expire("c"), "re-expiring the same cursor drops nothing")
	assert.Equal(t, 0, newRingIndex(3).expire(""), "expiring an empty index drops nothing")
}

// After an expire the index holds fewer entries than the inner
// FiniteReplayer, but it must never name an id the inner buffer has
// already evicted: both append in the same order and evict
// oldest-first, so the index stays a suffix of the inner queue and the
// two re-converge once cap more events land.
func TestRingIndex_Expire_KeepsCapacityAndReconverges(t *testing.T) {
	idx := newRingIndex(3)
	base := time.Unix(1_700_000_000, 0).UTC()
	for i, id := range []string{"a", "b", "c"} {
		idx.put(id, base.Add(time.Duration(i)*10*time.Second))
	}
	require.Equal(t, 3, idx.expire(""))

	for i, id := range []string{"d", "e", "f", "g"} {
		idx.put(id, base.Add(time.Duration(30+i*10)*time.Second))
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
