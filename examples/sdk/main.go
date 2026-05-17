// Command sdk-example walks every public resource on the auth0-mock
// Go SDK against a running mock, then exercises one of the stubs
// end-to-end via the official go-auth0 SDK — the same code path a
// production caller would take, just pointed at the local mock.
//
// Run the mock in another terminal first:
//
//	make watch        # or: docker compose up -d --build
//
// Then run the example from this directory:
//
//	cd examples/sdk
//	go run .                                     # InsecureSkipVerify on the mock's self-signed cert
//	go run . -cert /tmp/auth0-mock-demo-tls/tls.crt   # full chain verification
//
// `make demo-sdk` from the repo root does the full setup automatically:
// boots the mock with a persisted TLS cert, waits for /healthz, runs
// this example with -cert pointed at the cert, tears the mock down.
//
// The example lives in its own Go module so the go-auth0 dependency
// doesn't leak into the auth0-mock module graph. The local-path
// replace points at the in-tree SDK; downstream consumers copying
// this example into their own project should drop the replace and
// pin a real version.
//
// Flags:
//
//	-mock   base URL of the running auth0-mock (HTTPS — go-auth0 only speaks TLS)
//	-cert   PEM file containing the mock's TLS cert; if empty, skip verification
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/auth0/go-auth0"
	"github.com/auth0/go-auth0/authentication"
	"github.com/auth0/go-auth0/authentication/oauth"
	"github.com/auth0/go-auth0/management"

	"github.com/sergiught/auth0-mock/pkg/auth0mock"
)

func main() {
	mockURL := flag.String("mock", "https://localhost:8443", "auth0-mock base URL (HTTPS — go-auth0 only speaks TLS)")
	certFile := flag.String("cert", "", "PEM file containing the mock's TLS cert; if empty, skip verification")
	flag.Parse()

	ctx := context.Background()

	httpClient, err := newHTTPClient(*certFile)
	if err != nil {
		log.Fatalf("build HTTP client: %v", err)
	}

	// Hand the TLS-aware http.Client to the SDK so /admin0 calls
	// succeed against the same listener go-auth0 will talk to later.
	c, err := auth0mock.NewClient(*mockURL, auth0mock.WithHTTPClient(httpClient))
	if err != nil {
		log.Fatal(err)
	}

	banner("auth0-mock SDK example")
	fmt.Printf("  Mock:  %s\n", *mockURL)
	fmt.Printf("  SDK:   github.com/sergiught/auth0-mock/pkg/auth0mock\n")
	fmt.Printf("  Driver: github.com/auth0/go-auth0 (real Auth0 SDK pointed at the mock)\n\n")

	if err = run(ctx, c, *mockURL, httpClient); err != nil {
		log.Fatal(err)
	}

	banner("Done — SDK stubs verified end-to-end through go-auth0")
	fmt.Println("  Drop into your own tests with `auth0mocktest.Bracket(t, c)` and you're done.")
}

func run(ctx context.Context, c *auth0mock.Client, mockURL string, hc *http.Client) error {
	if err := phase1Reset(ctx, c); err != nil {
		return err
	}
	aliceStub, err := phase2Stubs(ctx, c)
	if err != nil {
		return err
	}
	if err := phase3Claims(ctx, c); err != nil {
		return err
	}
	if err := phase4Permissions(ctx, c); err != nil {
		return err
	}
	if err := phase5MFA(ctx, c); err != nil {
		return err
	}
	if err := phase6Clock(ctx, c); err != nil {
		return err
	}
	if err := phase7Readback(ctx, c); err != nil {
		return err
	}
	accessToken, err := phase8DriveWithGoAuth0(ctx, mockURL, hc)
	if err != nil {
		return err
	}
	if err := phase9TokenExpired(ctx, c, mockURL, accessToken, hc); err != nil {
		return err
	}
	if err := phase10Verify(ctx, c, aliceStub); err != nil {
		return err
	}
	return phase11Cleanup(ctx, c)
}

// ── Phases ──────────────────────────────────────────────────────────

