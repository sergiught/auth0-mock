package journal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/matches"
)

func newRouter(j *Journal) *chi.Mux {
	r := chi.NewRouter()
	r.Use(j.Middleware)
	r.Get("/api/v2/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		MarkMatched(r.Context(), "exp-1")
		w.WriteHeader(http.StatusOK)
	})
	r.Get("/api/v2/nomatch", func(w http.ResponseWriter, r *http.Request) {
		MarkNoMatch(r.Context(), []matches.NearMiss{{ExpectationID: "e", Method: "GET", Path: "/api/v2/nomatch"}})
		w.WriteHeader(http.StatusNotFound)
	})
	r.Get("/api/v2/unmarked", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	r.Post("/api/v2/echo", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	})
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	return r
}

func do(h http.Handler, method, target string, body io.Reader, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, body)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestMiddleware_RecordsMatched(t *testing.T) {
	j := New(10)
	do(newRouter(j), "GET", "/api/v2/users/42?x=1", nil, nil)
	es := j.List(Filter{})
	require.Len(t, es, 1)
	e := es[0]
	assert.Equal(t, OutcomeMatched, e.Outcome)
	assert.Equal(t, "exp-1", e.ExpectationID)
	assert.Equal(t, "/api/v2/users/{id}", e.Operation)
	assert.Equal(t, "/api/v2/users/42", e.Path)
	assert.Equal(t, "GET", e.Method)
	assert.Equal(t, 200, e.Status)
	assert.Equal(t, "1", e.Query.Get("x"))
	assert.NotEmpty(t, e.ID)
	assert.False(t, e.ReceivedAt.IsZero())
}

func TestMiddleware_RecordsNoMatchWithNearMisses(t *testing.T) {
	j := New(10)
	do(newRouter(j), "GET", "/api/v2/nomatch", nil, nil)
	es := j.Unmatched(Filter{})
	require.Len(t, es, 1)
	assert.Equal(t, 404, es[0].Status)
	require.Len(t, es[0].NearMisses, 1)
	assert.Equal(t, "e", es[0].NearMisses[0].ExpectationID)
}

func TestMiddleware_UnmarkedIsRejected(t *testing.T) {
	j := New(10)
	do(newRouter(j), "GET", "/api/v2/unmarked", nil, nil)
	es := j.List(Filter{})
	require.Len(t, es, 1)
	assert.Equal(t, OutcomeRejected, es[0].Outcome)
	assert.Equal(t, 401, es[0].Status)
}

func TestMiddleware_UnknownRouteRejectedEmptyOperation(t *testing.T) {
	j := New(10)
	do(newRouter(j), "GET", "/api/v2/does-not-exist", nil, nil)
	es := j.List(Filter{})
	require.Len(t, es, 1)
	assert.Equal(t, OutcomeRejected, es[0].Outcome)
	assert.Equal(t, "", es[0].Operation)
}

func TestMiddleware_IgnoresNonAPIPaths(t *testing.T) {
	j := New(10)
	h := j.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	do(h, "GET", "/admin0/x", nil, nil)
	do(h, "POST", "/oauth/token", nil, nil)
	assert.Empty(t, j.List(Filter{}))
}

func TestMiddleware_RedactsHeaders(t *testing.T) {
	j := New(10)
	do(newRouter(j), "GET", "/api/v2/users/1", nil, map[string]string{
		"Authorization": "Bearer secret", "Cookie": "a=b", "X-Other": "ok",
	})
	e := j.List(Filter{})[0]
	assert.Equal(t, []string{"[REDACTED]"}, e.Headers.Values("Authorization"))
	assert.Equal(t, []string{"[REDACTED]"}, e.Headers.Values("Cookie"))
	assert.Equal(t, []string{"ok"}, e.Headers.Values("X-Other"))
}

func TestMiddleware_BodyEncoding(t *testing.T) {
	j := New(10)
	h := newRouter(j)
	do(h, "POST", "/api/v2/echo", strings.NewReader(`{"a":1}`), nil)
	do(h, "POST", "/api/v2/echo", strings.NewReader(`a=b`), nil)
	do(h, "POST", "/api/v2/echo", nil, nil)
	es := j.List(Filter{})
	require.Len(t, es, 3)
	assert.JSONEq(t, `{"a":1}`, string(es[0].Body))
	assert.JSONEq(t, `"a=b"`, string(es[1].Body))
	assert.Empty(t, es[2].Body)
	b, err := json.Marshal(es[2])
	require.NoError(t, err)
	assert.NotContains(t, string(b), `"body"`)
}

func TestMiddleware_BodyTruncated(t *testing.T) {
	j := New(10)
	body := `{"k":"` + strings.Repeat("x", 70000-8) + `"}`
	require.Len(t, body, 70000)
	var got int
	h := j.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = len(b)
		w.WriteHeader(200)
	}))
	do(h, "POST", "/api/v2/x", strings.NewReader(body), nil)
	assert.Equal(t, 70000, got)
	e := j.List(Filter{})[0]
	assert.True(t, e.BodyTruncated)
	var s string
	require.NoError(t, json.Unmarshal(e.Body, &s))
	assert.Equal(t, body[:MaxStoredBody], s)
}

func TestMiddleware_OversizeBodyKeepsDownstreamError(t *testing.T) {
	j := New(10)
	var readErr error
	next := j.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
		w.WriteHeader(400)
	}))
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 10)
		next.ServeHTTP(w, r)
	})
	do(wrapped, "POST", "/api/v2/x", bytes.NewReader([]byte(strings.Repeat("a", 100))), nil)
	var mbe *http.MaxBytesError
	require.True(t, errors.As(readErr, &mbe))
	assert.Len(t, j.List(Filter{}), 1)
}

func TestMiddleware_RecordsAtFirstWrite(t *testing.T) {
	j := New(10)
	release := make(chan struct{})
	started := make(chan struct{})
	srv := httptest.NewServer(j.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		assert.NoError(t, http.NewResponseController(w).Flush())
		close(started)
		<-release
	})))
	defer srv.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, err := http.Get(srv.URL + "/api/v2/events")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-started
	assert.Eventually(t, func() bool {
		es := j.List(Filter{})
		return len(es) == 1 && es[0].Status == 200
	}, 2*time.Second, 5*time.Millisecond)
	close(release)
	<-done
}

func TestMark_NoSlotIsNoop(t *testing.T) {
	MarkMatched(context.Background(), "x")
	MarkNoMatch(context.Background(), nil)
}

func TestMiddleware_NoWriteStillRecorded(t *testing.T) {
	j := New(10)
	h := j.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	do(h, "GET", "/api/v2/x", nil, nil)
	es := j.List(Filter{})
	require.Len(t, es, 1)
	assert.Equal(t, 200, es[0].Status)
}

func TestMiddleware_WriteHeaderWriteFlushSingleEntry(t *testing.T) {
	j := New(10)
	h := j.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("x"))
		assert.NoError(t, http.NewResponseController(w).Flush())
	}))
	do(h, "POST", "/api/v2/x", nil, nil)
	es := j.List(Filter{})
	require.Len(t, es, 1)
	assert.Equal(t, 201, es[0].Status)
}
