// Package logger constructs the project's zerolog.Logger.
package logger

import (
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// New returns a logger styled for humans reading a local-dev / CI tail.
// Output goes through zerolog's ConsoleWriter with:
//
//   - a compact `HH:MM:SS.mmm` timestamp (year/date isn't useful in a
//     dev tail and just shifts the message column right);
//   - colourised full-word level labels (TRACE/DEBUG/INFO/WARNING/
//     ERROR/FATAL/PANIC) so a reader doesn't have to map a three-
//     letter code in their head;
//   - a single eye-catching glyph on warning/error/fatal/panic only —
//     `⚠` for warning, `✗` for the rest — so unusual lines stand out
//     against a sea of info without adding noise to the common case.
//
// When stdout isn't a TTY (piped to a file, captured by CI, …) we drop
// both the colour and the glyph so log scrapers see plain ASCII.
//
// Auth0-mock is a local-dev / CI fixture, never an exposed production
// service, so optimising for human scannability over JSON-aggregator
// friendliness is the right call. If you ever need JSON for an
// aggregator, replace this constructor — don't add a toggle.
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

	noColor := !stdoutIsTTY()

	cw := zerolog.ConsoleWriter{
		Out:         os.Stdout,
		TimeFormat:  "15:04:05.000",
		NoColor:     noColor,
		FormatLevel: levelFormatter(noColor),
	}
	return zerolog.New(cw).With().Timestamp().Logger().Level(parsed)
}

// stdoutIsTTY returns true when stdout is a character device — i.e. an
// interactive terminal rather than a pipe or file. We use this to decide
// whether to emit ANSI colours and the warn/error glyph so non-TTY
// consumers see plain ASCII.
func stdoutIsTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// levelFormatter mirrors zerolog's default colours but uses full-word
// labels and layers a small glyph onto warning/error/fatal/panic so they
// catch the eye in a scrolling log. Info/debug/trace stay clean — most
// lines are info, so the eye should glide past them unless something
// stands out.
func levelFormatter(noColor bool) zerolog.Formatter {
	return func(i any) string {
		s, _ := i.(string)
		switch s {
		case zerolog.LevelTraceValue:
			return colorize("TRACE", "90", noColor) // Bright black (grey).
		case zerolog.LevelDebugValue:
			return colorize("DEBUG", "36", noColor) // Cyan.
		case zerolog.LevelInfoValue:
			return colorize("INFO", "32", noColor) // Green.
		case zerolog.LevelWarnValue:
			return colorize("⚠ WARNING", "33", noColor) // Yellow + warning glyph.
		case zerolog.LevelErrorValue:
			return colorize("✗ ERROR", "31", noColor) // Red + ballot-x glyph.
		case zerolog.LevelFatalValue:
			return colorize("✗ FATAL", "1;31", noColor) // Bold red + glyph.
		case zerolog.LevelPanicValue:
			return colorize("✗ PANIC", "1;31", noColor) // Bold red + glyph.
		default:
			return s
		}
	}
}

// colorize wraps s in an ANSI SGR sequence when colour is enabled, or
// returns s unchanged when noColor is true (piped output, dumb terminal).
func colorize(s, ansi string, noColor bool) string {
	if noColor {
		return s
	}
	return "\x1b[" + ansi + "m" + s + "\x1b[0m"
}
