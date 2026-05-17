package auth0mock

import (
	"context"
	"net/http"
)

// Reset clears every expectation, claim, permission, and MFA flag back
// to the mock's startup defaults, and restores the clock to real mode.
// Equivalent to restarting the mock process from the perspective of
// registered state, but ~1000x faster — call this from t.Cleanup so
// each test starts from a known-empty mock.
//
// Also drops the Client's local verification ledger (the list of
// *RegisteredExpectation handles tracked for Expectations.Verify) so
// post-Reset Verify is a clean slate matching the server's empty
// state. If the ledger was non-empty when dropped — i.e. there were
// unverified Times/AtLeast/AtMost constraints — a flag is raised so
// the next Verify() returns an actionable error pointing at the
// cleanup-ordering bug (rather than silently passing because the
// constraints are gone). [auth0mocktest.Bracket] gets the order
// right; manual t.Cleanup setups can invert it, which this guard
// catches.
//
// Returns *APIError for non-2xx responses; a wrapped transport error
// otherwise. The local ledger is dropped regardless of server errors
// — partial failures on Reset are unrecoverable anyway, and a dirty
// ledger would only make future Verify output more confusing.
func (c *Client) Reset(ctx context.Context) error {
	err := c.do(ctx, http.MethodPost, "/admin0/reset", nil, nil)
	c.dropRegistered()
	return err
}
