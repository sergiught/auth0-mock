// Package scenario holds the godog test harness: a per-scenario context that
// boots the auth0-mock service in-process on a random port and provides HTTP
// helpers for the .feature step files.
package scenario

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/cucumber/godog"
	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog"
	"github.com/tidwall/gjson"

	"github.com/sergiught/auth0-mock/api"
	"github.com/sergiught/auth0-mock/internal/claims"
	"github.com/sergiught/auth0-mock/internal/clock"
	"github.com/sergiught/auth0-mock/internal/journal"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/mfa"
	"github.com/sergiught/auth0-mock/internal/permissions"
	"github.com/sergiught/auth0-mock/internal/pkce"
	"github.com/sergiught/auth0-mock/internal/router"
	"github.com/sergiught/auth0-mock/internal/seed"
	"github.com/sergiught/auth0-mock/internal/server"
	"github.com/sergiught/auth0-mock/internal/spec"
)

// Context is the per-scenario state passed to step definitions.
type Context struct {
	t          *testing.T
	BaseURL    string
	Host       string // when set, every request carries this Host header
	BearerTok  string
	MFAToken   string // captured from a 403 mfa_required response.
	RefreshTok string // captured from a token response.
	LastResp   *http.Response
	LastBody   []byte
	SavedBody  []byte // set by "I save the response body".
	// SavedValues holds JSON values saved by name with "I save the response
	// JSON path".
	SavedValues map[string]string
	// SSEResp holds the response from a successful SSE subscription
	// so subsequent steps (push, deliver) can read frames from its
	// body without conflicting with assertions against LastResp.
	SSEResp    *http.Response
	cancelBoot context.CancelFunc
	bootDone   chan struct{} // closed once the booted server has fully stopped
	// jarClient is set by the "I keep cookies" step; nil means requests use
	// nonFollowingClient and carry no cookies.
	jarClient *http.Client
	// seedPath and seed are set when the scenario booted with a seed file.
	seedPath string
	seed     *seed.Manager
}

// bootOptions configures boot.
type bootOptions struct {
	// issuerFor maps the listen address to the ISSUER_URL value.
	issuerFor func(addr string) string
	// seedFile, when set, is loaded as SEED_FILE before the router is built.
	seedFile string
}

// nonFollowingClient is used by all step requests so the tests can assert on
// 302 responses (e.g. /authorize, /v2/logout) instead of silently following.
var nonFollowingClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// client returns the cookie-keeping client once "I keep cookies" ran, and the
// shared cookieless nonFollowingClient otherwise.
func (c *Context) client() *http.Client {
	base := nonFollowingClient
	if c.jarClient != nil {
		base = c.jarClient
	}
	if c.Host == "" {
		return base
	}
	clone := *base
	clone.Transport = hostTransport{host: c.Host, next: http.DefaultTransport}
	return &clone
}

// hostTransport sends requests with a fixed Host header unless the request
// already carries its own (req.Host set and different from the URL host).
type hostTransport struct {
	host string
	next http.RoundTripper
}

func (h hostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	if req.Host == "" || req.Host == req.URL.Host {
		req.Host = h.host
	}
	return h.next.RoundTrip(req)
}

// KeepCookies makes this scenario's later requests share one cookie jar.
func (c *Context) KeepCookies() {
	jar, _ := cookiejar.New(nil)
	c.jarClient = &http.Client{
		Jar:           jar,
		CheckRedirect: nonFollowingClient.CheckRedirect,
	}
}

// New constructs a fresh Context, boots the service in-process on a random
// localhost port, and registers cleanup hooks via godog.
func New(t *testing.T, sc *godog.ScenarioContext) *Context {
	t.Helper()

	c := &Context{t: t}
	c.mustBoot(bootOptions{issuerFor: func(addr string) string { return "http://" + addr + "/" }})

	sc.After(func(_ context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		c.shutdown()
		return nil, nil
	})
	return c
}

// RestartWithIssuer stops the scenario's server and boots a fresh one on a
// new port with the given ISSUER_URL value (e.g. "auto"), repointing BaseURL.
func (c *Context) RestartWithIssuer(issuer string) {
	c.shutdown()
	c.mustBoot(bootOptions{issuerFor: func(string) string { return issuer }})
}

