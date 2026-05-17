package clock_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/clock"
)

// approxNow asserts got is within tol of wall-clock now. Used for
// real-mode and offset-mode assertions where exact equality isn't
// achievable.
func approxNow(t *testing.T, got time.Time, expectedOffset, tol time.Duration) {
	t.Helper()
	want := time.Now().Add(expectedOffset)
	diff := got.Sub(want)
	if diff < 0 {
		diff = -diff
	}
	assert.LessOrEqualf(t, diff, tol, "now=%s want≈%s (±%s) diff=%s", got, want, tol, diff)
}

func TestRealClock(t *testing.T) {
	c := clock.Real{}
	approxNow(t, c.Now(), 0, 50*time.Millisecond)
}

func TestControlled_StartsInRealMode(t *testing.T) {
	c := clock.NewControlled()
	mode, now := c.State()
	assert.Equal(t, clock.ModeReal, mode)
	approxNow(t, now, 0, 50*time.Millisecond)
}

func TestControlled_Freeze(t *testing.T) {
	c := clock.NewControlled()
	t0 := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	c.Freeze(t0)

	mode, now := c.State()
	assert.Equal(t, clock.ModeFrozen, mode)
	assert.True(t, now.Equal(t0), "now=%s want=%s", now, t0)

	// Subsequent Now() calls return the same instant.
	time.Sleep(20 * time.Millisecond)
	assert.True(t, c.Now().Equal(t0))
}

func TestControlled_Offset(t *testing.T) {
	c := clock.NewControlled()
	c.Offset(25 * time.Hour)

	mode, now := c.State()
	assert.Equal(t, clock.ModeOffset, mode)
	approxNow(t, now, 25*time.Hour, 50*time.Millisecond)

	off, ok := c.ConfiguredOffset()
	require.True(t, ok)
	assert.Equal(t, 25*time.Hour, off)
}

func TestControlled_Reset(t *testing.T) {
	c := clock.NewControlled()
	c.Freeze(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	c.Reset()
	mode, now := c.State()
	assert.Equal(t, clock.ModeReal, mode)
	approxNow(t, now, 0, 50*time.Millisecond)
}

func TestControlled_Advance_FrozenAddsToHeld(t *testing.T) {
	c := clock.NewControlled()
	t0 := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	c.Freeze(t0)

	require.NoError(t, c.Advance(time.Hour))
	assert.True(t, c.Now().Equal(t0.Add(time.Hour)))

	require.NoError(t, c.Advance(25*time.Hour))
	assert.True(t, c.Now().Equal(t0.Add(26*time.Hour)))
}

func TestControlled_Advance_OffsetAddsToOffset(t *testing.T) {
	c := clock.NewControlled()
	c.Offset(time.Hour)
	require.NoError(t, c.Advance(time.Hour))
	approxNow(t, c.Now(), 2*time.Hour, 50*time.Millisecond)
}

func TestControlled_Advance_RealReturnsError(t *testing.T) {
	c := clock.NewControlled()
	err := c.Advance(time.Hour)
	require.Error(t, err)
	assert.ErrorIs(t, err, clock.ErrAdvanceInRealMode)
}

func TestControlled_Advance_NegativeDelta(t *testing.T) {
	c := clock.NewControlled()
	t0 := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	c.Freeze(t0)
	require.NoError(t, c.Advance(-2*time.Hour))
	assert.True(t, c.Now().Equal(t0.Add(-2*time.Hour)))
}

func TestControlled_OffsetCanBeNegative(t *testing.T) {
	c := clock.NewControlled()
	c.Offset(-7 * 24 * time.Hour)
	approxNow(t, c.Now(), -7*24*time.Hour, 50*time.Millisecond)
}

func TestControlled_ConfiguredOffset_NotInOffsetMode(t *testing.T) {
	c := clock.NewControlled()
	_, ok := c.ConfiguredOffset()
	assert.False(t, ok)

	c.Freeze(time.Now())
	_, ok = c.ConfiguredOffset()
	assert.False(t, ok)
}

func TestControlled_ImplementsClockInterface(t *testing.T) {
	var _ clock.Clock = clock.Real{}
	var _ clock.Clock = clock.NewControlled()
}

func TestControlled_Snapshot_Frozen(t *testing.T) {
	c := clock.NewControlled()
	t0 := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	c.Freeze(t0)

	mode, now, offset, hasOffset := c.Snapshot()
	assert.Equal(t, clock.ModeFrozen, mode)
	assert.True(t, now.Equal(t0))
	assert.Equal(t, time.Duration(0), offset)
	assert.False(t, hasOffset)
}

func TestControlled_Snapshot_Offset(t *testing.T) {
	c := clock.NewControlled()
	c.Offset(25 * time.Hour)

	mode, now, offset, hasOffset := c.Snapshot()
	assert.Equal(t, clock.ModeOffset, mode)
	approxNow(t, now, 25*time.Hour, 50*time.Millisecond)
	assert.Equal(t, 25*time.Hour, offset)
	assert.True(t, hasOffset)
}

func TestControlled_Snapshot_Real(t *testing.T) {
	c := clock.NewControlled()
	mode, now, offset, hasOffset := c.Snapshot()
	assert.Equal(t, clock.ModeReal, mode)
	approxNow(t, now, 0, 50*time.Millisecond)
	assert.Equal(t, time.Duration(0), offset)
	assert.False(t, hasOffset)
}

// TestControlled_Snapshot_NeverInconsistentUnderContention pushes the
// Snapshot accessor through many concurrent mode flips and asserts
// that every (mode, hasOffset) pair returned is internally consistent
// — i.e. hasOffset is true if and only if mode == "offset". The
// previous State + ConfiguredOffset combo took two separate RLocks
// and could return inconsistent pairs under contention; this test is
// the regression guard for that.
func TestControlled_Snapshot_NeverInconsistentUnderContention(t *testing.T) {
	c := clock.NewControlled()
	// Prime in offset mode so both flip directions are exercised.
	c.Offset(time.Hour)

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				c.Freeze(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
				c.Offset(time.Hour)
			}
		}
	}()
	defer close(done)

	for range 10_000 {
		mode, _, _, hasOffset := c.Snapshot()
		if mode == clock.ModeOffset {
			assert.True(t, hasOffset, "offset mode must have hasOffset=true")
		} else {
			assert.False(t, hasOffset, "non-offset mode (%s) must have hasOffset=false", mode)
		}
	}
}
