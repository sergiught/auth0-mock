package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"

	"github.com/sergiught/auth0-mock/internal/middleware"
)

// TestLogging_InfoDisabled_BypassesRecorder covers the fast-path guard: when
// the logger's info level is filtered out, Logging must hand the request
// straight to the next handler with the raw ResponseWriter (no statusRecorder
// wrap) and emit nothing. Zerolog.Nop() is a disabled logger, so
// log.Info().Enabled() is false and the guard fires.
func TestLogging_InfoDisabled_BypassesRecorder(t *testing.T) {
	mw := middleware.Logging(zerolog.Nop())

	var gotWriter http.ResponseWriter
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		gotWriter = w
		w.WriteHeader(http.StatusTeapot)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	mw(next).ServeHTTP(rec, req)

	if !called {
		t.Fatal("next handler was not called")
	}
	// The guard passes the original ResponseWriter through unwrapped; the
	// slow path would have substituted a *statusRecorder here.
	if gotWriter != http.ResponseWriter(rec) {
		t.Errorf("next received %T, want the original *httptest.ResponseRecorder", gotWriter)
	}
	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
}
