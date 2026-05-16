// Command consumer is a stand-alone example that proves auth0-mock is a
// drop-in for Auth0 when driven by the official go-auth0 SDK — same SDK
// calls, same response shapes, no code changes between the mock and the
// real thing.
//
// The mock has two distinct layers, and this example exercises both:
//
//	┌─────────────────────────────────────────────────────────────────┐
//	│                 auth0-mock (a single binary)                    │
//	├──────────────────────────────┬──────────────────────────────────┤
//	│  Auth API  —  REAL           │  Management API  —  STUB-DRIVEN  │
//	│                              │                                  │
//	│  /oauth/token                │  /api/v2/*   (400+ endpoints)    │
//	│  /.well-known/jwks.json      │                                  │
//	│  /.well-known/openid-        │  Replies with whatever you       │
//	│    configuration             │  registered via the mock's       │
//	│                              │  POST /admin0/expectations       │
//	│  Mints REAL RS256 JWTs       │  control plane                   │
//	└──────────────┬───────────────┴───────────────┬──────────────────┘
//	               ▲                               ▲
//	               │                               │
//	      go-auth0/authentication           go-auth0/management
//
// The three phases below mirror that diagram:
//
//  1. mintToken                     — runs the client_credentials grant
//     through the SDK's authentication client and receives a genuine
//     signed JWT.
//  2. verifyToken                   — re-validates that JWT against the
//     mock's JWKS the same way a downstream service would, proving the
//     token would also pass against real Auth0.
//  3. createAndRead{Client,User}    — stubs Management API responses via
//     /admin0/expectations, then drives the SDK's management client
//     through create/read pairs. From the SDK's point of view it's
//     talking to Auth0; it has no idea the responses are canned.
//
// The example lives in its own Go module so the go-auth0 SDK never leaks
// into the auth0-mock module graph.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/MicahParks/jwkset"
	"github.com/MicahParks/keyfunc/v3"
	"github.com/auth0/go-auth0"
	"github.com/auth0/go-auth0/authentication"
	"github.com/auth0/go-auth0/authentication/oauth"
	"github.com/auth0/go-auth0/management"
	"github.com/golang-jwt/jwt/v5"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// The go-auth0 SDK only speaks HTTPS, so target the mock's TLS listener
	// (:8443 by default), not the HTTP one.
	//
	// TLS trust comes in two flavours:
	//   -cert <path>   →  load that PEM into a RootCAs pool and do real
	//                     certificate verification. Use this when the mock
	//                     is launched with TLS_CACHE_DIR=<dir> (the cert
	//                     lives at <dir>/tls.crt). `make demo` wires this
	//                     up automatically.
	//   no -cert       →  fall back to InsecureSkipVerify, the lazy path
	//                     for ad-hoc runs against any local mock. Fine for
	//                     a demo, never for production.
	mockURL := flag.String("mock", "https://localhost:8443", "auth0-mock base URL (HTTPS)")
	certFile := flag.String("cert", "", "PEM file containing the mock's TLS cert; if empty, skip verification")
	flag.Parse()

	ctx := context.Background()
	httpClient, err := newHTTPClient(*certFile)
	if err != nil {
		return fmt.Errorf("build HTTP client: %w", err)
	}
	domain := strings.TrimPrefix(*mockURL, "https://")

	// ── Phase 1 ──────────────────────────────────────────────────────────
	// Hit POST /oauth/token through the SDK's authentication client. The
	// mock generates a fresh RS256 keypair on boot and signs every token
	// with it — what comes back is a fully-valid JWT, structurally identical
	// to one Auth0 would produce. No placeholders, no magic strings.
	//
	// Side effect worth knowing: authentication.New() fetches
	// /.well-known/openid-configuration during construction, so this call
	// also sanity-checks the mock's OIDC discovery document.
	token, err := mintToken(ctx, domain, *mockURL+"/api/v2/", httpClient)
	if err != nil {
		return fmt.Errorf("mint token: %w", err)
	}
	step(1, "Minted access token via go-auth0 authentication SDK", preview(token, 40))

	// ── Phase 2 ──────────────────────────────────────────────────────────
	// Re-verify the token signature with MicahParks/keyfunc + golang-jwt/jwt
	// — the same OSS libraries a real Auth0 consumer (your API, your other
	// microservice) would use. The SDK already trusts /oauth/token; this
	// step exists to demonstrate the value proposition: if signature
	// verification works against the mock, it'll work against real Auth0,
	// because tokens are *real* JWTs end to end.
	if err := verifyToken(ctx, *mockURL, token, httpClient); err != nil {
		return fmt.Errorf("verify token signature: %w", err)
	}
	step(2, "Verified token signature against the mock's JWKS",
		*mockURL+"/.well-known/jwks.json")

	// ── Phase 3 ──────────────────────────────────────────────────────────
	// Stand up a management client and run a create/read round-trip for a
	// client application and a user. Unlike the Auth API, the Management
	// API in the mock is stub-driven: 400+ endpoints from the Auth0 OpenAPI
	// spec exist, but they hold no real state. We tell the mock what to
	// return for each (method, path) by POSTing to /admin0/expectations,
	// and the SDK then makes its normal API calls and gets those canned
	// responses back — without ever knowing.
	api, err := management.New(domain,
		management.WithStaticToken(token),
		management.WithClient(httpClient),
	)
	if err != nil {
		return fmt.Errorf("new management client: %w", err)
	}

	clientID, err := createAndReadClient(ctx, *mockURL, api, httpClient)
	if err != nil {
		return fmt.Errorf("client round-trip: %w", err)
	}
	userID, err := createAndReadUser(ctx, *mockURL, api, httpClient)
	if err != nil {
		return fmt.Errorf("user round-trip: %w", err)
	}
	step(3, "Drove the Management API through the management SDK",
		"created + read back client: "+clientID,
		"created + read back user:   "+userID,
	)

	fmt.Println()
	fmt.Println("Done. go-auth0 SDK works against auth0-mock unchanged.")
	return nil
}

