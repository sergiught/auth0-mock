// Package logger constructs the project's zerolog.Logger.
package logger

import (
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// New returns a logger configured for the given environment and minimum log
// level. In "development" the output is human-readable; otherwise JSON.
// Invalid levels fall back to info with a one-line warning rather than
// silently dropping logs.
func New(environment, level string) zerolog.Logger {
	zerolog.TimeFieldFormat = time.RFC3339Nano

	parsed, err := zerolog.ParseLevel(level)
	if err != nil || parsed == zerolog.NoLevel {
		fmt.Fprintf(os.Stderr, "logger: invalid LOG_LEVEL %q, falling back to info\n", level)
		parsed = zerolog.InfoLevel
	}

	var base zerolog.Logger
	if environment == "development" {
		base = zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).
			With().Timestamp().Logger()
	} else {
		base = zerolog.New(os.Stdout).With().Timestamp().Logger()
	}
	return base.Level(parsed)
}