func phase1Reset(ctx context.Context, c *auth0mock.Client) error {
	section(1, "Reset")
	wire("POST", "/admin0/reset")
	explain("Wipes every expectation, claim, permission, and MFA flag",
		"back to startup defaults. Equivalent to restarting the mock",
		"process — but ~1000x faster. Call from t.Cleanup so each test",
		"starts from a known-empty mock.")
	if err := c.Reset(ctx); err != nil {
		return fmt.Errorf("reset: %w", err)
	}
	ok("Mock state cleared")
	return nil
}

// phase2Stubs registers two Management API stubs. Returns the
// concrete-path handle so phase 8 can assert it was hit exactly once
// by the go-auth0 round-trip in phase 7.
func phase2Stubs(ctx context.Context, c *auth0mock.Client) (*auth0mock.RegisteredExpectation, error) {
	section(2, "Register Management API stubs")
	explain("Stubs are registered via POST /admin0/expectations. The",
		"server validates each response body against the OpenAPI schema",
		"for that operation at registration time — bad fixtures fail",
		"fast instead of mid-test.")

	wire("POST", "/admin0/expectations")
	fmt.Println("  Stub 1 — concrete path (exact match) + Times(1) constraint:")
	fmt.Println("    GET /api/v2/users/auth0|alice")
	fmt.Println(`    → 200 {"user_id":"auth0|alice","email":"alice@example.com"}`)
	alice, err := c.ExpectGet("/api/v2/users/auth0|alice").
		Respond(200).
		JSON(map[string]any{"user_id": "auth0|alice", "email": "alice@example.com"}).
		Apply(ctx)
	if err != nil {
		return nil, fmt.Errorf("stub alice: %w", err)
	}
	alice.Times(1)
	ok("Registered (will be Verify'd in phase 8)")

	wire("POST", "/admin0/expectations")
	fmt.Println("  Stub 2 — template (catch-all for every other user id):")
	fmt.Println("    GET /api/v2/users/{id}")
	fmt.Println(`    → 200 {"user_id":"any","email":"placeholder@example.com"}`)
	if _, err := c.ExpectGet("/api/v2/users/{id}").
		Respond(200).
		JSON(map[string]any{"user_id": "any", "email": "placeholder@example.com"}).
		Apply(ctx); err != nil {
		return nil, fmt.Errorf("stub catch-all: %w", err)
	}
	ok("Registered")

	explain("Resolution rule: concrete-path stubs always beat template",
		"stubs, so a request for auth0|alice gets stub 1; any other user",
		"id falls through to stub 2.")
	return alice, nil
}

func phase3Claims(ctx context.Context, c *auth0mock.Client) error {
	section(3, "Inject custom JWT claims")
	wire("PUT", "/admin0/claims")
	explain("Custom claims merge into every access_token and id_token the",
		"mock mints. Use them to fake namespaced Auth0 Action output",
		"(tenant IDs, roles, feature flags, ...).")
	fmt.Println("  Setting:")
	fmt.Println(`    "https://example.com/tenant": "acme"`)
	if err := c.Claims.Set(ctx, map[string]any{
		"https://example.com/tenant": "acme",
	}); err != nil {
		return fmt.Errorf("set claims: %w", err)
	}
	ok("Claims set — next minted token will carry them")
	return nil
}

func phase4Permissions(ctx context.Context, c *auth0mock.Client) error {
	section(4, "Set per-audience permissions")
	wire("PUT", "/admin0/permissions/https%3A%2F%2Fapi.example.com%2F")
	explain("Permissions land in the `permissions` claim of access",
		"tokens issued for the given audience. URL-form audiences",
		"(with slashes) work natively — the SDK url.PathEscapes them",
		"on the wire and the server's chi wildcard route decodes.")
	fmt.Println("  Setting on audience https://api.example.com/:")
	fmt.Println("    • read:users")
	fmt.Println("    • write:users")
	if err := c.Permissions.Set(ctx, "https://api.example.com/", []string{
		"read:users", "write:users",
	}); err != nil {
		return fmt.Errorf("set permissions: %w", err)
	}
	ok("Permissions set")
	return nil
}

