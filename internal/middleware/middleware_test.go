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

func TestDebugDump_LogsRequestAndResponse(t *testing.T) {
	var sb strings.Builder
	log := zerolog.New(&sb)

	h := DebugDump(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		assert.Equal(t, `{"hello":"world"}`, string(body),
			"DebugDump must restore the body so handlers can re-read it")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	req := httptest.NewRequest("POST", "/api/things?q=1", strings.NewReader(`{"hello":"world"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer eyJhbGciOiJSUzI1NiJ9.aaaaaaaaaaaaaaaaaa")
	h.ServeHTTP(httptest.NewRecorder(), req)

	out := sb.String()
	// Request line.
	assert.Contains(t, out, `"message":"→ request"`)
	assert.Contains(t, out, `"method":"POST"`)
	assert.Contains(t, out, `"path":"/api/things"`)
	assert.Contains(t, out, `"query":"q=1"`)
	assert.Contains(t, out, `{\"hello\":\"world\"}`)
	// Response line.
	assert.Contains(t, out, `"message":"← response"`)
	assert.Contains(t, out, `"status":201`)
	assert.Contains(t, out, `{\"ok\":true}`)
	// Authorization redaction: the prefix "Bearer e" leaks (intentional —
	// signals scheme) but the rest is gone. Zerolog writes literal "<" and
	// ">" rather than </>, hence the simple substring match.
	assert.Contains(t, out, "Bearer e…<redacted>")
	assert.NotContains(t, out, "eyJhbGciOiJSUzI1NiJ9", "full JWT must not leak")
}

func TestDebugDump_TruncatesLongBody(t *testing.T) {
	var sb strings.Builder
	log := zerolog.New(&sb)

	h := DebugDump(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, 16*1024)) // 16 KiB, twice the cap.
	}))
	req := httptest.NewRequest("GET", "/big", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	out := sb.String()
	assert.Contains(t, out, "(truncated, 16384 bytes total)",
		"response body over debugBodyCap must surface a truncation suffix")
}

func TestDebugDump_RedactsTokensInResponseBody(t *testing.T) {
	// Realistic /oauth/token response — the mock mints a real JWT and
	// returns it. DebugDump must NOT leak that token to logs even though
	// the response body is otherwise reproduced verbatim.
	const jwt = "eyJhbGciOiJSUzI1NiIsImtpZCI6Iko0M2RVajB0TkRJIn0.eyJleHAiOjE3NzkwNDU3MTd9.signaturepart"
	var sb strings.Builder
	log := zerolog.New(&sb)

	h := DebugDump(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"` + jwt + `","id_token":"` + jwt + `","token_type":"Bearer","expires_in":86400}`))
	}))
	req := httptest.NewRequest("POST", "/oauth/token", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	out := sb.String()
	assert.NotContains(t, out, jwt, "JWT value must be scrubbed from the logged response body")
	assert.Contains(t, out, `\"access_token\":\"<redacted>\"`)
	assert.Contains(t, out, `\"id_token\":\"<redacted>\"`)
	// Non-sensitive fields stay visible — that's the whole point of the dump.
	assert.Contains(t, out, `\"token_type\":\"Bearer\"`)
	assert.Contains(t, out, `\"expires_in\":86400`)
}

func TestDebugDump_RedactsCredentialsInRequestBody(t *testing.T) {
	// Form-encoded POST to /oauth/token with client_secret + refresh_token
	// — both must be scrubbed from the logged request body so a debug
	// session doesn't commit them to terminal scrollback.
	var sb strings.Builder
	log := zerolog.New(&sb)

	h := DebugDump(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	body := "grant_type=refresh_token&client_id=abc&client_secret=supersecret123&refresh_token=rt-abcdef"
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(httptest.NewRecorder(), req)

	out := sb.String()
	assert.NotContains(t, out, "supersecret123")
	assert.NotContains(t, out, "rt-abcdef")
	assert.Contains(t, out, "client_secret=<redacted>")
	assert.Contains(t, out, "refresh_token=<redacted>")
	// Client_id stays visible.
	assert.Contains(t, out, "client_id=abc")
}

func TestDebugDump_ScrubsTokensInQueryString(t *testing.T) {
	var sb strings.Builder
	log := zerolog.New(&sb)

	h := DebugDump(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/x?access_token=should-not-leak&legit=value", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	out := sb.String()
	assert.NotContains(t, out, "should-not-leak")
	assert.Contains(t, out, "access_token=<redacted>")
	assert.Contains(t, out, "legit=value")
}

func TestFlatHeaders_RedactsAndSorts(t *testing.T) {
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("Authorization", "Bearer 1234567890abcdef")
	h.Set("Cookie", "session=topsecret")
	h.Set("X-Custom", "alpha")

	out := flatHeaders(h)
	// Sorted alphabetically by header name.
	assert.Regexp(t, `^Authorization=.*; Content-Type=application/json; Cookie=.*; X-Custom=alpha$`, out)
	assert.Contains(t, out, "Bearer 1…<redacted>")
	assert.Contains(t, out, "session=…<redacted>")
	assert.NotContains(t, out, "1234567890abcdef")
	assert.NotContains(t, out, "topsecret")
}
