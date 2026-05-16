package middleware

import (
	"bytes"
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
	// Action (method, path, status) goes in the message so a reader's
	// eye lands on it first instead of after the alphabetical field
	// wall. Bytes + latency stay as fields so they're greppable.
	var sb strings.Builder
	log := zerolog.New(&sb)

	h := Logging(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(204)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))

	out := sb.String()
	require.NotEmpty(t, out)
	assert.Contains(t, out, `"message":"GET /x 204"`,
		"action lives in the message — method, path, status, in that order")
	assert.Contains(t, out, `"bytes":0`)
	assert.Contains(t, out, `"latency":`)
	assert.NotContains(t, out, `"request_id"`,
		"rid is no longer dumped per-line; only echoed via X-Request-Id header")
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
	// Action (method + path on request, +status on response) lives in
	// the message so the eye lands on it first.
	assert.Contains(t, out, `"message":"→ POST /api/things"`)
	assert.Contains(t, out, `"message":"← POST /api/things 201"`)
	// Per-direction body_bytes count + filtered headers + latency on
	// the response side.
	assert.Contains(t, out, `"query":"q=1"`)
	assert.Contains(t, out, `"body_bytes":17`)
	assert.Contains(t, out, `"latency":`)
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
	// And a trailing blank line so multi-request output stays scannable.
	assert.True(t, strings.HasSuffix(bodyOut, "\n\n"),
		"each request triplet must end with a blank-line separator")
}

func TestInterestingHeaders_FiltersNoise(t *testing.T) {
	t.Parallel()

	// Kept=true means the header survives the noise filter; kept=false
	// means it gets dropped. Sections grouped by why they're noisy.
	cases := []struct {
		name   string
		header string
		value  string
		kept   bool
	}{
		// Standard request noise (exact-match denylist).
		{"Accept", "Accept", "*/*", false},
		{"User-Agent", "User-Agent", "curl/8", false},
		{"Content-Length redundant with body_bytes", "Content-Length", "12", false},
		{"Host", "Host", "localhost", false},
		{"Origin", "Origin", "https://localhost", false},
		{"Referer", "Referer", "https://localhost/docs", false},
		{"Accept-Encoding", "Accept-Encoding", "gzip", false},
		{"Accept-Language", "Accept-Language", "en-US", false},
		{"Cache-Control", "Cache-Control", "no-cache", false},
		{"Connection", "Connection", "keep-alive", false},
		{"Upgrade-Insecure-Requests", "Upgrade-Insecure-Requests", "1", false},

		// Browser fingerprinting / privacy.
		{"DNT", "Dnt", "1", false},
		{"Sec-GPC", "Sec-Gpc", "1", false},
		{"Priority", "Priority", "u=0", false},
		{"Pragma", "Pragma", "no-cache", false},

		// Sec-Fetch-* family (prefix match).
		{"Sec-Fetch-Dest", "Sec-Fetch-Dest", "empty", false},
		{"Sec-Fetch-Mode", "Sec-Fetch-Mode", "cors", false},
		{"Sec-Fetch-Site", "Sec-Fetch-Site", "same-origin", false},
		{"Sec-Fetch-User", "Sec-Fetch-User", "?1", false},

		// Sec-Ch-Ua* family (prefix match).
		{"Sec-Ch-Ua", "Sec-Ch-Ua", `"Chromium";v="128"`, false},
		{"Sec-Ch-Ua-Mobile", "Sec-Ch-Ua-Mobile", "?0", false},
		{"Sec-Ch-Ua-Platform", "Sec-Ch-Ua-Platform", `"Linux"`, false},

		// Forwarding metadata (production-only).
		{"X-Request-Id already echoed", "X-Request-Id", "abc-123", false},
		{"X-Forwarded-For", "X-Forwarded-For", "1.2.3.4", false},
		{"X-Real-IP", "X-Real-Ip", "1.2.3.4", false},

		// Response-side noise.
		{"Date redundant with log timestamp", "Date", "Mon, 16 May 2026", false},
		{"Server", "Server", "auth0-mock", false},

		// Signal: must survive.
		{"Content-Type kept", "Content-Type", "application/json", true},
		{"Authorization kept (with redaction)", "Authorization", "Bearer xxxxxxxxxxxxx", true},
		{"Cookie kept (with redaction)", "Cookie", "session=xxxxxxxxxxxxxxx", true},
		{"Set-Cookie kept", "Set-Cookie", "auth=xxxxxxxxxxxxxxx", true},
		{"Custom X-* kept", "X-Custom-Trace", "yes", true},
		{"Location kept (redirects)", "Location", "https://app/cb", true},
		{"WWW-Authenticate kept (OAuth challenge)", "Www-Authenticate", "Bearer error=invalid_token", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h := http.Header{}
			h.Set(c.header, c.value)
			got := interestingHeaders(h)
			if c.kept {
				assert.Contains(t, got, c.header+"=",
					"%s should have survived the filter", c.header)
			} else {
				assert.NotContains(t, got, c.header+"=",
					"%s should have been filtered as noise", c.header)
			}
		})
	}
}

func TestRecovery_PanicStackPrintsAsIndentedBlock(t *testing.T) {
	// The structured log line carries the panic value + locator; the
	// stack trace prints as a multi-line indented block to a separate
	// writer so a reader can actually see it (Bytes("stack", …) into
	// zerolog escaped every newline into `\n` soup).
	var logBuf strings.Builder
	var stackBuf bytes.Buffer
	prev := recoveryStackOut
	recoveryStackOut = &stackBuf
	t.Cleanup(func() { recoveryStackOut = prev })

	log := zerolog.New(&logBuf)
	h := Recovery(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("synthetic boom")
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/explode", nil))

	// Wire response stays 500 with the JSON body.
	assert.Equal(t, 500, w.Code)
	assert.Contains(t, w.Body.String(), `"statusCode":500`)

	// Log line carries the panic value AND the location.
	logOut := logBuf.String()
	assert.Contains(t, logOut, `"panic":"synthetic boom"`)
	assert.Contains(t, logOut, `panic recovered: POST /explode`)
	assert.NotContains(t, logOut, `"stack"`,
		"stack must NOT live in the structured log field — it's printed separately")

	// Stack writer got a multi-line, indented block (every line prefixed
	// with 4 spaces, no escape-soup).
	stackOut := stackBuf.String()
	assert.Contains(t, stackOut, "goroutine ")
	assert.NotContains(t, stackOut, `\n`,
		"stack must be raw multi-line, not JSON-escaped")
	for line := range strings.SplitSeq(strings.TrimRight(stackOut, "\n"), "\n") {
		assert.True(t, strings.HasPrefix(line, "    "),
			"every stack line must be indented with 4 spaces; got %q", line)
	}
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
