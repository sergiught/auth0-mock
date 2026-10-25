package admin0_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/sergiught/auth0-mock/internal/admin0"
	"github.com/sergiught/auth0-mock/internal/journal"
	"github.com/sergiught/auth0-mock/internal/matches"
)

// seedJournal records one matched GET /api/v2/widgets/a call and one
// no_match POST /api/v2/widgets call through the real journal middleware.
func seedJournal(t *testing.T, j *journal.Journal) {
	t.Helper()
	h := j.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			journal.MarkNoMatch(r.Context(), nil)
		}
	}))
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/v2/widgets/a"},
		{http.MethodPost, "/api/v2/widgets"},
	} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(c.method, c.path, nil))
	}
}

func newJournalRouter(j *journal.Journal) (chi.Router, *matches.Store) {
	store := matches.NewStore()
	r := chi.NewRouter()
	admin0.Mount(r, admin0.Deps{Matches: store, Journal: j})
	return r, store
}

func TestJournal_EmptyIsWrapperNotNull(t *testing.T) {
	r, _ := newJournalRouter(journal.New(10))
	for _, p := range []string{"/admin0/journal", "/admin0/journal/unmatched"} {
		rec := do(t, r, http.MethodGet, p, "")
		require.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"entries":[]}`, rec.Body.String())
	}
}

func TestJournal_DisabledReturnsEmptyWrapper(t *testing.T) {
	r, _ := newJournalRouter(journal.New(0))
	rec := do(t, r, http.MethodGet, "/admin0/journal", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"entries":[]}`, rec.Body.String())
}

func TestJournal_ListAppliesFilters(t *testing.T) {
	j := journal.New(10)
	seedJournal(t, j)
	r, _ := newJournalRouter(j)

	all := do(t, r, http.MethodGet, "/admin0/journal", "")
	assert.Equal(t, int64(2), gjson.Get(all.Body.String(), "entries.#").Int())

	byMethod := do(t, r, http.MethodGet, "/admin0/journal?method=post", "")
	assert.Equal(t, int64(1), gjson.Get(byMethod.Body.String(), "entries.#").Int())
	assert.Equal(t, "POST", gjson.Get(byMethod.Body.String(), "entries.0.method").String())

	byPath := do(t, r, http.MethodGet, "/admin0/journal?path=/api/v2/widgets/a", "")
	assert.Equal(t, int64(1), gjson.Get(byPath.Body.String(), "entries.#").Int())
	assert.Equal(t, "GET", gjson.Get(byPath.Body.String(), "entries.0.method").String())
}

func TestJournal_UnmatchedFiltersByOutcome(t *testing.T) {
	j := journal.New(10)
	seedJournal(t, j)
	r, _ := newJournalRouter(j)

	rec := do(t, r, http.MethodGet, "/admin0/journal/unmatched", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, int64(1), gjson.Get(rec.Body.String(), "entries.#").Int())
	assert.Equal(t, "no_match", gjson.Get(rec.Body.String(), "entries.0.outcome").String())

	none := do(t, r, http.MethodGet, "/admin0/journal/unmatched?method=GET", "")
	assert.JSONEq(t, `{"entries":[]}`, none.Body.String())
}

func TestJournal_DeleteClears(t *testing.T) {
	j := journal.New(10)
	seedJournal(t, j)
	r, _ := newJournalRouter(j)

	rec := do(t, r, http.MethodDelete, "/admin0/journal", "")
	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, j.List(journal.Filter{}))
}

func TestReset_ClearsJournal(t *testing.T) {
	j := journal.New(10)
	seedJournal(t, j)
	r, _ := newJournalRouter(j)

	rec := do(t, r, http.MethodPost, "/admin0/reset", "")
	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, j.List(journal.Filter{}))
}

func TestDeleteExpectations_LeavesJournalIntact(t *testing.T) {
	j := journal.New(10)
	seedJournal(t, j)
	r, _ := newJournalRouter(j)

	rec := do(t, r, http.MethodDelete, "/admin0/expectations", "")
	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Len(t, j.List(journal.Filter{}), 2)
}
