package logger

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

// TestLevelFormatter_NoColor locks the human-readable codes: info/debug/
// trace stay clean, warn/error/fatal/panic get the eye-catching glyph.
// NoColor=true keeps the assertions free of ANSI sequences.
func TestLevelFormatter_NoColor(t *testing.T) {
	t.Parallel()
	f := levelFormatter(true)

	cases := map[string]string{
		zerolog.LevelTraceValue: "TRACE",
		zerolog.LevelDebugValue: "DEBUG",
		zerolog.LevelInfoValue:  "INFO",
		zerolog.LevelWarnValue:  "⚠ WARNING",
		zerolog.LevelErrorValue: "✗ ERROR",
		zerolog.LevelFatalValue: "✗ FATAL",
		zerolog.LevelPanicValue: "✗ PANIC",
	}
	for level, want := range cases {
		t.Run(level, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, f(level))
		})
	}
}

// TestLevelFormatter_WithColor confirms the SGR wrapper actually wraps when
// noColor=false (which is what we hand to an interactive terminal). We
// don't pin exact ANSI codes — only that the output contains the start
// (ESC [) and end (ESC [0m) tokens so a future colour tweak doesn't
// break the test.
func TestLevelFormatter_WithColor(t *testing.T) {
	t.Parallel()
	out := levelFormatter(false)(zerolog.LevelInfoValue)
	assert.Contains(t, out, "\x1b[")
	assert.Contains(t, out, "\x1b[0m")
	assert.Contains(t, out, "INFO")
}

// TestLevelFormatter_UnknownLevel falls through to returning the input
// verbatim so a future zerolog level addition doesn't crash the formatter.
func TestLevelFormatter_UnknownLevel(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "verbose", levelFormatter(true)("verbose"))
	assert.Equal(t, "", levelFormatter(true)(""))
}

// TestNew_InvalidLevelFallsBackToInfo confirms the documented behaviour
// (warning to stderr, info-level logger returned) instead of e.g. panicking
// or returning a zero-value logger that silently drops everything.
func TestNew_InvalidLevelFallsBackToInfo(t *testing.T) {
	l := New("garbage")
	// Zerolog exposes the configured level via GetLevel().
	assert.Equal(t, zerolog.InfoLevel, l.GetLevel())
}
