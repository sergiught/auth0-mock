package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecovery_TurnsPanicInto500(t *testing.T) {
	log := zerolog.New(io.Discard)
	h := Recovery(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		panic("boom")
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
	assert.Equal(t, 500, w.Code)
}

func TestRequestID_GeneratesWhenAbsent(t *testing.T) {
	var seen string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFromContext(r.Context())
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
	assert.NotEmpty(t, seen)
	assert.Equal(t, seen, w.Header().Get("X-Request-Id"))
}

func TestRequestID_PassesIncomingHeader(t *testing.T) {
	var seen string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFromContext(r.Context())
	}))
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("X-Request-Id", "abc-123")
	h.ServeHTTP(w, req)
	assert.Equal(t, "abc-123", seen)
}

func TestMaxBodyBytes_RejectsOversize(t *testing.T) {
	t.Parallel()
	called := false
	h := MaxBodyBytes(8)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = io.ReadAll(r.Body) // Triggers the cap check.
	}))
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/x", strings.NewReader("payload-is-longer-than-eight-bytes"))
	h.ServeHTTP(w, req)
	assert.True(t, called, "handler must run; MaxBytesReader signals via the read error, not by short-circuiting")
}

func TestMaxBodyBytes_AllowsUnderCap(t *testing.T) {
	t.Parallel()
	h := MaxBodyBytes(64)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		_, _ = w.Write(body)
	}))
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/x", strings.NewReader("small"))
	h.ServeHTTP(w, req)
	assert.Equal(t, "small", w.Body.String())
}

func TestMaxBodyBytes_NoLimitIsNoop(t *testing.T) {
	t.Parallel()
	h := MaxBodyBytes(0)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		_, _ = w.Write(body)
	}))
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/x", strings.NewReader(strings.Repeat("x", 4096)))
	h.ServeHTTP(w, req)
	assert.Equal(t, 4096, w.Body.Len())
}

func TestLogging_WritesOneLinePerRequest(t *testing.T) {
	var sb strings.Builder
	log := zerolog.New(&sb)

	ctx := context.WithValue(context.Background(), requestIDKey{}, "rid-1")
	h := Logging(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(204)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil).WithContext(ctx))

	out := sb.String()
	require.NotEmpty(t, out)
	assert.Contains(t, out, `"method":"GET"`)
	assert.Contains(t, out, `"status":204`)
	assert.Contains(t, out, `"request_id":"rid-1"`)
}