func phase5MFA(ctx context.Context, c *auth0mock.Client) error {
	section(5, "Toggle MFA enforcement")
	wire("PUT", "/admin0/mfa-required")
	explain("Forces the password and password-realm grants to demand an",
		"MFA step-up before issuing a token. Process-global toggle; flip",
		"it back to false to disable.")
	if err := c.MFA.Set(ctx, true); err != nil {
		return fmt.Errorf("require mfa: %w", err)
	}
	ok("MFA now required for password / password-realm grants")
	return nil
}

// The clock script phase 6 runs. Declared at package scope so phase 8
// can assert against the resulting "now" without re-encoding the
// freeze/advance values — change these here and the assertion follows.
var (
	clockFreezeStart = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	clockAdvances    = []time.Duration{time.Hour, 25 * time.Hour}
)

// clockEnd is the wall-time the mock holds after phase 6 runs. Phase
// 8 asserts the minted token's iat equals this value — proving the
// clock reaches the minter end-to-end.
func clockEnd() time.Time {
	end := clockFreezeStart
	for _, d := range clockAdvances {
		end = end.Add(d)
	}
	return end
}

// phase6Clock demonstrates the admin0/clock surface. Freezes at
// clockFreezeStart, applies clockAdvances in order, and reads the
// resulting state back. The end state is the iat phase 8's go-auth0
// token mint will inherit.
func phase6Clock(ctx context.Context, c *auth0mock.Client) error {
	section(6, "Control the clock")
	explain("Freeze, offset, and advance the mock's perception of time.",
		"Lets tests assert on token iat/exp without sleeping or",
		"hand-minting fixtures. The end state here is the `now`",
		"phase 8's go-auth0 token mint will inherit.")

	wire("PUT", "/admin0/clock")
	fmt.Printf("  Freezing at %s\n", clockFreezeStart.Format(time.RFC3339))
	if err := c.Clock.Freeze(ctx, clockFreezeStart); err != nil {
		return fmt.Errorf("freeze: %w", err)
	}
	ok("Clock frozen")

	for _, d := range clockAdvances {
		wire("POST", "/admin0/clock/advance")
		fmt.Printf("  Advancing %s\n", d)
		if err := c.Clock.Advance(ctx, d); err != nil {
			return fmt.Errorf("advance %s: %w", d, err)
		}
	}

	wire("GET", "/admin0/clock")
	state, err := c.Clock.Get(ctx)
	if err != nil {
		return fmt.Errorf("get clock: %w", err)
	}
	fmt.Printf("  Clock now: mode=%s now=%s\n", state.Mode, state.Now.Format(time.RFC3339))
	ok("Phase 8's mint will see this exact instant")
	return nil
}

func phase7Readback(ctx context.Context, c *auth0mock.Client) error {
	section(7, "Read state back")
	explain("Every sub-client exposes a Get/List/All counterpart for the",
		"setter. Useful for snapshot-style assertions or sanity-checking",
		"test setup before the system-under-test runs.")

	wire("GET", "/admin0/expectations")
	exps, err := c.Expectations.List(ctx)
	if err != nil {
		return fmt.Errorf("list expectations: %w", err)
	}
	fmt.Printf("  Found %d registered expectation(s):\n", len(exps))
	for i, e := range exps {
		bodyPreview := compactJSON(e.Response.Body)
		if bodyPreview == "" {
			bodyPreview = "(no body)"
		}
		fmt.Printf("    %d. %s %s  →  %d %s\n", i+1, e.Method, e.Path, e.Response.Status, bodyPreview)
	}

	wire("GET", "/admin0/claims")
	claims, err := c.Claims.Get(ctx)
	if err != nil {
		return fmt.Errorf("get claims: %w", err)
	}
	fmt.Printf("  Found %d claim(s):\n", len(claims))
	for _, k := range sortedKeys(claims) {
		fmt.Printf("    • %s = %v\n", k, claims[k])
	}

	wire("GET", "/admin0/permissions/https%3A%2F%2Fapi.example.com%2F")
	perms, err := c.Permissions.Get(ctx, "https://api.example.com/")
	if err != nil {
		return fmt.Errorf("get permissions: %w", err)
	}
	fmt.Printf("  Audience https://api.example.com/ has %d permission(s):\n", len(perms))
	for _, p := range perms {
		fmt.Printf("    • %s\n", p)
	}

	wire("GET", "/admin0/mfa-required")
	required, err := c.MFA.Get(ctx)
	if err != nil {
		return fmt.Errorf("get mfa: %w", err)
	}
	fmt.Printf("  MFA required: %v\n", required)
	return nil
}

