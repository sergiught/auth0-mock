// Package journal records Management API calls in a bounded in-memory ring so
// tests can assert on what the application under test actually sent.
package journal

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sergiught/auth0-mock/internal/matches"
)

// Outcome says how the mock resolved a recorded call.
type Outcome string

const (
	// OutcomeMatched means a stub (or the SSE stream) served the call.
	OutcomeMatched Outcome = "matched"
	// OutcomeNoMatch means the call was well formed but no stub matched.
	OutcomeNoMatch Outcome = "no_match"
	// OutcomeRejected means the call never reached stub matching (auth,
	// validation, unknown route, ...).
	OutcomeRejected Outcome = "rejected"
)

// MaxStoredBody is the most request-body bytes kept per entry.
const MaxStoredBody = 64 << 10

// Entry is one recorded Management API call.
type Entry struct {
	ID            string             `json:"id"`
	ReceivedAt    time.Time          `json:"receivedAt"`
	Method        string             `json:"method"`
	Path          string             `json:"path"`
	Operation     string             `json:"operation"`
	Query         url.Values         `json:"query"`
	Headers       http.Header        `json:"headers"`
	Body          json.RawMessage    `json:"body,omitempty"`
	BodyTruncated bool               `json:"bodyTruncated,omitempty"`
	Status        int                `json:"status"`
	Outcome       Outcome            `json:"outcome"`
	ExpectationID string             `json:"expectationId,omitempty"`
	NearMisses    []matches.NearMiss `json:"nearMisses,omitempty"`
}

// Filter narrows List and Unmatched. Empty fields match everything. A Path
// containing "{...}" is compared with the entry's Operation, otherwise with
// its concrete Path.
type Filter struct{ Method, Path string }

func (f Filter) matches(e Entry) bool {
	if f.Method != "" && !strings.EqualFold(f.Method, e.Method) {
		return false
	}
	if f.Path == "" {
		return true
	}
	if matches.KindOf(f.Path) == matches.KindTemplate {
		return f.Path == e.Operation
	}
	return f.Path == e.Path
}

// Journal is a fixed-size ring of entries, safe for concurrent use.
type Journal struct {
	mu    sync.Mutex
	buf   []Entry
	start int
	n     int
}

// New returns a journal keeping the newest size entries. A size <= 0
// disables recording.
func New(size int) *Journal {
	if size < 0 {
		size = 0
	}
	return &Journal{buf: make([]Entry, size)}
}

func (j *Journal) add(e Entry) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if len(j.buf) == 0 {
		return
	}
	if j.n < len(j.buf) {
		j.buf[(j.start+j.n)%len(j.buf)] = e
		j.n++
		return
	}
	j.buf[j.start] = e
	j.start = (j.start + 1) % len(j.buf)
}

func (j *Journal) collect(f Filter, keep func(Entry) bool) []Entry {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := []Entry{}
	for i := range j.n {
		e := j.buf[(j.start+i)%len(j.buf)]
		if f.matches(e) && keep(e) {
			out = append(out, e)
		}
	}
	return out
}

// List returns matching entries, oldest first. It never returns nil.
func (j *Journal) List(f Filter) []Entry {
	return j.collect(f, func(Entry) bool { return true })
}

// Unmatched returns matching entries whose outcome is no_match.
func (j *Journal) Unmatched(f Filter) []Entry {
	return j.collect(f, func(e Entry) bool { return e.Outcome == OutcomeNoMatch })
}

// Clear drops every entry.
func (j *Journal) Clear() {
	j.mu.Lock()
	defer j.mu.Unlock()
	clear(j.buf)
	j.start, j.n = 0, 0
}
