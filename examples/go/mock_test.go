package example

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"testing"
)

var mockURL = envOr("AUTH0_MOCK_URL", "https://localhost:8443/")

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// post sends a JSON body to a path on the mock and decodes the JSON reply.
func post(t *testing.T, path string, body, out any) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := url.JoinPath(mockURL, path)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Post(endpoint, "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		t.Fatalf("%s returned %d", path, res.StatusCode)
	}
	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
}

// mintToken asks the mock for a client-credentials access token.
func mintToken(t *testing.T, audience string) string {
	t.Helper()
	var reply struct {
		AccessToken string `json:"access_token"`
	}
	post(t, "oauth/token", map[string]string{
		"grant_type":    "client_credentials",
		"client_id":     "demo",
		"client_secret": "x",
		"audience":      audience,
	}, &reply)
	return reply.AccessToken
}

// stub registers an expectation so the mock answers a request the way you say.
func stub(t *testing.T, expectation any) {
	t.Helper()
	post(t, "admin0/expectations", expectation, nil)
}
