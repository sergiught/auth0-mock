package auth0mock

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// ClockState mirrors GET /admin0/clock.
//
// Mode is one of "real", "frozen", or "offset". Now is the current
// effective time according to the mock — what clock.Now() returns
// server-side. Offset is the configured skew, populated only when
// Mode == "offset" (zero otherwise).
type ClockState struct {
	Mode   string
	Now    time.Time
	Offset time.Duration
}

// clockWireResponse is the raw JSON shape. The wire form uses Go
// duration strings ("25h", "30m") so the server-side handler can
// just call time.Duration.String(); the SDK parses them back into
// typed time.Duration values for callers.
type clockWireResponse struct {
	Mode   string `json:"mode"`
	Now    string `json:"now"`
	Offset string `json:"offset,omitempty"`
}

// ClockClient is the typed entry point for /admin0/clock — runtime
// control over the mock's perception of time. Reach it via
// Client.Clock.
//
// The mock has one global clock; ClockClient mutations are
// process-global. Tests that share a single mock across parallel
// workers should serialise clock mutations.
type ClockClient struct{ c *Client }

// Get returns the current clock state.
func (cl *ClockClient) Get(ctx context.Context) (*ClockState, error) {
	var raw clockWireResponse
	if err := cl.c.do(ctx, http.MethodGet, "/admin0/clock", nil, &raw); err != nil {
		return nil, err
	}
	t, err := time.Parse(time.RFC3339, raw.Now)
	if err != nil {
		return nil, fmt.Errorf("auth0mock: clock: parse server now %q: %w", raw.Now, err)
	}
	out := &ClockState{Mode: raw.Mode, Now: t}
	if raw.Offset != "" {
		d, err := time.ParseDuration(raw.Offset)
		if err != nil {
			return nil, fmt.Errorf("auth0mock: clock: parse server offset %q: %w", raw.Offset, err)
		}
		out.Offset = d
	}
	return out, nil
}

// Freeze pins the mock's clock to t. Subsequent token mints and
// bearer validations see Now == t until the next Freeze/Offset/Reset.
func (cl *ClockClient) Freeze(ctx context.Context, t time.Time) error {
	body := map[string]string{"now": t.UTC().Format(time.RFC3339)}
	return cl.c.do(ctx, http.MethodPut, "/admin0/clock", body, nil)
}

// Offset switches the mock's clock into offset mode with skew d. The
// wall clock keeps ticking; Now returns time.Now() + d server-side.
// Replaces any prior offset value (does not add to it — use Advance
// for incremental shifts).
func (cl *ClockClient) Offset(ctx context.Context, d time.Duration) error {
	body := map[string]string{"offset": d.String()}
	return cl.c.do(ctx, http.MethodPut, "/admin0/clock", body, nil)
}

// Advance mutates the held value by d. In frozen mode the pinned
// instant moves by d; in offset mode the configured offset grows by d.
// In real mode the server returns *APIError with ErrorCode
// "invalid_clock_state" — Freeze or Offset first.
func (cl *ClockClient) Advance(ctx context.Context, d time.Duration) error {
	body := map[string]string{"by": d.String()}
	return cl.c.do(ctx, http.MethodPost, "/admin0/clock/advance", body, nil)
}

// Reset restores the mock's clock to real mode and clears any held
// state. POST /admin0/reset also restores the clock, so
// auth0mocktest.Bracket covers this automatically.
func (cl *ClockClient) Reset(ctx context.Context) error {
	return cl.c.do(ctx, http.MethodDelete, "/admin0/clock", nil, nil)
}