// phase8DriveWithGoAuth0 closes the loop: the official go-auth0 SDK
// makes the SAME API calls a production service would, but lands on
// the mock instead of Auth0. The mock answers from the stubs phase 2
// registered through our SDK. From go-auth0's point of view, it's
// talking to Auth0; it has no idea the response came from a fixture.
//
// Returns the minted access token so phase 9 can reuse it for the
// expiry assertion — proving the bearer middleware shares the same
// clock as the minter.
func phase8DriveWithGoAuth0(ctx context.Context, mockURL string, hc *http.Client) (string, error) {
	section(8, "Drive the stubs through the real go-auth0 SDK")
	explain("Until now every wire call was made by the auth0-mock SDK.",
		"This phase boots a real go-auth0 client pointed at the mock's",
		"HTTPS listener, mints a token, then calls api.User.Read — the",
		"same code path a production caller would take. The mock answers",
		"from the stub we registered in phase 2.")

	domain := strings.TrimPrefix(mockURL, "https://")

	wire("POST", "/oauth/token")
	fmt.Println("  Minting an access token via go-auth0.authentication ...")
	auth, err := authentication.New(ctx, domain,
		authentication.WithClientID("demo"),
		authentication.WithClientSecret("x"),
		authentication.WithClient(hc),
	)
	if err != nil {
		return "", fmt.Errorf("new go-auth0 authentication client: %w", err)
	}
	tokens, err := auth.OAuth.LoginWithClientCredentials(ctx, oauth.LoginWithClientCredentialsRequest{
		Audience: "https://api.example.com/",
	}, oauth.IDTokenValidationOptions{})
	if err != nil {
		return "", fmt.Errorf("client_credentials grant: %w", err)
	}
	// Show the JWT header prefix only (~12 chars) so the demo output
	// has a visible "yes we got a token" signal without spilling
	// claim bytes if the token shape ever changes. Safe here because
	// it's a mock token; the same pattern against real Auth0 would
	// be a leak.
	ok(fmt.Sprintf("Got an access token (%s..., %d bytes total)",
		preview(tokens.AccessToken, 12), len(tokens.AccessToken)))

	// Decode the token and prove the clock from phase 6 drove the
	// minter. iat must equal the frozen instant + 26h of advances.
	claims, err := decodeJWTPayload(tokens.AccessToken)
	if err != nil {
		return "", fmt.Errorf("decode minted token: %w", err)
	}
	iat, err := claimUnix(claims, "iat")
	if err != nil {
		return "", fmt.Errorf("iat: %w", err)
	}
	exp, err := claimUnix(claims, "exp")
	if err != nil {
		return "", fmt.Errorf("exp: %w", err)
	}
	fmt.Printf("  Decoded token: iat=%s exp=%s\n",
		time.Unix(iat, 0).UTC().Format(time.RFC3339),
		time.Unix(exp, 0).UTC().Format(time.RFC3339))

	wantIat := clockEnd().Unix()
	if iat != wantIat {
		return "", fmt.Errorf("iat = %d, want %d (clock should be at %s after phase 6)",
			iat, wantIat, clockEnd().Format(time.RFC3339))
	}
	ok("iat matches the clock state set in phase 6 — clock reaches the minter")

	api, err := management.New(domain,
		management.WithStaticToken(tokens.AccessToken),
		management.WithClient(hc),
	)
	if err != nil {
		return "", fmt.Errorf("new go-auth0 management client: %w", err)
	}

	wire("GET", "/api/v2/users/auth0|alice")
	fmt.Println("  Calling api.User.Read('auth0|alice') via go-auth0 ...")
	user, err := api.User.Read(ctx, "auth0|alice")
	if err != nil {
		return "", fmt.Errorf("api.User.Read: %w", err)
	}
	fmt.Printf("  ↳ got back user: id=%s email=%s\n",
		auth0.StringValue(user.ID), auth0.StringValue(user.Email))
	ok("go-auth0 SDK received exactly what the auth0mock SDK stubbed")
	return tokens.AccessToken, nil
}

