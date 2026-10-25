package journal

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/sergiught/auth0-mock/internal/matches"
)

const apiPrefix = "/api/v2/"

type slotKey struct{}

// slot carries the handler's verdict to the middleware.
type slot struct {
	mu            sync.Mutex
	outcome       Outcome
	expectationID string
	nearMisses    []matches.NearMiss
}

// MarkMatched records that a stub served the call. ExpectationID is empty
// for the SSE stream. It is a no-op when ctx has no journal slot.
func MarkMatched(ctx context.Context, expectationID string) {
	if s, ok := ctx.Value(slotKey{}).(*slot); ok {
		s.mu.Lock()
		s.outcome, s.expectationID = OutcomeMatched, expectationID
		s.mu.Unlock()
	}
}

// MarkNoMatch records that no stub matched, with the closest candidates. It
// is a no-op when ctx has no journal slot.
func MarkNoMatch(ctx context.Context, nearMisses []matches.NearMiss) {
	if s, ok := ctx.Value(slotKey{}).(*slot); ok {
		s.mu.Lock()
		s.outcome, s.nearMisses = OutcomeNoMatch, nearMisses
		s.mu.Unlock()
	}
}

// errReader returns the already-read bytes and then the original read error,
// so downstream handlers see the same failure as without the journal.
type errReader struct {
	r   *bytes.Reader
	err error
}

func (e *errReader) Read(p []byte) (int, error) {
	if e.r.Len() > 0 {
		return e.r.Read(p)
	}
	return 0, e.err
}

// Middleware records every /api/v2/ call once its response starts.
func (j *Journal) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(j.buf) == 0 || !strings.HasPrefix(r.URL.Path, apiPrefix) {
			next.ServeHTTP(w, r)
			return
		}
		received := time.Now()
		var body []byte
		if r.Body != nil {
			var err error
			body, err = io.ReadAll(r.Body)
			if err != nil {
				r.Body = io.NopCloser(&errReader{r: bytes.NewReader(body), err: err})
			} else {
				r.Body = io.NopCloser(bytes.NewReader(body))
			}
		}
		s := &slot{outcome: OutcomeRejected}
		r = r.WithContext(context.WithValue(r.Context(), slotKey{}, s))
		rec := &recorder{ResponseWriter: w, commit: func(status int) {
			j.add(buildEntry(r, received, body, status, s))
		}}
		next.ServeHTTP(rec, r)
		rec.once.Do(func() { rec.commit(http.StatusOK) })
	})
}

func buildEntry(r *http.Request, received time.Time, body []byte, status int, s *slot) Entry {
	op := ""
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if p := rc.RoutePattern(); strings.HasPrefix(p, apiPrefix) {
			op = p
		}
	}
	headers := r.Header.Clone()
	for name := range headers {
		if matches.IsRedactedHeader(name) {
			headers[name] = []string{matches.RedactedValue}
		}
	}
	e := Entry{
		ID:         uuid.NewString(),
		ReceivedAt: received,
		Method:     r.Method,
		Path:       r.URL.Path,
		Operation:  op,
		Query:      r.URL.Query(),
		Headers:    headers,
		Status:     status,
	}
	e.Body, e.BodyTruncated = encodeBody(body)
	s.mu.Lock()
	e.Outcome, e.ExpectationID, e.NearMisses = s.outcome, s.expectationID, s.nearMisses
	s.mu.Unlock()
	return e
}

// encodeBody stores valid JSON as-is and anything else (or anything cut at
// MaxStoredBody) as a JSON string.
func encodeBody(b []byte) (json.RawMessage, bool) {
	if len(b) == 0 {
		return nil, false
	}
	truncated := len(b) > MaxStoredBody
	if truncated {
		b = b[:MaxStoredBody]
	} else if json.Valid(b) {
		return json.RawMessage(bytes.Clone(b)), false
	}
	raw, _ := json.Marshal(string(b))
	return raw, truncated
}

// recorder commits the journal entry on the first header write, write or
// flush, so long-lived streams are visible while still open.
type recorder struct {
	http.ResponseWriter
	once   sync.Once
	commit func(status int)
}

func (r *recorder) WriteHeader(status int) {
	r.once.Do(func() { r.commit(status) })
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(b []byte) (int, error) {
	r.once.Do(func() { r.commit(http.StatusOK) })
	return r.ResponseWriter.Write(b)
}

func (r *recorder) Flush() {
	r.once.Do(func() { r.commit(http.StatusOK) })
	_ = http.NewResponseController(r.ResponseWriter).Flush()
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
