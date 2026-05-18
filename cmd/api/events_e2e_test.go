package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/api"
	"github.com/sergiught/auth0-mock/internal/claims"
	"github.com/sergiught/auth0-mock/internal/clock"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/mfa"
	"github.com/sergiught/auth0-mock/internal/permissions"
	"github.com/sergiught/auth0-mock/internal/pkce"
	"github.com/sergiught/auth0-mock/internal/router"
	"github.com/sergiught/auth0-mock/internal/spec"
	"github.com/sergiught/auth0-mock/pkg/auth0mock"
)

// startE2EServer boots the full mock against an httptest.Server and
// returns (baseURL, bearer). It uses the same wiring as
// cmd/api/main.go's run() so the SUT under test is the production
// handler graph.
func startE2EServer(t *testing.T) (baseURL, bearer string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	issuer := "http://" + addr + "/"

	clk := clock.NewControlled()
	keys, err := jwks.NewKeySet(jwks.Config{
		Issuer:         issuer,
		AccessTokenTTL: time.Hour,
		IDTokenTTL:     time.Hour,
		Now:            clk.Now,
	})
	require.NoError(t, err)
	openapiSpec, err := spec.Load(api.ManagementOpenAPIJSON)
	require.NoError(t, err)
	validator, err := spec.NewValidator(openapiSpec)
	require.NoError(t, err)

	handler, err := router.New(router.Deps{
		Log:                zerolog.Nop(),
		Store:              matches.NewStore(),
		Claims:             claims.NewStore(),
		Permissions:        permissions.NewStore(),
		PKCE:               pkce.NewStore(pkce.WithNow(clk.Now)),
		MFA:                mfa.NewStore(mfa.WithNow(clk.Now)),
		Keys:               keys,
		Spec:               openapiSpec,
		Validator:          validator,
		Clock:              clk,
		Issuer:             issuer,
		DefaultAudience:    issuer + "api/v2/",
		EventsReplayBuffer: 50,
	})
	require.NoError(t, err)

	srv := httptest.NewUnstartedServer(handler)
	require.NoError(t, srv.Listener.Close())
	srv.Listener, err = net.Listen("tcp", addr)
	require.NoError(t, err)
	srv.Start()
	t.Cleanup(srv.Close)

	tok, err := keys.Mint(jwks.MintOpts{
		Subject:  "e2e",
		Audience: []string{issuer + "api/v2/"},
		TTL:      time.Hour,
	})
	require.NoError(t, err)
	return srv.URL, tok
}

func TestEvents_E2E_PushAndReceive(t *testing.T) {
	baseURL, bearer := startE2EServer(t)
	c, err := auth0mock.NewClient(baseURL)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan string, 1)
	go func() {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v2/events", nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			got <- "ERR " + err.Error()
			return
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			got <- "BADSTATUS " + resp.Status + " " + string(b)
			return
		}
		r := bufio.NewReader(resp.Body)
		var frame strings.Builder
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				got <- "READERR " + err.Error()
				return
			}
			// Skip keep-alive comment frames.
			if strings.HasPrefix(line, ":") {
				continue
			}
			frame.WriteString(line)
			if line == "\n" {
				got <- frame.String()
				return
			}
		}
	}()

	time.Sleep(150 * time.Millisecond)
	require.NoError(t, c.Events.Push(context.Background(), auth0mock.Event{
		Type: "user.created",
		ID:   "evt_e2eaaaaaaaaaaaaa",
		Payload: json.RawMessage(`{
		  "type":"user.created","offset":"0",
		  "event":{
		    "specversion":"1.0","type":"user.created","source":"https://auth0.local/",
		    "id":"evt_e2eaaaaaaaaaaaaa","time":"2026-05-19T00:00:00Z",
		    "a0tenant":"e2e","a0stream":"est_aaaaaaaaaaaaaaaa",
		    "data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}
		  }
		}`),
	}))

	select {
	case frame := <-got:
		assert.Contains(t, frame, "id: evt_e2eaaaaaaaaaaaaa")
		assert.Contains(t, frame, "event: user.created")
		assert.Contains(t, frame, `"user_id":"u-1"`)
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for SSE frame")
	}
}
