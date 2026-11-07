package example

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const audience = "https://my-api.example.com"

func get(t *testing.T, srv *httptest.Server, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/private", nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	app, err := NewApp(mockURL, audience)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app)
	t.Cleanup(srv.Close)
	return srv
}

func TestTokenFromTheMockIsAccepted(t *testing.T) {
	srv := newServer(t)
	res := get(t, srv, mintToken(t, audience))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	var body struct{ Sub string }
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Sub != "demo@clients" {
		t.Fatalf("sub = %q, want demo@clients", body.Sub)
	}
}

func TestMissingTokenIsRejected(t *testing.T) {
	res := get(t, newServer(t), "")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", res.StatusCode)
	}
}

func TestTokenForAnotherAPIIsRejected(t *testing.T) {
	res := get(t, newServer(t), mintToken(t, "https://other.example.com"))
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", res.StatusCode)
	}
}
