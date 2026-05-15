// Command consumer is a stand-alone example that proves auth0-mock is a
// drop-in for Auth0 when driven by the official go-auth0 SDK.
//
// It runs three steps against a running mock:
//
//  1. mintToken          - mint an access token with the SDK's authentication
//     client (client_credentials grant against /oauth/token).
//  2. verifyToken        - validate the token's signature against the mock's
//     published JWKS, using the same jwt library a real
//     Auth0-consuming service would use.
//  3. createAndRead*     - create and read back a client application and a
//     user with the SDK's management client, with the
//     Management API responses stubbed via /admin0/expectations.
//
// The example lives in its own Go module so the go-auth0 SDK never leaks into
// the auth0-mock module graph.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
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
	// The go-auth0 SDK only speaks HTTPS, so target the mock's TLS listener.
	mockURL := flag.String("mock", "https://localhost:8443", "auth0-mock base URL (HTTPS)")
	flag.Parse()

	ctx := context.Background()
	httpClient := insecureHTTPClient()
	domain := strings.TrimPrefix(*mockURL, "https://")

	// Step 1: mint an access token with the SDK's authentication client.
	token, err := mintToken(ctx, domain, *mockURL+"/api/v2/", httpClient)
	if err != nil {
		return fmt.Errorf("mint token: %w", err)
	}
	fmt.Println("minted token via go-auth0 authentication SDK:", preview(token, 40))

	// Step 2: verify the token's signature against the mock's published JWKS.
	if err := verifyToken(ctx, *mockURL, token, httpClient); err != nil {
		return fmt.Errorf("verify token signature: %w", err)
	}
	fmt.Println("token signature verified against", *mockURL+"/.well-known/jwks.json")

	// Step 3: drive the Management API with the SDK's management client.
	api, err := management.New(domain,
		management.WithStaticToken(token),
		management.WithClient(httpClient),
	)
	if err != nil {
		return fmt.Errorf("create management client: %w", err)
	}

	clientID, err := createAndReadClient(ctx, *mockURL, api, httpClient)
	if err != nil {
		return fmt.Errorf("client round-trip: %w", err)
	}
	fmt.Println("created + read back a client application:", clientID)

	userID, err := createAndReadUser(ctx, *mockURL, api, httpClient)
	if err != nil {
		return fmt.Errorf("user round-trip: %w", err)
	}
	fmt.Println("created + read back a user:", userID)

	return nil
}

// insecureHTTPClient returns an HTTP client that trusts the mock's self-signed
// certificate. A real deployment would trust the cert properly instead - see
// the TLS section of the repo README.
func insecureHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed mock cert
		},
	}
}

// mintToken runs the client_credentials grant against the mock's /oauth/token
// endpoint using the go-auth0 authentication client.
func mintToken(ctx context.Context, domain, audience string, hc *http.Client) (string, error) {
	auth, err := authentication.New(ctx, domain,
		authentication.WithClientID("demo"),
		authentication.WithClientSecret("x"),
		authentication.WithClient(hc),
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

// verifyToken validates the token signature against the mock's published JWKS,
// using the same libraries a real Auth0-consuming service would use.
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

	if _, err := jwt.Parse(token, verifier.Keyfunc); err != nil {
		return fmt.Errorf("parse token: %w", err)
	}
	return nil
}

// createAndReadClient stubs the Management API, then creates and reads back a
// client application through the go-auth0 management client.
func createAndReadClient(ctx context.Context, base string, api *management.Management, hc *http.Client) (string, error) {
	const clientID = "demo-client-id"

	// The mock returns this same object for both the create and the read.
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

	created := &management.Client{
		Name:    auth0.String("Demo App"),
		AppType: auth0.String("non_interactive"),
	}
	if err := api.Client.Create(ctx, created); err != nil {
		return "", fmt.Errorf("Client.Create: %w", err)
	}

	got, err := api.Client.Read(ctx, created.GetClientID())
	if err != nil {
		return "", fmt.Errorf("Client.Read: %w", err)
	}
	return got.GetClientID(), nil
}

// createAndReadUser stubs the Management API, then creates and reads back a
// user through the go-auth0 management client.
func createAndReadUser(ctx context.Context, base string, api *management.Management, hc *http.Client) (string, error) {
	const userID = "auth0|demo"

	// The mock returns this same object for both the create and the read.
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

// expectation is the payload for the mock's POST /admin0/expectations
// control-plane endpoint: "when you receive Method+Path, reply with Response".
type expectation struct {
	Method   string       `json:"method"`
	Path     string       `json:"path"`
	Response stubResponse `json:"response"`
}

// stubResponse is the canned HTTP response the mock should return.
type stubResponse struct {
	Status int `json:"status"`
	Body   any `json:"body"`
}

// registerExpectations posts each expectation to the mock, stopping at the
// first failure.
func registerExpectations(base string, hc *http.Client, exps ...expectation) error {
	for _, exp := range exps {
		if err := registerExpectation(base, hc, exp); err != nil {
			return err
		}
	}
	return nil
}

// registerExpectation posts a single expectation to the mock. /admin0 is the
// mock's own test-setup API, not part of Auth0, so the go-auth0 SDK has no
// concept of it - it's a plain HTTP call.
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

// preview shortens a long string for display, appending an ellipsis when
// truncated.
func preview(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
