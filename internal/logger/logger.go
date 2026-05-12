// Package logger constructs the project's zerolog.Logger.
package logger

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

// New returns a logger configured for the given environment.
// In "development" the output is human-readable; otherwise JSON.
func New(environment string) zerolog.Logger {
	zerolog.TimeFieldFormat = time.RFC3339Nano
	if environment == "development" {
		return zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).
			With().Timestamp().Logger()
	}
	return zerolog.New(os.Stdout).With().Timestamp().Logger()
}
