// Package middleware contains shared net/http middleware.
package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type requestIDKey struct{}

// RequestIDFromContext returns the request_id stored in the context (or "").
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey{}).(string); ok {
		return v
	}
	return ""
}

// RequestID populates the context (and the X-Request-Id response header) with
// the incoming X-Request-Id header value, or a new UUID if absent.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := r.Header.Get("X-Request-Id")
		if rid == "" {
			rid = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", rid)
		ctx := context.WithValue(r.Context(), requestIDKey{}, rid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Recovery converts panics in downstream handlers into 500 responses.
func Recovery(log zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error().
						Str("request_id", RequestIDFromContext(r.Context())).
						Interface("panic", rec).
						Bytes("stack", debug.Stack()).
						Msg("panic recovered")
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"statusCode":500,"error":"Internal Server Error","message":"unexpected panic"}`))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// statusRecorder wraps http.ResponseWriter to capture status code and bytes.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

func (sr *statusRecorder) Write(b []byte) (int, error) {
	if sr.status == 0 {
		sr.status = http.StatusOK
	}
	n, err := sr.ResponseWriter.Write(b)
	sr.bytes += n
	return n, err
}

// MaxBodyBytes caps every incoming request body to limit bytes. Reads past
// the limit return *http.MaxBytesError from the wrapped reader; downstream
// handlers surface that to the client through their normal decode-error
// path (a 400 in this codebase). The cap exists to bound the per-request
// allocation that /admin0/expectations and /oauth/token would otherwise
// accept unbounded.
//
// Limit ≤ 0 is treated as "no limit" — the middleware is a no-op so callers
// can configure their way out of the cap if they really need to.
func MaxBodyBytes(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if limit <= 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// debugBodyCap is the per-direction body byte cap for DebugDump's log
// output (NOT the request body cap — that's MaxBodyBytes). 8 KiB is
// generous enough for typical OAuth / Mgmt-API payloads and small enough
// that flooding the console is hard. Bodies larger than this are
// truncated with a "…(truncated, N bytes total)" suffix.
const debugBodyCap = 8 * 1024

// debugDumpMu serialises a "structured log line + pretty body block"
// pair so concurrent requests can't interleave a body half-way through
// another request's metadata.
var debugDumpMu sync.Mutex

// DebugDump logs every request (method, path, query, headers, body) and
// every response (status, headers, body) at INFO level. Off by default
// — enable via the DEBUG env var. Output shape per request:
//
//	21:39:28 INFO  → request  method=POST  path=/oauth/token  body_bytes=63
//	    grant_type=client_credentials
//	    client_id=docs
//	    client_secret=<redacted>
//	21:39:28 INFO  ← response  status=200  body_bytes=142
//	    {
//	      "access_token": "<redacted>",
//	      "token_type": "Bearer",
//	      "expires_in": 86400
//	    }
//
// Bodies print AFTER the structured line as indented multi-line blocks
// because zerolog escapes everything inside a field value (which turns
// nested JSON into unreadable `\"` soup). JSON bodies get
// pretty-printed; form-encoded bodies split one key=value per line;
// everything else prints as-is. Authorization/Cookie headers are
// redacted (first 8 chars + "…<redacted>"); JWT/secret field values
// inside bodies are redacted to `<redacted>`. Bodies are capped at
// debugBodyCap (8 KiB).
//
// NOT for production — buffering every request + response body and
// serialising through the logger costs an allocation and a synchronous
// write per request. Auth0-mock is local-dev / CI tooling, but even
// here you only want this on while actively debugging an SDK trace.
func DebugDump(log zerolog.Logger, bodyOut io.Writer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqBody, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(reqBody))

			rid := RequestIDFromContext(r.Context())
			reqCT := r.Header.Get("Content-Type")

			debugDumpMu.Lock()
			log.Info().
				Str("request_id", rid).
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Str("query", scrubSensitiveQuery(r.URL.RawQuery)).
				Str("headers", flatHeaders(r.Header)).
				Int("body_bytes", len(reqBody)).
				Msg("→ request")
			writeBodyBlock(bodyOut, reqBody, len(reqBody), reqCT)
			debugDumpMu.Unlock()

			rec := &debugRecorder{ResponseWriter: w, body: &bytes.Buffer{}}
			next.ServeHTTP(rec, r)
			respCT := rec.Header().Get("Content-Type")

			debugDumpMu.Lock()
			log.Info().
				Str("request_id", rid).
				Int("status", rec.statusOrOK()).
				Str("headers", flatHeaders(rec.Header())).
				Int("body_bytes", rec.totalLen).
				Msg("← response")
			writeBodyBlock(bodyOut, rec.body.Bytes(), rec.totalLen, respCT)
			debugDumpMu.Unlock()
		})
	}
}

const bodyIndent = "    "

// writeBodyBlock renders the body for human eyeballs: redact sensitive
// fields, pretty-print JSON, split form-encoded into one pair per line,
// indent every output line with bodyIndent, and add a truncation suffix
// when the original write was bigger than the captured slice. Empty
// bodies print nothing.
func writeBodyBlock(out io.Writer, captured []byte, totalLen int, contentType string) {
	if len(captured) == 0 {
		return
	}
	pretty := strings.TrimRight(prettyBody(redactSensitiveInBody(captured), contentType), "\n")
	if totalLen > len(captured) {
		pretty += "\n…(truncated, " + strconv.Itoa(totalLen) + " bytes total)"
	}
	// Best-effort write — if the body block can't be flushed (broken
	// pipe on a piped tail, full disk on a capture), we'd rather drop
	// the dump than crash the request path it decorates.
	_, _ = fmt.Fprintln(out, indentLines(pretty, bodyIndent))
}