// mintToken runs the client_credentials grant against /oauth/token via the
// go-auth0 authentication client.
//
// The clientID/secret are unused by the mock in its default config — the
// mock signs the token with its own key regardless — but they have to be
// present because the SDK requires them.
func mintToken(ctx context.Context, domain, audience string, hc *http.Client) (string, error) {
	auth, err := authentication.New(ctx, domain,
		authentication.WithClientID("demo"),
		authentication.WithClientSecret("x"),
		authentication.WithClient(hc), // hand the mock-trusting HTTP client to the SDK
	)
	if err != nil {
		return "", fmt.Errorf("new authentication client: %w", err)
	}

	tokens, err := auth.OAuth.LoginWithClientCredentials(ctx, oauth.LoginWithClientCredentialsRequest{
		Audience: audience,
	}, oauth.IDTokenValidationOptions{})
	if err != nil {
		return "", fmt.Errorf("client credentials login: %w", err)
	}
	if tokens.AccessToken == "" {
		return "", fmt.Errorf("response contained no access_token")
	}
	return tokens.AccessToken, nil
}

// verifyToken validates the token signature against the mock's published
// JWKS, using the same library combination a real Auth0-consuming service
// would reach for.
//
// The three steps below mirror what any JWT-verifying service does on every
// request: fetch the JWK set, build a keyfunc that resolves the right key
// per JWT "kid", then ask jwt.Parse to enforce the signature.
func verifyToken(ctx context.Context, base, token string, hc *http.Client) error {
	storage, err := jwkset.NewStorageFromHTTP(base+"/.well-known/jwks.json", jwkset.HTTPClientStorageOptions{
		Client: hc,
		Ctx:    ctx,
	})
	if err != nil {
		return fmt.Errorf("load JWKS: %w", err)
	}

	verifier, err := keyfunc.New(keyfunc.Options{Storage: storage})
	if err != nil {
		return fmt.Errorf("build keyfunc: %w", err)
	}

	// jwt.Parse runs the full signature check; we don't care about the
	// parsed claims here, only that verification succeeded.
	if _, err := jwt.Parse(token, verifier.Keyfunc); err != nil {
		return fmt.Errorf("parse token: %w", err)
	}
	return nil
}

// createAndReadClient stubs the Management API, then exercises a typical
// create/read round-trip for a client application through the go-auth0
// management client.
//
// Two expectations are registered up-front:
//
//	POST /api/v2/clients          → 201, returns `stub`
//	GET  /api/v2/clients/<id>     → 200, returns `stub`
//
// The SDK then makes those exact calls believing it's talking to Auth0,
// and unmarshals the canned response bodies into typed management.Client
// values. The same `stub` is reused for both responses — mirroring how
// Auth0 echoes the resource back from a create and from a subsequent get.
func createAndReadClient(ctx context.Context, base string, api *management.Management, hc *http.Client) (string, error) {
	const clientID = "demo-client-id"
	stub := &management.Client{
		ClientID: auth0.String(clientID),
		Name:     auth0.String("Demo App"),
		AppType:  auth0.String("non_interactive"),
	}
	if err := registerExpectations(base, hc,
		expectation{Method: http.MethodPost, Path: "/api/v2/clients", Response: stubResponse{Status: http.StatusCreated, Body: stub}},
		expectation{Method: http.MethodGet, Path: "/api/v2/clients/" + clientID, Response: stubResponse{Status: http.StatusOK, Body: stub}},
	); err != nil {
		return "", err
	}

	// Client.Create marshals a request body, POSTs it, and unmarshals the
	// stubbed 201 response back into `created` — after this call,
	// created.GetClientID() is the id the mock handed back.
	created := &management.Client{
		Name:    auth0.String("Demo App"),
		AppType: auth0.String("non_interactive"),
	}
	if err := api.Client.Create(ctx, created); err != nil {
		return "", fmt.Errorf("Client.Create: %w", err)
	}

	// Client.Read confirms the id round-trips cleanly through the mock.
	got, err := api.Client.Read(ctx, created.GetClientID())
	if err != nil {
		return "", fmt.Errorf("Client.Read: %w", err)
	}
	return got.GetClientID(), nil
}