// RestartWithSeed stops the scenario's server and boots a fresh one whose
// SEED_FILE holds content. A seed that fails to load is returned as the
// error and leaves no server running.
func (c *Context) RestartWithSeed(content string) error {
	c.shutdown()
	dir, err := os.MkdirTemp(c.t.TempDir(), "seed")
	if err != nil {
		return err
	}
	c.seedPath = filepath.Join(dir, "seed.yaml")
	if err := os.WriteFile(c.seedPath, []byte(content), 0o600); err != nil {
		return err
	}
	return c.boot(bootOptions{
		issuerFor: func(addr string) string { return "http://" + addr + "/" },
		seedFile:  c.seedPath,
	})
}

// ReplaceSeedFile saves content over the seed file the way an editor does
// (write a temp file, rename it over) and reloads it. Nothing sleeps: the
// harness runs no watcher and polls twice instead, as a changed file is acted
// on only once it has been seen on two consecutive polls.
func (c *Context) ReplaceSeedFile(content string) error {
	tmp := c.seedPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, c.seedPath); err != nil {
		return err
	}
	c.seed.Poll(context.Background())
	c.seed.Poll(context.Background())
	return nil
}

// DeleteSeedFile removes the seed file and polls once.
func (c *Context) DeleteSeedFile() error {
	if err := os.Remove(c.seedPath); err != nil {
		return err
	}
	c.seed.Poll(context.Background())
	return nil
}

func (c *Context) mustBoot(o bootOptions) {
	if err := c.boot(o); err != nil {
		c.t.Fatalf("boot: %v", err)
	}
}

// shutdown stops the booted server and waits until it has fully stopped.
func (c *Context) shutdown() {
	if c.cancelBoot == nil {
		return
	}
	c.cancelBoot()
	<-c.bootDone
	c.cancelBoot = nil
}

// boot starts the in-process server on a free port. It fails only when the
// seed file does not load, and then starts nothing.
func (c *Context) boot(o bootOptions) error {
	t := c.t
	addr := freePort(t)
	issuer := o.issuerFor(addr)
	clk := clock.NewControlled()
	ks, err := jwks.NewKeySet(jwks.Config{
		Issuer: issuer, AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour,
		Now: clk.Now,
	})
	if err != nil {
		t.Fatalf("jwks: %v", err)
	}
	openapiSpec, err := spec.Load(api.ManagementOpenAPIJSON)
	if err != nil {
		t.Fatalf("spec: %v", err)
	}
	store := matches.NewStore()
	claimsStore := claims.NewStore()
	claimMappings := claims.NewMappingStore()
	permsStore := permissions.NewStore()
	pkceStore := pkce.NewStore(pkce.WithNow(clk.Now))
	mfaStore := mfa.NewStore(mfa.WithNow(clk.Now))
	validator, err := spec.NewValidator(openapiSpec)
	if err != nil {
		t.Fatalf("spec validator: %v", err)
	}
	var seedMgr *seed.Manager
	if o.seedFile != "" {
		seedMgr, err = seed.NewManager(o.seedFile, validator, seed.Stores{
			Matches:       store,
			Claims:        claimsStore,
			ClaimMappings: claimMappings,
			Permissions:   permsStore,
			MFA:           mfaStore,
		}, zerolog.Nop())
		if err != nil {
			return err
		}
	}
	handler, err := router.New(router.Deps{
		Log:                  zerolog.Nop(),
		Store:                store,
		Claims:               claimsStore,
		ClaimMappings:        claimMappings,
		Permissions:          permsStore,
		PKCE:                 pkceStore,
		MFA:                  mfaStore,
		Keys:                 ks,
		Spec:                 openapiSpec,
		Validator:            validator,
		Clock:                clk,
		Issuer:               issuer,
		DefaultAudience:      "http://" + addr + "/api/v2/",
		SpecValidationStrict: true,
		MaxRequestBodyBytes:  1 << 20, // 1 MiB, matches the production default
		LogoutAllowedURLs:    []string{"https://app/bye"},
		CORSAllowedOrigins:   []string{"http://app.test", "https://*.example.test"},
		EventsReplayBuffer:   50, // small but functional in scenario tests
		Journal:              journal.New(100),
		Seed:                 seedMgr,
	})
	if err != nil {
		t.Fatalf("router: %v", err)
	}
	srv := server.NewHTTP(addr, handler, server.Timeouts{ReadHeader: time.Second, Write: 5 * time.Second, Idle: 30 * time.Second})
	orc := server.NewOrchestrator(srv)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = orc.Start(ctx)
	}()
	waitReachable(t, "http://"+addr+"/admin0/expectations")

	c.BaseURL = "http://" + addr
	c.cancelBoot = cancel
	c.bootDone = done
	c.seed = seedMgr
	return nil
}

