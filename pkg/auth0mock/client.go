package auth0mock

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// defaultTimeout is the per-request HTTP timeout used when the caller
// hasn't supplied a custom *http.Client. Generous enough to absorb a
// cold Docker-bridged mock start, tight enough that a misconfigured
// baseURL fails the test instead of hanging it.
const defaultTimeout = 30 * time.Second

// Client is a typed handle to a running auth0-mock control plane (the
// /admin0/* HTTP surface). Safe for concurrent use across goroutines —
// every sub-client shares the same underlying *http.Client.
//
// /admin0/* is unauthenticated by design; Client does not carry a token
// and offers no way to set one. Bind the mock to localhost or keep it
// inside your CI container.
type Client struct {
	baseURL string
	http    *http.Client

	// Expectations is the typed entry point for the
	// /admin0/expectations CRUD surface.
	Expectations *ExpectationsClient

	// Claims is the typed entry point for /admin0/claims — the
	// per-process custom-claim map merged into every minted token.
	Claims *ClaimsClient

	// Permissions is the typed entry point for /admin0/permissions —
	// the per-audience permission lists baked into the `permissions`
	// claim of issued access tokens.
	Permissions *PermissionsClient

	// MFA is the typed entry point for /admin0/mfa-required — the
	// process-wide toggle that gates the password and password-realm
	// grants behind an MFA step-up.
	MFA *MFAClient

	// RegisteredMu guards registered (the list of handles returned
	// from Expectations.Add). Consulted by Expectations.Verify and
	// pruned by Clear / Reset.
	registeredMu sync.Mutex
	registered   []*RegisteredExpectation

	// PendingAdds counts handles added since the last Verify or
	// Reset. ResetAfterUnverified flips to true when Reset wipes
	// the ledger while pendingAdds > 0; Verify checks this flag to
	// catch the "user wrote Reset before Verify" cleanup-ordering
	// bug (auth0mocktest.Bracket gets the order right, but manual
	// t.Cleanup setups can invert it silently). A successful Verify
	// clears the flag so subsequent Adds + Verifies in the same
	// Client carry on cleanly.
	pendingAdds          atomic.Int64
	resetAfterUnverified atomic.Bool
}

// trackRegistered appends re to the Client's verification ledger
// and increments the pending-adds counter. Called by Expectations.Add;
// not part of the public API.
func (c *Client) trackRegistered(re *RegisteredExpectation) {
	c.registeredMu.Lock()
	defer c.registeredMu.Unlock()
	c.registered = append(c.registered, re)
	c.pendingAdds.Add(1)
}

// decrementPendingAdds subtracts n from pendingAdds, clamped at 0.
// Called by the untrack* helpers when the ledger shrinks via Clear
// (not Reset). Without this, an Add → Clear → Reset sequence would
// leave pendingAdds > 0 from the Add, then Reset would trip the
// safety net even though the constraint was intentionally retracted.
// Caller must hold registeredMu.
func (c *Client) decrementPendingAdds(n int) {
	if n <= 0 {
		return
	}
	for {
		cur := c.pendingAdds.Load()
		if cur == 0 {
			return
		}
		next := max(cur-int64(n), 0)
		if c.pendingAdds.CompareAndSwap(cur, next) {
			return
		}
	}
}

// untrackByID removes any ledger entry whose ID matches id. Called by
// per-stub Clear to keep the ledger bounded across long Add → Clear
// cycles and keep pendingAdds in sync. Idempotent — no-op if id is
// absent.
func (c *Client) untrackByID(id string) {
	c.registeredMu.Lock()
	defer c.registeredMu.Unlock()
	before := len(c.registered)
	kept := c.registered[:0]
	for _, re := range c.registered {
		if re.ID != id {
			kept = append(kept, re)
		}
	}
	// Zero the gap so the GC reclaims the dropped *RegisteredExpectation.
	for i := len(kept); i < len(c.registered); i++ {
		c.registered[i] = nil
	}
	c.registered = kept
	c.decrementPendingAdds(before - len(kept))
}

