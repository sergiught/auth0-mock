package auth0mock_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/pkg/auth0mock"
)

// TestReset_Success drives the happy path against a stub that records
// the method + path the SDK actually hit. Locks the wire-shape contract:
// POST /admin0/reset, no body, no required headers. A future SDK refactor
// can change anything internally; this test fails if the wire call
// changes.
func TestReset_Success(t *testing.T) {
	t.Parallel()
	var (
		gotMethod, gotPath string
		hits               atomic.Int32
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	c, err := auth0mock.NewClient(srv.URL)
	require.NoError(t, err)
	require.NoError(t, c.Reset(context.Background()))
	assert.Equal(t, int32(1), hits.Load())
	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/admin0/reset", gotPath)
}

// TestReset_APIError verifies that a server-side error envelope (the
// shape the mock actually returns from /admin0/* failures) round-trips
// into an *APIError with every field populated.
func TestReset_APIError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"statusCode":400,"error":"Bad Request","message":"bad things","errorCode":"some_code"}`))
	}))
	t.Cleanup(srv.Close)

	c, err := auth0mock.NewClient(srv.URL)
	require.NoError(t, err)
	err = c.Reset(context.Background())
	require.Error(t, err)

	var apiErr *auth0mock.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.Equal(t, "Bad Request", apiErr.Reason)
	assert.Equal(t, "bad things", apiErr.Message)
	assert.Equal(t, "some_code", apiErr.ErrorCode)
	// Error() should include both code and message so log lines are
	// actionable without the caller unwrapping anything.
	assert.Contains(t, err.Error(), "some_code")
	assert.Contains(t, err.Error(), "bad things")
}

// TestReset_NonJSONErrorBody covers the fallback path: server returns a
// non-2xx without the standard envelope. We still want a useful *APIError
// rather than swallowing the body or returning a generic Go error.
func TestReset_NonJSONErrorBody(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("oops, plain text"))
	}))
	t.Cleanup(srv.Close)

	c, err := auth0mock.NewClient(srv.URL)
	require.NoError(t, err)
	err = c.Reset(context.Background())
	var apiErr *auth0mock.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
	assert.Equal(t, "Internal Server Error", apiErr.Reason)
	assert.Equal(t, "oops, plain text", apiErr.Message)
}

// TestReset_ContextCancellation locks the canonical Go contract: a
// canceled context must abort the request and surface context.Canceled
// up the wrapped-error chain. Without this, callers can't enforce
// per-test budgets.
func TestReset_ContextCancellation(t *testing.T) {
	t.Parallel()
	// Server that intentionally hangs so the only way out is ctx cancel.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	c, err := auth0mock.NewClient(srv.URL)
	require.NoError(t, err)
	err = c.Reset(ctx)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled),
		"want context.Canceled in chain, got %v", err)
}