// prettyBody pretty-prints JSON bodies (two-space indent) and splits
// form-encoded bodies one key=value per line. Everything else returns
// as-is. JSON parse failures fall back to raw — a truncated JSON body
// is invalid JSON but the partial content is still useful.
func prettyBody(body []byte, contentType string) string {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "json") || looksLikeJSON(body) {
		var buf bytes.Buffer
		if err := json.Indent(&buf, body, "", "  "); err == nil {
			return buf.String()
		}
	}
	if strings.Contains(ct, "x-www-form-urlencoded") {
		return strings.ReplaceAll(string(body), "&", "\n")
	}
	return string(body)
}

// looksLikeJSON does a cheap first-byte check so bodies without a
// Content-Type but with obvious JSON shape still get pretty-printed.
func looksLikeJSON(b []byte) bool {
	for _, c := range b {
		switch c {
		case ' ', '\t', '\n', '\r':
			continue
		case '{', '[':
			return true
		default:
			return false
		}
	}
	return false
}

// indentLines prefixes every line of s with the given indent.
func indentLines(s, indent string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = indent + l
	}
	return strings.Join(lines, "\n")
}

// debugRecorder wraps http.ResponseWriter to capture status code, the
// total number of body bytes written, and a bounded copy of the body
// for DebugDump. TotalLen is tracked separately from body.Len() so
// writeBodyBlock can report the original size in the truncation suffix
// when the response was bigger than what we captured.
type debugRecorder struct {
	http.ResponseWriter
	status   int
	body     *bytes.Buffer
	totalLen int
}

func (d *debugRecorder) WriteHeader(code int) {
	d.status = code
	d.ResponseWriter.WriteHeader(code)
}

func (d *debugRecorder) Write(b []byte) (int, error) {
	if d.status == 0 {
		d.status = http.StatusOK
	}
	d.totalLen += len(b)
	if remaining := debugBodyCap - d.body.Len(); remaining > 0 {
		if len(b) > remaining {
			d.body.Write(b[:remaining])
		} else {
			d.body.Write(b)
		}
	}
	return d.ResponseWriter.Write(b)
}

func (d *debugRecorder) statusOrOK() int {
	if d.status == 0 {
		return http.StatusOK
	}
	return d.status
}

// flatHeaders renders an http.Header map as a sorted "k=v; k=v" string,
// redacting bearer tokens and cookies. Sorting keeps the dump diffable
// across runs.
func flatHeaders(h http.Header) string {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := strings.Join(h[k], ",")
		if isSensitiveHeader(k) {
			v = redact(v)
		}
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "; ")
}

func isSensitiveHeader(k string) bool {
	switch strings.ToLower(k) {
	case "authorization", "cookie", "set-cookie", "proxy-authorization":
		return true
	}
	return false
}

// redact keeps the first 8 chars of a header value (enough to tell
// "Bearer" from "Basic") and replaces the rest with a marker. Short
// values get fully replaced.
func redact(v string) string {
	if len(v) <= 8 {
		return "<redacted>"
	}
	return v[:8] + "…<redacted>"
}

// sensitiveBodyFields are the Auth0/OAuth field names whose values look
// like secrets in transit. We scrub their values out of request and
// response bodies before logging so a `DEBUG=true` SDK trace doesn't
// commit a real (or mock-minted) bearer to the operator's terminal
// history / scrollback. /oauth/token responses live or die by this:
// without scrubbing, every minted JWT is one Ctrl-F away.
var sensitiveBodyFields = []string{
	"access_token", "id_token", "refresh_token", "mfa_token",
	"client_secret", "password", "code_verifier", "client_assertion",
}

// jsonSensitiveRE matches "field":"value" inside JSON-encoded bodies.
// The character class `[^"\\]*(?:\\.[^"\\]*)*` skips over `\"` inside
// the value so the close-quote isn't mis-detected.
var jsonSensitiveRE = regexp.MustCompile(
	`"(` + strings.Join(sensitiveBodyFields, "|") + `)"\s*:\s*"([^"\\]*(?:\\.[^"\\]*)*)"`,
)

// formSensitiveRE matches field=value inside x-www-form-urlencoded
// request bodies (and the same shape in URL query strings).
var formSensitiveRE = regexp.MustCompile(
	`(^|&)(` + strings.Join(sensitiveBodyFields, "|") + `)=([^&]+)`,
)

// redactSensitiveInBody replaces values of sensitiveBodyFields with
// "<redacted>" in both JSON and form-encoded bodies. Pass-through for
// any other content (binary, HTML, plain text).
func redactSensitiveInBody(b []byte) []byte {
	if len(b) == 0 {
		return b
	}
	out := jsonSensitiveRE.ReplaceAll(b, []byte(`"$1":"<redacted>"`))
	out = formSensitiveRE.ReplaceAll(out, []byte(`${1}${2}=<redacted>`))
	return out
}

// scrubSensitiveQuery applies form-style redaction to a URL query string
// (the `?...` portion). Mirrors redactSensitiveInBody so a sensitive
// field sneaking into the query (e.g. `?access_token=…`) gets the same
// treatment.
func scrubSensitiveQuery(q string) string {
	if q == "" {
		return ""
	}
	return string(formSensitiveRE.ReplaceAll([]byte(q), []byte(`${1}${2}=<redacted>`)))
}

// Logging emits one structured log line per request.
func Logging(log zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sr := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(sr, r)
			log.Info().
				Str("request_id", RequestIDFromContext(r.Context())).
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", sr.status).
				Int("bytes", sr.bytes).
				Dur("latency", time.Since(start)).
				Msg("http request")
		})
	}
}