// untrackByMethodPath removes every ledger entry whose method+path
// matches the args. Called by Expectations.ClearOp (which wipes a
// whole operation's stubs server-side); the SDK mirrors the wipe in
// the local ledger so Verify doesn't carry stale handles.
//
// Method comparison is case-insensitive (via strings.EqualFold) to
// match the server's normalisation — stubs registered with "get"
// live in the same bucket as "GET", and ClearOp("GET", ...) Must
// prune both.
func (c *Client) untrackByMethodPath(method, path string) {
	c.registeredMu.Lock()
	defer c.registeredMu.Unlock()
	before := len(c.registered)
	kept := c.registered[:0]
	for _, re := range c.registered {
		if !strings.EqualFold(re.method, method) || re.path != path {
			kept = append(kept, re)
		}
	}
	for i := len(kept); i < len(c.registered); i++ {
		c.registered[i] = nil
	}
	c.registered = kept
	c.decrementPendingAdds(before - len(kept))
}

// untrackAll wipes every ledger entry. Called by Expectations.Clear
// (which wipes every server-side stub) so Verify after a bulk Clear
// doesn't report cleared violations. Also resets pendingAdds so a
// subsequent Reset doesn't fire the safety net for the cleared
// constraints.
func (c *Client) untrackAll() {
	c.registeredMu.Lock()
	defer c.registeredMu.Unlock()
	for i := range c.registered {
		c.registered[i] = nil
	}
	c.registered = c.registered[:0]
	c.pendingAdds.Store(0)
}

// registeredSnapshot returns a copy of the verification ledger so
// Verify can iterate without holding the mutex.
func (c *Client) registeredSnapshot() []*RegisteredExpectation {
	c.registeredMu.Lock()
	defer c.registeredMu.Unlock()
	out := make([]*RegisteredExpectation, len(c.registered))
	copy(out, c.registered)
	return out
}

// dropRegistered empties the verification ledger AND records whether
// the wipe destroyed unverified adds. Reset calls this; Verify
// inspects the resetAfterUnverified flag to catch wrong-order cleanup
// setups.
//
// Both the pendingAdds swap and the slice wipe happen under
// registeredMu so a concurrent trackRegistered can't slip a fresh
// append between them (which would leave a handle in the ledger with
// pendingAdds=0, silently un-trackable for Verify).
func (c *Client) dropRegistered() {
	c.registeredMu.Lock()
	defer c.registeredMu.Unlock()
	if c.pendingAdds.Swap(0) > 0 {
		c.resetAfterUnverified.Store(true)
	}
	c.registered = nil
}

// Option configures a Client at construction.
type Option func(*Client)

// WithHTTPClient overrides the default *http.Client. Use this to plug
// in your own retries, transports, or timeouts — for example a client
// with a longer timeout when the mock is behind a slow CI network.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.http = h
		}
	}
}

// NewClient binds to a running auth0-mock instance at baseURL.
//
// BaseURL is the root the mock is serving from — for example
// "http://localhost:8080" or "https://localhost:8443" — NOT the
// /admin0 prefix. A trailing slash is tolerated and trimmed.
//
// Returns an error if baseURL is empty, unparsable, or missing a
// scheme or host. Failing fast at construction keeps typo'd URLs
// from looking like cryptic transport errors on the first SDK call.
// For tests, the auth0mocktest.Must* helpers are convenient wrappers
// that t.Fatal on this error.
func NewClient(baseURL string, opts ...Option) (*Client, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("auth0mock: NewClient: baseURL is empty")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("auth0mock: NewClient: parse baseURL %q: %w", baseURL, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("auth0mock: NewClient: baseURL %q must include a scheme and host (e.g. http://localhost:8080)", baseURL)
	}
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: defaultTimeout},
	}
	for _, opt := range opts {
		opt(c)
	}
	c.Expectations = &ExpectationsClient{c: c}
	c.Claims = &ClaimsClient{c: c}
	c.Permissions = &PermissionsClient{c: c}
	c.MFA = &MFAClient{c: c}
	return c, nil
}

// BaseURL returns the server root the client is bound to. Mostly useful
// for logging and for the testing.TB helpers.
func (c *Client) BaseURL() string {
	return c.baseURL
}
