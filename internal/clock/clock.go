// Package clock provides a pluggable Now() time source so the mock's
// protocol-time outputs (JWT iat/exp, code/OTP TTLs, bearer validation)
// can be frozen or skewed at runtime via /admin0/clock for
// deterministic tests.
//
// Wall-clock uses (request logging, middleware latency, http.Server
// timeouts) deliberately keep using time.Now directly — log
// timestamps should not lie because protocol time was frozen.
package clock

import (
	"errors"
	"sync"
	"time"
)

// Clock is the single read-side primitive the mock's handlers depend
// on. One method keeps the surface trivial to stub.
type Clock interface {
	Now() time.Time
}

// Real is the production default: delegates straight to time.Now.
// Zero value is usable; empty struct so embedding is cheap.
type Real struct{}

// Now returns the wall clock.
func (Real) Now() time.Time { return time.Now() }

// Mode is the discriminator for Controlled's internal state.
type Mode string

// Mode values are also the JSON wire form returned by GET /admin0/clock.
const (
	ModeReal   Mode = "real"
	ModeFrozen Mode = "frozen"
	ModeOffset Mode = "offset"
)

// ErrAdvanceInRealMode is returned by Advance when the clock is in
// real mode. There's nothing to advance against; the caller should
// Freeze or Offset first.
var ErrAdvanceInRealMode = errors.New("clock: cannot advance while in real mode (Freeze or Offset first)")

// Controlled is a mutable Clock the admin0 surface and the SDK drive.
// Safe for concurrent use; all reads take an RLock, all mutations take
// the write lock.
type Controlled struct {
	mu     sync.RWMutex
	mode   Mode
	pinned time.Time     // valid only when mode == ModeFrozen
	offset time.Duration // valid only when mode == ModeOffset
}

// NewControlled returns a Controlled in real mode.
func NewControlled() *Controlled {
	return &Controlled{mode: ModeReal}
}

// Now returns the current effective time given the mode.
func (c *Controlled) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	switch c.mode {
	case ModeFrozen:
		return c.pinned
	case ModeOffset:
		return time.Now().Add(c.offset)
	default:
		return time.Now()
	}
}

// State returns (mode, current Now) as an atomic snapshot — no races
// between the mode and value reads.
func (c *Controlled) State() (Mode, time.Time) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	switch c.mode {
	case ModeFrozen:
		return c.mode, c.pinned
	case ModeOffset:
		return c.mode, time.Now().Add(c.offset)
	default:
		return c.mode, time.Now()
	}
}

// ConfiguredOffset returns the configured offset and true when the
// clock is in offset mode; (0, false) otherwise. Used by the GET
// /admin0/clock handler to surface the skew alongside the resolved
// now so callers don't have to compute now - wall_clock.
func (c *Controlled) ConfiguredOffset() (time.Duration, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.mode != ModeOffset {
		return 0, false
	}
	return c.offset, true
}

// Freeze pins Now to t until the next mode change. Also zeroes the
// offset field even though Now never reads it in frozen mode — a
// future method that touches offset without rechecking mode (e.g. a
// "scale offset by N" helper) shouldn't see leftover state from a
// prior Offset() call.
func (c *Controlled) Freeze(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mode = ModeFrozen
	c.pinned = t
	c.offset = 0
}

// Offset switches to offset mode with the given skew. The wall clock
// keeps ticking; Now returns time.Now() + d. Also zeroes the pinned
// field for the same future-safety reason as Freeze.
func (c *Controlled) Offset(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mode = ModeOffset
	c.offset = d
	c.pinned = time.Time{}
}

// Advance mutates the held value by d. In frozen mode the pinned
// instant moves by d; in offset mode the offset grows by d; in real
// mode the call is a no-op and returns ErrAdvanceInRealMode.
func (c *Controlled) Advance(d time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch c.mode {
	case ModeFrozen:
		c.pinned = c.pinned.Add(d)
		return nil
	case ModeOffset:
		c.offset += d
		return nil
	default:
		return ErrAdvanceInRealMode
	}
}

// Reset returns the clock to real mode and clears any held state.
func (c *Controlled) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mode = ModeReal
	c.pinned = time.Time{}
	c.offset = 0
}
