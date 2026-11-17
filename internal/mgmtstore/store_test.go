package mgmtstore

import (
	"fmt"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fixedNow = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }

const fixedStamp = "2026-01-02T03:04:05.000Z"

var both = stamp{created: true, updated: true}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	e, _ := loadEngine(t)
	return e.NewStore(fixedNow)
}

func TestCreateGeneratesHexID(t *testing.T) {
	s := newTestStore(t)
	got, err := s.create("/organizations", "id", Object{"name": "a"}, both)
	require.NoError(t, err)
	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]{24}$`), got["id"])
	assert.Equal(t, fixedStamp, got["created_at"])
	assert.Equal(t, fixedStamp, got["updated_at"])
}

func TestCreateKeepsClientID(t *testing.T) {
	s := newTestStore(t)
	got, err := s.create("/organizations", "id", Object{"id": "mine"}, both)
	require.NoError(t, err)
	assert.Equal(t, "mine", got["id"])
	_, ok := s.get("/organizations", "mine")
	assert.True(t, ok)
}

func TestCreateDuplicateConflict(t *testing.T) {
	s := newTestStore(t)
	_, err := s.create("/organizations", "id", Object{"id": "x"}, both)
	require.NoError(t, err)
	_, err = s.create("/organizations", "id", Object{"id": "x"}, both)
	assert.ErrorIs(t, err, errConflict)
}

func TestCreateLimit(t *testing.T) {
	s := newTestStore(t)
	s.max = 2
	for _, id := range []string{"a", "b"} {
		_, err := s.create("/c", "id", Object{"id": id}, both)
		require.NoError(t, err)
	}
	_, err := s.create("/c", "id", Object{"id": "c"}, both)
	assert.ErrorIs(t, err, errLimit)
	assert.Len(t, s.list("/c"), 2)
}

func TestMergeShallowAndNullRemoves(t *testing.T) {
	s := newTestStore(t)
	later := time.Date(2026, 2, 2, 3, 4, 5, 0, time.UTC)
	_, err := s.create("/c", "id", Object{"id": "1", "a": 1, "b": Object{"x": 1}}, both)
	require.NoError(t, err)
	s.now = func() time.Time { return later }
	got, err := s.merge("/c", "1", "id", Object{"b": Object{"y": 2}, "a": nil, "id": "other", "created_at": "zzz"}, both)
	require.NoError(t, err)
	assert.Equal(t, Object{"id": "1", "b": Object{"y": 2}, "created_at": fixedStamp, "updated_at": "2026-02-02T03:04:05.000Z"}, got)
	_, err = s.merge("/c", "nope", "id", Object{}, both)
	assert.ErrorIs(t, err, errNotFound)
}

func TestReplaceKeepsIDAndCreatedAt(t *testing.T) {
	s := newTestStore(t)
	_, err := s.create("/c", "id", Object{"id": "1", "a": 1}, both)
	require.NoError(t, err)
	s.now = func() time.Time { return fixedNow().Add(time.Hour) }
	got, err := s.replace("/c", "1", "id", Object{"id": "zz", "b": 2, "created_at": "zzz"}, both)
	require.NoError(t, err)
	assert.Equal(t, Object{"id": "1", "b": 2, "created_at": fixedStamp, "updated_at": "2026-01-02T04:04:05.000Z"}, got)
	_, err = s.replace("/c", "nope", "id", Object{}, both)
	assert.ErrorIs(t, err, errNotFound)
}

func TestRemoveCascades(t *testing.T) {
	s := newTestStore(t)
	for _, o := range []string{"o1", "o2"} {
		_, err := s.create("/organizations", "id", Object{"id": o}, both)
		require.NoError(t, err)
		_, err = s.create("/organizations/"+o+"/invitations", "id", Object{"id": "i"}, both)
		require.NoError(t, err)
		s.replaceSettings("/organizations/"+o+"/cfg", Object{"k": 1})
	}
	assert.True(t, s.remove("/organizations", "o1"))
	assert.False(t, s.remove("/organizations", "o1"))
	assert.Empty(t, s.list("/organizations/o1/invitations"))
	assert.Len(t, s.list("/organizations/o2/invitations"), 1)
	assert.Equal(t, Object{"d": 1}, s.getSettings("/organizations/o1/cfg", Object{"d": 1}))
	assert.Equal(t, Object{"k": 1}, s.getSettings("/organizations/o2/cfg", Object{"d": 1}))
}

func TestDeepCopy(t *testing.T) {
	s := newTestStore(t)
	in := Object{"id": "1", "n": Object{"x": 1}, "l": []any{Object{"y": 1}}}
	out, err := s.create("/c", "id", in, both)
	require.NoError(t, err)
	in["n"].(Object)["x"] = 99
	out["n"].(Object)["x"] = 98
	out["l"].([]any)[0].(Object)["y"] = 97
	got, _ := s.get("/c", "1")
	assert.Equal(t, Object{"x": 1}, got["n"])
	assert.Equal(t, []any{Object{"y": 1}}, got["l"])
	got["n"].(Object)["x"] = 5
	again := s.list("/c")[0]
	assert.Equal(t, Object{"x": 1}, again["n"])
}

func TestConcurrentCreates(t *testing.T) {
	s := newTestStore(t)
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.create("/c", "id", Object{}, both)
			assert.NoError(t, err)
		}()
	}
	wg.Wait()
	ids := map[string]bool{}
	for _, o := range s.list("/c") {
		ids[fmt.Sprint(o["id"])] = true
	}
	assert.Len(t, ids, 50)
	assert.Len(t, s.list("/c"), 50)
}

func TestSettingsInitialMergeReplaceReset(t *testing.T) {
	s := newTestStore(t)
	initial := Object{"a": 1}
	got := s.getSettings("/p", initial)
	assert.Equal(t, initial, got)
	got["a"] = 2
	assert.Equal(t, 1, initial["a"])
	assert.Equal(t, Object{"a": 1}, s.getSettings("/p", initial))

	got = s.mergeSettings("/p", initial, Object{"b": 2})
	assert.Equal(t, Object{"a": 1, "b": 2}, got)
	got = s.mergeSettings("/p", initial, Object{"a": nil})
	assert.Equal(t, Object{"b": 2}, got)
	assert.Equal(t, Object{"c": 3}, s.replaceSettings("/p", Object{"c": 3}))
	s.resetSettings("/p")
	assert.Equal(t, Object{"a": 1}, s.getSettings("/p", initial))
	s.Clear()
}