// phase9TokenExpired proves the bearer middleware shares the same
// clock as the minter. Reuses the access token minted in phase 8
// (frozen at 2030-01-02T02:00:00Z, TTL 1h ⇒ exp = 2030-01-02T03:00:00Z).
// Advances the clock 7 days, then re-runs the stubbed Read call with
// the same token — expects 401 because the token's exp is now well
// in the past from the mock's POV.
//
// Phase 2's stub still has Times(1): bearer middleware short-circuits
// before the matcher fires, so the failed call doesn't count against
// the constraint and phase 10's Verify stays green.
func phase9TokenExpired(ctx context.Context, c *auth0mock.Client, mockURL, accessToken string, hc *http.Client) error {
	section(9, "Token expiry (negative test) — same token, expired by the clock")
	explain("Advances the mock's clock 7 days past where phase 8 minted",
		"the token. The bearer middleware now sees an expired token and",
		"returns 401 — proving the validator is wired to the same clock",
		"as the minter.")

	wire("POST", "/admin0/clock/advance")
	fmt.Println("  Advancing the clock by 7 days")
	if err := c.Clock.Advance(ctx, 7*24*time.Hour); err != nil {
		return fmt.Errorf("advance 7d: %w", err)
	}

	wire("GET", "/api/v2/users/auth0|alice  (expecting 401)")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		mockURL+"/api/v2/users/auth0|alice", nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("expected 401, got %d: %s", resp.StatusCode, string(body))
	}
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("  ↳ got 401: %s\n", strings.TrimSpace(string(body)))
	ok("Bearer middleware rejected the token — clock reaches the validator too")
	return nil
}

// phase10Verify proves the go-auth0 call in phase 8 actually hit the
// stub we registered in phase 2 — by checking the Times(1) constraint
// set on the handle there. Phase 9's 401 doesn't count: bearer
// middleware short-circuits before the matcher fires.
func phase10Verify(ctx context.Context, c *auth0mock.Client, alice *auth0mock.RegisteredExpectation) error {
	section(10, "Verify the stub was hit (Times constraint)")
	explain("Phase 2 set Times(1) on the alice stub. Now that go-auth0",
		"has run, calling Verify checks every registered constraint",
		"server-side and returns nil on success or a joined error",
		"naming every violator.")

	hits, err := alice.Hits(ctx)
	if err != nil {
		return fmt.Errorf("read alice hits: %w", err)
	}
	fmt.Printf("  Hits on alice stub: %d (expected exactly 1)\n", hits)

	wire("GET", "/admin0/expectations  (Verify reads List once and matches by ID)")
	if err := c.Expectations.Verify(ctx); err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	ok("All constraints satisfied")
	return nil
}

func phase11Cleanup(ctx context.Context, c *auth0mock.Client) error {
	section(11, "Final cleanup")
	wire("POST", "/admin0/reset")
	explain("Always reset on exit so the next test or example starts",
		"from a clean slate. auth0mocktest.Bracket(t, c) does this",
		"automatically + wires Verify in one call. The same Reset also",
		"restores the clock to real mode, so phase 6's freeze doesn't",
		"leak across runs.")
	if err := c.Reset(ctx); err != nil {
		return fmt.Errorf("final reset: %w", err)
	}
	ok("Mock state cleared")

	wire("GET", "/admin0/clock")
	state, err := c.Clock.Get(ctx)
	if err != nil {
		return fmt.Errorf("post-reset clock get: %w", err)
	}
	fmt.Printf("  Clock after reset: mode=%s\n", state.Mode)
	if state.Mode != "real" {
		return fmt.Errorf("expected mode=real after reset, got %q", state.Mode)
	}
	ok("Reset also restored real-time mode")
	return nil
}

