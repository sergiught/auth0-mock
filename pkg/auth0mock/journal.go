package auth0mock

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// JournalClient reads and clears the mock's request journal. Reach it
// via Client.Journal. The journal records every /api/v2 request the
// mock served, including the ones no stub matched.
type JournalClient struct{ c *Client }

// JournalFilter narrows List and Unmatched. Empty fields match all.
type JournalFilter struct {
	Method string
	Path   string
}

// Difference is one way a request differed from a stub's matcher.
// Field is "path", "query.<key>", "headers.<Canonical-Key>", "body"
// or a dotted body path such as "body.blocked". Reason is "mismatch",
// "missing" or "invalid_json".
type Difference struct {
	Field  string          `json:"field"`
	Reason string          `json:"reason"`
	Want   json.RawMessage `json:"want,omitempty"`
	Got    json.RawMessage `json:"got,omitempty"`
}

// String renders the difference on one line, for example
// `body.blocked: want true, got false`.
func (d Difference) String() string {
	if d.Reason == "invalid_json" {
		return d.Field + ": request body is not valid JSON"
	}
	got := "nothing"
	if d.Reason != "missing" && len(d.Got) > 0 {
		got = string(d.Got)
	}
	return fmt.Sprintf("%s: want %s, got %s", d.Field, d.Want, got)
}

// NearMiss is a stub that almost matched a request.
type NearMiss struct {
	ExpectationID string       `json:"expectationId"`
	Method        string       `json:"method"`
	Path          string       `json:"path"`
	Differences   []Difference `json:"differences"`
}

// JournalEntry is one recorded request. Outcome is "matched",
// "no_match" or "rejected". Authorization and Cookie header values
// are redacted by the server.
type JournalEntry struct {
	ID            string          `json:"id"`
	ReceivedAt    time.Time       `json:"receivedAt"`
	Method        string          `json:"method"`
	Path          string          `json:"path"`
	Operation     string          `json:"operation"`
	Query         url.Values      `json:"query"`
	Headers       http.Header     `json:"headers"`
	Body          json.RawMessage `json:"body,omitempty"`
	BodyTruncated bool            `json:"bodyTruncated,omitempty"`
	Status        int             `json:"status"`
	Outcome       string          `json:"outcome"`
	ExpectationID string          `json:"expectationId,omitempty"`
	NearMisses    []NearMiss      `json:"nearMisses,omitempty"`
}

// String renders the entry on one line, for example
// `PATCH /api/v2/users/124 → 404 no_match at 13:04:05.123`. The time
// is shown in the location the server reported (its local zone).
func (e JournalEntry) String() string {
	return fmt.Sprintf("%s %s → %d %s at %s",
		e.Method, e.Path, e.Status, e.Outcome, e.ReceivedAt.Format("15:04:05.000"))
}

// List returns recorded requests, oldest first, optionally filtered
// by method and exact path.
func (j *JournalClient) List(ctx context.Context, f JournalFilter) ([]JournalEntry, error) {
	return j.fetch(ctx, "/admin0/journal", f)
}

// Unmatched returns only the requests that got no_match, each with
// its near misses.
func (j *JournalClient) Unmatched(ctx context.Context, f JournalFilter) ([]JournalEntry, error) {
	return j.fetch(ctx, "/admin0/journal/unmatched", f)
}

// Clear empties the journal.
func (j *JournalClient) Clear(ctx context.Context) error {
	return j.c.do(ctx, http.MethodDelete, "/admin0/journal", nil, nil)
}

func (j *JournalClient) fetch(ctx context.Context, path string, f JournalFilter) ([]JournalEntry, error) {
	q := url.Values{}
	if f.Method != "" {
		q.Set("method", f.Method)
	}
	if f.Path != "" {
		q.Set("path", f.Path)
	}
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var resp struct {
		Entries []JournalEntry `json:"entries"`
	}
	if err := j.c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Entries, nil
}
