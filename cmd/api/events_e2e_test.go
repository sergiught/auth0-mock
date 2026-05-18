package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
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
	"github.com/sergiught/auth0-mock/pkg/auth0mock/auth0mocktest"
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

	// Open the subscription, then push — the helper does keep-alive
	// filtering and frame parsing so the test reads like the user
	// journey: subscribe → push → assert what arrived.
	stream := auth0mocktest.SubscribeEvents(t, c, bearer, "")
	// Let the subscription register before we push.
	time.Sleep(100 * time.Millisecond)

	auth0mocktest.MustPush(t, c, `{
		"type":"user.created","offset":"0",
		"event":{
		  "specversion":"1.0","type":"user.created","source":"https://auth0.local/",
		  "id":"evt_e2eaaaaaaaaaaaaa","time":"2026-05-19T00:00:00Z",
		  "a0tenant":"e2e","a0stream":"est_aaaaaaaaaaaaaaaa",
		  "data":{"object":{"user_id":"u-1","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}
		}
	}`)

	evt := stream.NextEvent(t, 3*time.Second)
	assert.Equal(t, "evt_e2eaaaaaaaaaaaaa", evt.ID)
	assert.Equal(t, "user.created", evt.Type)
	assert.Contains(t, string(evt.Data), `"user_id":"u-1"`)
}

// TestEvents_E2E_SurvivesShortWriteTimeout is a regression for the
// review finding that ResponseController.SetWriteDeadline silently
// failed when the response writer was wrapped in middleware lacking
// Unwrap(). The handler used to swallow the ErrNotSupported and the
// http.Server.WriteTimeout would tear down healthy SSE subscribers.
//
// We boot a real http.Server with WriteTimeout=200ms, open a
// subscriber, idle past 500ms (well over the deadline), then push.
// If the bypass is broken, the connection is dead by the time the
// push lands and the subscriber sees nothing.
func TestEvents_E2E_SurvivesShortWriteTimeout(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
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
		Log: zerolog.Nop(), Store: matches.NewStore(),
		Claims: claims.NewStore(), Permissions: permissions.NewStore(),
		PKCE: pkce.NewStore(pkce.WithNow(clk.Now)),
		MFA:  mfa.NewStore(mfa.WithNow(clk.Now)),
		Keys: keys, Spec: openapiSpec, Validator: validator, Clock: clk,
		Issuer: issuer, DefaultAudience: issuer + "api/v2/",
		EventsReplayBuffer: 10,
	})
	require.NoError(t, err)

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: time.Second,
		WriteTimeout:      200 * time.Millisecond, // The smoking gun.
	}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })

	bearer, err := keys.Mint(jwks.MintOpts{
		Subject: "e2e", Audience: []string{issuer + "api/v2/"}, TTL: time.Hour,
	})
	require.NoError(t, err)
	c, err := auth0mock.NewClient("http://" + addr)
	require.NoError(t, err)

	stream := auth0mocktest.SubscribeEvents(t, c, bearer, "")
	// Sleep past the 200ms WriteTimeout. If the bypass is broken,
	// http.Server kills the response writer here; the next push will
	// land but never reach the subscriber.
	time.Sleep(500 * time.Millisecond)

	auth0mocktest.MustPush(t, c, `{
		"type":"user.created","offset":"0",
		"event":{
		  "specversion":"1.0","type":"user.created","source":"x",
		  "id":"evt_writetimeout0000","time":"2026-05-19T00:00:00Z",
		  "a0tenant":"e2e","a0stream":"est_aaaaaaaaaaaaaaaa",
		  "data":{"object":{"user_id":"u","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}
		}
	}`)
	evt := stream.NextEvent(t, 2*time.Second)
	assert.Equal(t, "evt_writetimeout0000", evt.ID)
}