// ── HTTP / TLS plumbing ─────────────────────────────────────────────

// newHTTPClient builds an HTTP client suitable for talking to the mock
// over HTTPS. If certFile is non-empty the file is loaded as a PEM
// certificate and added to the client's RootCAs pool (full chain
// verification — what `make demo-sdk` wires up). If certFile is
// empty, falls back to InsecureSkipVerify so the example still works
// against any local mock without setup.
func newHTTPClient(certFile string) (*http.Client, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS13}

	if certFile == "" {
		tlsCfg.InsecureSkipVerify = true //nolint:gosec // demo-only; documented above + runtime warning below.
		log.Println("⚠  TLS verification DISABLED (InsecureSkipVerify=true). " +
			"This is fine for the local mock demo — NEVER copy this pattern into production code, " +
			"and never run it against any host you don't fully control. " +
			"Pass -cert to enable full chain verification.")
	} else {
		pem, err := os.ReadFile(certFile)
		if err != nil {
			return nil, fmt.Errorf("read cert file %s: %w", certFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("no PEM-encoded certs in %s", certFile)
		}
		tlsCfg.RootCAs = pool
	}

	return &http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
	}, nil
}

// ── Output helpers ─────────────────────────────────────────────────

// banner prints a heavyweight section divider. Used for the top + bottom
// of the example's run.
func banner(title string) {
	bar := strings.Repeat("═", 71)
	fmt.Printf("\n%s\n  %s\n%s\n", bar, title, bar)
}

// section prints a per-phase header. The numbered title gives the run
// a visible spine when scrolled past.
func section(n int, title string) {
	bar := strings.Repeat("─", 71)
	fmt.Printf("\n%s\n  Phase %d — %s\n%s\n", bar, n, title, bar)
}

// wire echoes the wire call about to fire so readers can correlate the
// SDK method to the HTTP endpoint without grepping the package source.
func wire(method, path string) {
	fmt.Printf("→ %s %s\n", method, path)
}

// explain prints a multi-line indented prose block under the wire line.
// Keeps each example phase self-documenting.
func explain(lines ...string) {
	for _, l := range lines {
		fmt.Printf("  %s\n", l)
	}
}

// ok marks a successful step with a check + brief status.
func ok(msg string) {
	fmt.Printf("✓ %s\n", msg)
}

// preview shortens s to maxLen + "..." for inline display.
func preview(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// compactJSON renders a json.RawMessage as a single-line, whitespace-
// stripped string for inline display. Empty input returns "".
func compactJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(out)
}

// sortedKeys returns the keys of a string-keyed map in lexical order so
// the output is deterministic between runs.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// decodeJWTPayload base64-decodes the payload segment of a compact JWT
// and JSON-unmarshals it. Verifies nothing — this is a demo-side
// pretty-printer, not a security check; the real bearer middleware
// verified the token when it accepted the request.
func decodeJWTPayload(tok string) (map[string]any, error) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("token is not a compact JWT (%d segments)", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}
	return out, nil
}

// claimUnix returns the integer Unix-seconds value of a numeric claim,
// or an error if the claim is missing / wrong type. encoding/json
// decodes JSON numbers as float64 by default.
func claimUnix(claims map[string]any, name string) (int64, error) {
	v, ok := claims[name]
	if !ok {
		return 0, fmt.Errorf("claim %q absent", name)
	}
	f, ok := v.(float64)
	if !ok {
		return 0, fmt.Errorf("claim %q is %T, want number", name, v)
	}
	return int64(f), nil
}
