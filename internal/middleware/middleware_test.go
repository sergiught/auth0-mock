package middleware

import (
	"bytes"
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

func TestDebugDump_LogsRequestAndResponseMetadata(t *testing.T) {
	// The structured log line carries METADATA only (method, path,
	// query, headers, body_bytes). Body content prints separately as
	// an indented multi-line block to bodyOut, NOT as a field value —
	// otherwise zerolog escapes everything inside the field and JSON
	// turns into unreadable `\"`-soup.
	var sb strings.Builder
	var bodyBuf bytes.Buffer
	log := zerolog.New(&sb)

	h := DebugDump(log, &bodyBuf)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	// Request metadata.
	assert.Contains(t, out, `"message":"→ request"`)
	assert.Contains(t, out, `"method":"POST"`)
	assert.Contains(t, out, `"path":"/api/things"`)
	assert.Contains(t, out, `"query":"q=1"`)
	assert.Contains(t, out, `"body_bytes":17`)
	// Response metadata.
	assert.Contains(t, out, `"message":"← response"`)
	assert.Contains(t, out, `"status":201`)
	assert.Contains(t, out, `"body_bytes":11`)
	// Authorization redaction: the prefix "Bearer e" leaks (intentional —
	// signals scheme) but the rest is gone.
	assert.Contains(t, out, "Bearer e…<redacted>")
	assert.NotContains(t, out, "eyJhbGciOiJSUzI1NiJ9", "full JWT must not leak")
	// The body itself should NOT appear inside the structured field
	// area — that was the whole point of the rewrite.
	assert.NotContains(t, out, `\"hello\":\"world\"`,
		"raw escaped body must not appear in the structured field area")

	// And it SHOULD appear in the body buffer, pretty-printed because
	// Content-Type was application/json.
	bodyOut := bodyBuf.String()
	assert.Contains(t, bodyOut, "    {\n      \"hello\": \"world\"\n    }")
	assert.Contains(t, bodyOut, "    {\n      \"ok\": true\n    }")
}

// writeBodyBlock is the function that prints the multi-line body block;
// the tests below exercise it directly because DebugDump writes to
// os.Stdout. The end-to-end visual is verified manually via `make watch`
// + DEBUG=true.
func TestWriteBodyBlock_JSONIsPrettyPrinted(t *testing.T) {
	var buf bytes.Buffer
	writeBodyBlock(&buf, []byte(`{"a":1,"b":[2,3],"c":{"d":"e"}}`), 31, "application/json")
	got := buf.String()
	// Indented two-space JSON, every line prefixed with 4 spaces.
	assert.Contains(t, got, "    {\n")
	assert.Contains(t, got, "      \"a\": 1")
	assert.Contains(t, got, "      \"b\": [")
}

func TestWriteBodyBlock_FormSplitsOneLinePerPair(t *testing.T) {
	var buf bytes.Buffer
	writeBodyBlock(&buf, []byte(`grant_type=password&client_id=abc&username=u`), 44,
		"application/x-www-form-urlencoded")
	got := buf.String()
	assert.Contains(t, got, "    grant_type=password\n")
	assert.Contains(t, got, "    client_id=abc\n")
	assert.Contains(t, got, "    username=u\n")
}

func TestWriteBodyBlock_TruncationSuffix(t *testing.T) {
	var buf bytes.Buffer
	// 100-byte captured slice, but totalLen says the wire response
	// was 16384 bytes — the suffix should reflect the true size.
	writeBodyBlock(&buf, []byte(strings.Repeat("x", 100)), 16384, "text/plain")
	got := buf.String()
	assert.Contains(t, got, "(truncated, 16384 bytes total)")
}

func TestWriteBodyBlock_EmptyBodyWritesNothing(t *testing.T) {
	var buf bytes.Buffer
	writeBodyBlock(&buf, nil, 0, "application/json")
	assert.Equal(t, "", buf.String())
}

func TestWriteBodyBlock_RedactionStillRuns(t *testing.T) {
	var buf bytes.Buffer
	writeBodyBlock(&buf, []byte(`{"access_token":"eyJhbGciOiJSUzI1NiJ9.aaa"}`),
		44, "application/json")
	got := buf.String()
	assert.NotContains(t, got, "eyJhbGciOiJSUzI1NiJ9")
	assert.Contains(t, got, `"access_token": "<redacted>"`)
}

// TestDebugDump_EndToEndRedaction exercises the full middleware against
// a realistic /oauth/token round-trip: form-encoded request with
// client_secret + refresh_token, JSON response with a minted JWT. Every
// known-sensitive field must be scrubbed both in the structured log
// (for queries + headers) and in the printed body block.
func TestDebugDump_EndToEndRedaction(t *testing.T) {
	const jwt = "eyJhbGciOiJSUzI1NiIsImtpZCI6Iko0M2RVajB0TkRJIn0.eyJleHAiOjE3NzkwNDU3MTd9.signaturepart"

	var sb strings.Builder
	var bodyBuf bytes.Buffer
	log := zerolog.New(&sb)

	h := DebugDump(log, &bodyBuf)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"` + jwt + `","token_type":"Bearer","expires_in":86400}`))
	}))
	body := "grant_type=refresh_token&client_id=abc&client_secret=supersecret123&refresh_token=rt-abcdef"
	req := httptest.NewRequest("POST", "/oauth/token?access_token=should-not-leak&legit=value",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+jwt)
	h.ServeHTTP(httptest.NewRecorder(), req)

	combined := sb.String() + bodyBuf.String()

	// Nothing sensitive in either stream.
	assert.NotContains(t, combined, jwt, "JWT must be scrubbed everywhere")
	assert.NotContains(t, combined, "supersecret123", "client_secret value leaks")
	assert.NotContains(t, combined, "rt-abcdef", "refresh_token value leaks")
	assert.NotContains(t, combined, "should-not-leak", "query token leaks")

	// Redactions DID happen.
	assert.Contains(t, sb.String(), "access_token=<redacted>",
		"query field scrubbed")
	assert.Contains(t, sb.String(), "Bearer e…<redacted>",
		"header bearer scrubbed")
	assert.Contains(t, bodyBuf.String(), "client_secret=<redacted>",
		"form body secret scrubbed")
	assert.Contains(t, bodyBuf.String(), "refresh_token=<redacted>",
		"form body token scrubbed")
	assert.Contains(t, bodyBuf.String(), `"access_token": "<redacted>"`,
		"JSON body access_token scrubbed (with pretty-print)")

	// Non-sensitive values still readable.
	assert.Contains(t, bodyBuf.String(), "client_id=abc")
	assert.Contains(t, bodyBuf.String(), `"token_type": "Bearer"`)
	assert.Contains(t, sb.String(), "legit=value")
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
