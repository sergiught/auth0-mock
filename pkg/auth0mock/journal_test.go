package auth0mock_test

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/pkg/auth0mock"
)

const journalBody = `{"entries":[{"id":"j1","receivedAt":"2026-10-25T13:04:05.123Z","method":"PATCH",
"path":"/api/v2/users/123","operation":"/api/v2/users/{id}","query":{"a":["1","2"]},
"headers":{"X-Tenant":["acme"]},"body":{"blocked":true},"status":404,"outcome":"no_match",
"nearMisses":[{"expectationId":"e1","method":"PATCH","path":"/api/v2/users/{id}",
"differences":[{"field":"body.blocked","reason":"mismatch","want":true,"got":false}]}]}]}`

func TestJournal_List(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(journalBody))
	}

	got, err := c.Journal.List(t.Context(), auth0mock.JournalFilter{Method: "PATCH", Path: "/api/v2/users/123"})
	require.NoError(t, err)

	call := rec.last(t)
	assert.Equal(t, http.MethodGet, call.Method)
	assert.Equal(t, "/admin0/journal", call.Path)
	require.Len(t, got, 1)
	e := got[0]
	assert.Equal(t, "j1", e.ID)
	assert.Equal(t, []string{"1", "2"}, e.Query["a"])
	assert.Equal(t, "acme", e.Headers.Get("X-Tenant"))
	assert.JSONEq(t, `{"blocked":true}`, string(e.Body))
	assert.Equal(t, 404, e.Status)
	require.Len(t, e.NearMisses, 1)
	assert.Equal(t, "e1", e.NearMisses[0].ExpectationID)
	assert.Equal(t, "body.blocked", e.NearMisses[0].Differences[0].Field)
}

func TestJournal_ListQueryString(t *testing.T) {
	t.Parallel()
	var raw atomic.Value
	raw.Store("")
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, r *http.Request) {
		raw.Store(r.URL.RawQuery)
		_, _ = w.Write([]byte(`{"entries":[]}`))
	}

	_, err := c.Journal.List(t.Context(), auth0mock.JournalFilter{Method: "PATCH", Path: "/api/v2/users/123"})
	require.NoError(t, err)
	assert.Equal(t, "method=PATCH&path=%2Fapi%2Fv2%2Fusers%2F123", raw.Load())

	_, err = c.Journal.List(t.Context(), auth0mock.JournalFilter{})
	require.NoError(t, err)
	assert.Empty(t, raw.Load(), "empty filter must send no query params")
}

func TestJournal_Unmatched(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(journalBody))
	}

	got, err := c.Journal.Unmatched(t.Context(), auth0mock.JournalFilter{Method: "PATCH"})
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, "/admin0/journal/unmatched", rec.last(t).Path)
}

func TestJournal_Clear(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)

	require.NoError(t, c.Journal.Clear(t.Context()))
	call := rec.last(t)
	assert.Equal(t, http.MethodDelete, call.Method)
	assert.Equal(t, "/admin0/journal", call.Path)
}

func TestDifference_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		d    auth0mock.Difference
		want string
	}{
		{"mismatch", auth0mock.Difference{Field: "body.blocked", Reason: "mismatch", Want: []byte("true"), Got: []byte("false")}, "body.blocked: want true, got false"},
		{"missing", auth0mock.Difference{Field: "headers.X-Tenant", Reason: "missing", Want: []byte(`"acme"`)}, `headers.X-Tenant: want "acme", got nothing`},
		{"invalid_json", auth0mock.Difference{Field: "body", Reason: "invalid_json", Want: []byte(`{}`)}, "body: request body is not valid JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.d.String())
		})
	}
}

func TestJournalEntry_String(t *testing.T) {
	t.Parallel()
	e := auth0mock.JournalEntry{
		Method: "PATCH", Path: "/api/v2/users/124", Status: 404, Outcome: "no_match",
		ReceivedAt: time.Date(2026, 10, 25, 13, 4, 5, 123_000_000, time.UTC),
	}
	assert.Equal(t, "PATCH /api/v2/users/124 → 404 no_match at 13:04:05.123", e.String())
}
