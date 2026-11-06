package example

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"testing"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestAuth0Mock(t *testing.T) {
	ctx := context.Background()

	// CI sets AUTH0_MOCK_IMAGE to a locally built image; readers get the published one.
	image := os.Getenv("AUTH0_MOCK_IMAGE")
	if image == "" {
		image = "ghcr.io/sergiught/auth0-mock:latest"
	}

	c, err := testcontainers.Run(ctx, image,
		testcontainers.WithExposedPorts("8080/tcp"),
		testcontainers.WithEnv(map[string]string{
			"ADMIN_ALLOW_REMOTE": "true", // the admin API is loopback-only by default; the test runs outside the container
			"HTTPS_ADDR":         "off",  // plain HTTP, no certificate to trust
			"ISSUER_URL":         "auto", // issuer follows the URL the test reaches the mock on
		}),
		// Wait for readiness instead of sleeping.
		testcontainers.WithWaitStrategy(wait.ForHTTP("/healthz").WithPort("8080/tcp")),
	)
	testcontainers.CleanupContainer(t, c)
	if err != nil {
		t.Fatal(err)
	}

	// Ask Testcontainers for the host and the random mapped port; the host is not always this machine.
	base, err := c.PortEndpoint(ctx, "8080/tcp", "http")
	if err != nil {
		t.Fatal(err)
	}

	// Stub the Management API call the app under test will make.
	stub := `{"method":"GET","path":"/api/v2/users/auth0|123","response":{"status":200,"body":{"user_id":"auth0|123","email":"alice@example.com","name":"Alice"}}}`
	resp, err := http.Post(base+"/admin0/expectations", "application/json", bytes.NewBufferString(stub))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("create expectation: status %d", resp.StatusCode)
	}

	// Get a token the way the app does.
	resp, err = http.PostForm(base+"/oauth/token", url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"demo"},
		"client_secret": {"x"},
		"audience":      {base + "/api/v2/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil || tok.AccessToken == "" {
		t.Fatalf("token response: status %d, err %v", resp.StatusCode, err)
	}

	// Call the stubbed endpoint; the "|" is percent-encoded so every HTTP client accepts it.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v2/users/auth0%7C123", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	userResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer userResp.Body.Close()
	if userResp.StatusCode != http.StatusOK {
		t.Fatalf("get user: status %d", userResp.StatusCode)
	}
	var user struct {
		UserID string `json:"user_id"`
		Email  string `json:"email"`
	}
	if err := json.NewDecoder(userResp.Body).Decode(&user); err != nil {
		t.Fatal(err)
	}
	if user.UserID != "auth0|123" || user.Email != "alice@example.com" {
		t.Fatalf("unexpected user: %+v", user)
	}

	// Verify the token as a real app would: discovery, JWKS, RS256 signature, issuer and audience.
	provider, err := oidc.NewProvider(ctx, base+"/")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Verifier(&oidc.Config{ClientID: base + "/api/v2/"}).Verify(ctx, tok.AccessToken); err != nil {
		t.Fatalf("token verification: %v", err)
	}
}