// createAndReadUser is the mirror of createAndReadClient for /api/v2/users.
// The shape is identical: stub two (method, path) pairs, then run the SDK
// calls.
func createAndReadUser(ctx context.Context, base string, api *management.Management, hc *http.Client) (string, error) {
	const userID = "auth0|demo"
	stub := &management.User{
		ID:         auth0.String(userID),
		Email:      auth0.String("demo@example.com"),
		Connection: auth0.String("Username-Password-Authentication"),
	}
	if err := registerExpectations(base, hc,
		expectation{Method: http.MethodPost, Path: "/api/v2/users", Response: stubResponse{Status: http.StatusCreated, Body: stub}},
		expectation{Method: http.MethodGet, Path: "/api/v2/users/" + userID, Response: stubResponse{Status: http.StatusOK, Body: stub}},
	); err != nil {
		return "", err
	}

	created := &management.User{
		Connection: auth0.String("Username-Password-Authentication"),
		Email:      auth0.String("demo@example.com"),
		Password:   auth0.String("Sup3rSecret!"),
	}
	if err := api.User.Create(ctx, created); err != nil {
		return "", fmt.Errorf("User.Create: %w", err)
	}

	got, err := api.User.Read(ctx, created.GetID())
	if err != nil {
		return "", fmt.Errorf("User.Read: %w", err)
	}
	return got.GetID(), nil
}

// ─────────────────────────────────────────────────────────────────────────
// Mock control plane (/admin0)
//
// The types and helpers below cover the mock's own test-setup API. They are
// NOT part of Auth0 — the go-auth0 SDK doesn't know /admin0 exists. Think
// of them as the "given/when" half of a test, with the SDK calls above as
// the "act/assert" half.
// ─────────────────────────────────────────────────────────────────────────

// expectation is the payload for POST /admin0/expectations: "when you
// receive Method+Path, reply with Response".
type expectation struct {
	Method   string       `json:"method"`
	Path     string       `json:"path"`
	Response stubResponse `json:"response"`
}

// stubResponse is the canned HTTP response the mock should return for a
// matching expectation.
type stubResponse struct {
	Status int `json:"status"`
	Body   any `json:"body"`
}

// registerExpectations posts each expectation in order, stopping at the
// first failure. Useful for paired expectations like POST + GET.
func registerExpectations(base string, hc *http.Client, exps ...expectation) error {
	for _, exp := range exps {
		if err := registerExpectation(base, hc, exp); err != nil {
			return err
		}
	}
	return nil
}

// registerExpectation posts a single expectation to the mock.
func registerExpectation(base string, hc *http.Client, exp expectation) error {
	payload, err := json.Marshal(exp)
	if err != nil {
		return fmt.Errorf("marshal expectation: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, base+"/admin0/expectations", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("post expectation: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("expectation for %s %s rejected: got HTTP %d, want 204: %s",
			exp.Method, exp.Path, resp.StatusCode, body)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────
// Utilities
// ─────────────────────────────────────────────────────────────────────────

// newHTTPClient builds an HTTP client suitable for talking to the mock.
//
// If certFile is non-empty, the file is loaded as a PEM-encoded
// certificate and added to the client's RootCAs pool — the normal
// "trust this CA / leaf" pattern, with full hostname and chain
// verification. This is what `make demo` uses: the mock writes its
// auto-generated cert to TLS_CACHE_DIR, and the example points at the
// same file.
//
// If certFile is empty the client falls back to InsecureSkipVerify so
// the example still works against any local mock without setup. Never
// use this mode against anything you actually care about.
func newHTTPClient(certFile string) (*http.Client, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS13}

	if certFile == "" {
		tlsCfg.InsecureSkipVerify = true //nolint:gosec // explicit opt-out, see godoc above
	} else {
		pem, err := os.ReadFile(certFile)
		if err != nil {
			return nil, fmt.Errorf("read cert file %s: %w", certFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("no PEM certificates found in %s", certFile)
		}
		tlsCfg.RootCAs = pool
	}

	return &http.Client{Transport: &http.Transport{TLSClientConfig: tlsCfg}}, nil
}

// step prints a numbered phase header followed by zero or more indented
// detail lines. Pure cosmetics — makes the run() output readable as a
// narrative.
func step(n int, header string, details ...string) {
	fmt.Printf("[%d/3] %s\n", n, header)
	for _, d := range details {
		fmt.Printf("      %s\n", d)
	}
}

// preview shortens a long string for display, appending an ellipsis when
// truncated.
func preview(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
