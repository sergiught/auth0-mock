package journal

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func entry(id, method, path, op string, o Outcome) Entry {
	return Entry{ID: id, Method: method, Path: path, Operation: op, Outcome: o}
}

func ids(es []Entry) []string {
	out := []string{}
	for _, e := range es {
		out = append(out, e.ID)
	}
	return out
}

func TestJournal_EvictsOldest(t *testing.T) {
	j := New(2)
	for _, id := range []string{"1", "2", "3"} {
		j.add(entry(id, "GET", "/api/v2/x", "/api/v2/x", OutcomeMatched))
	}
	assert.Equal(t, []string{"2", "3"}, ids(j.List(Filter{})))
}

func TestJournal_SizeZeroDisabled(t *testing.T) {
	j := New(0)
	require.NotNil(t, j)
	j.add(entry("1", "GET", "/api/v2/x", "", OutcomeRejected))
	got := j.List(Filter{})
	assert.NotNil(t, got)
	assert.Empty(t, got)
	b, err := json.Marshal(got)
	require.NoError(t, err)
	assert.JSONEq(t, `[]`, string(b))
	assert.NotNil(t, j.Unmatched(Filter{}))
}

func TestJournal_FilterMethodCaseInsensitive(t *testing.T) {
	j := New(5)
	j.add(entry("1", "PATCH", "/api/v2/x", "", OutcomeMatched))
	j.add(entry("2", "GET", "/api/v2/x", "", OutcomeMatched))
	assert.Equal(t, []string{"1"}, ids(j.List(Filter{Method: "patch"})))
	assert.Equal(t, []string{"1"}, ids(j.List(Filter{Method: "PATCH"})))
}

func TestJournal_FilterConcreteVsTemplatePath(t *testing.T) {
	j := New(5)
	j.add(entry("1", "GET", "/api/v2/users/1", "/api/v2/users/{id}", OutcomeMatched))
	j.add(entry("2", "GET", "/api/v2/users/2", "/api/v2/users/{id}", OutcomeMatched))
	assert.Equal(t, []string{"1"}, ids(j.List(Filter{Path: "/api/v2/users/1"})))
	assert.Equal(t, []string{"1", "2"}, ids(j.List(Filter{Path: "/api/v2/users/{id}"})))
}

func TestJournal_UnmatchedOnlyNoMatch(t *testing.T) {
	j := New(5)
	j.add(entry("1", "GET", "/api/v2/x", "", OutcomeMatched))
	j.add(entry("2", "GET", "/api/v2/x", "", OutcomeNoMatch))
	j.add(entry("3", "GET", "/api/v2/x", "", OutcomeRejected))
	assert.Equal(t, []string{"2"}, ids(j.Unmatched(Filter{})))
}

func TestJournal_ConcurrentAddListClear(t *testing.T) {
	j := New(10)
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j.add(entry(fmt.Sprint(i), "GET", "/api/v2/x", "", OutcomeMatched))
			j.List(Filter{})
			j.Unmatched(Filter{})
			if i%10 == 0 {
				j.Clear()
			}
		}()
	}
	wg.Wait()
	assert.LessOrEqual(t, len(j.List(Filter{})), 10)
}