// freePort asks the kernel for an available TCP port.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func waitReachable(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("service did not become reachable at %s", url)
}

// MintBearer issues a token via the live /oauth/token endpoint.
func (c *Context) MintBearer() {
	body := strings.NewReader("grant_type=client_credentials&client_id=test&client_secret=x&audience=" + c.BaseURL + "/api/v2/")
	req, _ := http.NewRequest("POST", c.BaseURL+"/oauth/token", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.client().Do(req)
	if err != nil {
		c.t.Fatalf("mint bearer: %v", err)
	}
	defer resp.Body.Close()
	var tr struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&tr)
	c.BearerTok = tr.AccessToken
}

// DoForm sends a POST with application/x-www-form-urlencoded body.
func (c *Context) DoForm(method, path string, form url.Values, withBearer bool) {
	authorization := ""
	if withBearer {
		authorization = "Bearer " + c.BearerTok
	}
	c.DoFormWithAuthorization(method, path, form, authorization)
}

// DoFormWithAuthorization is DoForm with an explicit Authorization header
// value; empty sends none.
func (c *Context) DoFormWithAuthorization(method, path string, form url.Values, authorization string) {
	req, _ := http.NewRequest(method, c.BaseURL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		c.t.Fatalf("do form: %v", err)
	}
	c.LastResp = resp
	c.LastBody, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
}

// VerifyAccessTokenAgainstJWKS fetches the mock's JWKS and verifies the
// token's RS256 signature using golang-jwt + MicahParks/keyfunc.
func (c *Context) VerifyAccessTokenAgainstJWKS(tok string) error {
	k, err := keyfunc.NewDefaultCtx(context.Background(), []string{c.BaseURL + "/.well-known/jwks.json"})
	if err != nil {
		return fmt.Errorf("fetch jwks: %w", err)
	}
	if _, err := jwt.Parse(tok, k.Keyfunc); err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	return nil
}

// Do sends an HTTP request and stores the response on the context.
func (c *Context) Do(method, path string, body string, withBearer bool) {
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, c.BaseURL+path, rdr)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if withBearer {
		req.Header.Set("Authorization", "Bearer "+c.BearerTok)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		c.t.Fatalf("do: %v", err)
	}
	c.LastResp = resp
	c.LastBody, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
}

// MintVia mints a user token through the live /oauth/token endpoint, sending
// the request with the given Host header, and stores it as the bearer. The
// whole token response is left in LastBody so its id_token can be reused.
func (c *Context) MintVia(host string) {
	form := url.Values{
		"grant_type": {"password"}, "client_id": {"demo"}, "username": {"alice@example.com"},
		"password": {"ignored"}, "scope": {"openid"}, "audience": {c.BaseURL + "/api/v2/"},
	}
	req, _ := http.NewRequest("POST", c.BaseURL+"/oauth/token", strings.NewReader(form.Encode()))
	req.Host = host
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.client().Do(req)
	if err != nil {
		c.t.Fatalf("mint via host %s: %v", host, err)
	}
	c.LastResp = resp
	c.LastBody, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	c.BearerTok = gjson.GetBytes(c.LastBody, "access_token").String()
	if c.BearerTok == "" {
		c.t.Fatalf("mint via host %s: no access_token (body=%s)", host, c.LastBody)
	}
}
