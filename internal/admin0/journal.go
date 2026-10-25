package admin0

import (
	"net/http"

	"github.com/go-chi/render"

	"github.com/sergiught/auth0-mock/internal/journal"
)

func journalFilter(r *http.Request) journal.Filter {
	q := r.URL.Query()
	return journal.Filter{Method: q.Get("method"), Path: q.Get("path")}
}

// ListJournalHandler returns every recorded /api/v2 call, oldest first,
// optionally narrowed by ?method= and ?path=.
type ListJournalHandler struct{ Journal *journal.Journal }

func (h *ListJournalHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, map[string][]journal.Entry{"entries": h.Journal.List(journalFilter(r))})
}

// ListUnmatchedJournalHandler returns only the calls no stub matched.
type ListUnmatchedJournalHandler struct{ Journal *journal.Journal }

func (h *ListUnmatchedJournalHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, map[string][]journal.Entry{"entries": h.Journal.Unmatched(journalFilter(r))})
}

// DeleteJournalHandler empties the journal.
type DeleteJournalHandler struct{ Journal *journal.Journal }

func (h *DeleteJournalHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	h.Journal.Clear()
	w.WriteHeader(http.StatusNoContent)
}
