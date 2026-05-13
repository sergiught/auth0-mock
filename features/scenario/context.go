// Package scenario holds the godog test harness: a per-scenario context that
// boots the auth0-mock service in-process on a random port and provides HTTP
// helpers for the .feature step files.
package scenario

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/rs/zerolog"

	"github.com/sergiught/auth0-mock/api"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/router"
	"github.com/sergiught/auth0-mock/internal/server"
	"github.com/sergiught/auth0-mock/internal/spec"
)

// Context is the per-scenario state passed to step definitions.
type Context struct {
	t          *testing.T
	BaseURL    string
	BearerTok  string
	LastResp   *http.Response
	LastBody   []byte
	cancelBoot context.CancelFunc
}

// New constructs a fresh Context, boots the service in-process on a random
// localhost port, and registers cleanup hooks via godog.
func New(t *testing.T, sc *godog.ScenarioContext) *Context {
	t.Helper()

	addr := freePort(t)
	ks, err := jwks.NewKeySet(jwks.Config{
		Issuer: "http://" + addr + "/", AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("jwks: %v", err)
	}
	openapiSpec, err := spec.Load(api.ManagementOpenAPIJSON)
	if err != nil {
		t.Fatalf("spec: %v", err)
	}
	store := matches.NewStore()
	handler, err := router.New(router.Deps{
		Log:                  zerolog.Nop(),
		Store:                store,
		Keys:                 ks,
		Spec:                 openapiSpec,
		Validator:            spec.NewValidator(openapiSpec),
		Issuer:               "http://" + addr + "/",
		DefaultAudience:      "http://" + addr + "/api/v2/",
		SpecValidationStrict: true,
	})
	if err != nil {
		t.Fatalf("router: %v", err)
	}
	srv := server.NewHTTP(addr, handler, time.Second)
	orc := server.NewOrchestrator(srv)

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = orc.Start(ctx) }()
	waitReachable(t, "http://"+addr+"/admin0/matches")

	c := &Context{t: t, BaseURL: "http://" + addr, cancelBoot: cancel}

	sc.After(func(_ context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		c.cancelBoot()
		return nil, nil
	})
	return c
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
	resp, err := http.DefaultClient.Do(req)
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
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("do: %v", err)
	}
	c.LastResp = resp
	c.LastBody, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
}
