// Package middleware contains shared net/http middleware.
package middleware

import (
	"context"
	"net/http"
	"runtime/debug"
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
// the limit return an error from the handler and surface to the client as
// 413 Request Entity Too Large via http.MaxBytesError, so handlers that
// already error-handle their decode path don't need extra logic.
//
// limit ≤ 0 is treated as "no limit" — the middleware is a no-op so callers
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
