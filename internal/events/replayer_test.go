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

func TestRecordingReplayer_PutIndexesAutoIDsAndForwards(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UTC()
	calls := 0
	now := func() time.Time {
		ts := base.Add(time.Duration(calls) * 10 * time.Second)
		calls++
		return ts
	}
	r, err := newRecordingReplayer(3, now)
	require.NoError(t, err)

	// Put three messages without IDs; FiniteReplayer with autoIDs=true
	// stamps each with a fresh ID and the recordingReplayer indexes it.
	for range 3 {
		out, err := r.Put(newTestMessage(t, ""), []string{"t1"})
		require.NoError(t, err)
		require.NotNil(t, out, "FiniteReplayer should return a message with an auto ID")
	}

	// Mid-window lookup returns one of the indexed IDs.
	got, ok := r.IDBefore(base.Add(25 * time.Second))
	require.True(t, ok)
	assert.NotEmpty(t, got)
}
