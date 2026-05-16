// Package logger constructs the project's zerolog.Logger.
package logger

import (
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// New returns a logger that always writes a human-readable, colourised line
// per record via zerolog's ConsoleWriter — auth0-mock is a local-dev / CI
// fixture, never an exposed production service, so optimising for human
// scannability over JSON-aggregator friendliness is the right call.
//
// Invalid log levels fall back to info with a one-line stderr warning
// rather than silently dropping logs.
func New(level string) zerolog.Logger {
	zerolog.TimeFieldFormat = time.RFC3339Nano

	parsed, err := zerolog.ParseLevel(level)
	if err != nil || parsed == zerolog.NoLevel {
		fmt.Fprintf(os.Stderr, "logger: invalid LOG_LEVEL %q, falling back to info\n", level)
		parsed = zerolog.InfoLevel
	}

	cw := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
		// Keep colour on whenever the writer is a TTY (zerolog auto-
		// detects via NoColor's default-false). Both `make demo` and
		// `make watch` benefit; CI's tee-into-file path strips ANSI
		// downstream rather than us pre-emptively disabling here.
	}
	return zerolog.New(cw).With().Timestamp().Logger().Level(parsed)
}
