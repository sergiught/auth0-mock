// Command consumer is a stand-alone example that proves the auth0-mock
// service is a drop-in for Auth0:
//
//   - Mints a token via /oauth/token
//   - Validates the token's signature against /.well-known/jwks.json using
//     the standard jwt library (NOT the mock's internal jwks package).
//   - Registers a mocked Mgmt API response and retrieves it with the token.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

func main() {
	mock := flag.String("mock", "http://localhost:8080", "auth0-mock base URL")
	flag.Parse()

	tok, err := mintToken(*mock)
	must(err)
	fmt.Println("minted token", tok[:40]+"...")

	must(verifyToken(*mock, tok))
	fmt.Println("token signature verified against", *mock+"/.well-known/jwks.json")

	must(registerAndCall(*mock, tok))
	fmt.Println("registered + retrieved a mocked Mgmt API resource")
}

func mintToken(base string) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", "demo")
	form.Set("client_secret", "x")
	form.Set("audience", base+"/api/v2/")
	resp, err := http.PostForm(base+"/oauth/token", form)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("no access_token in response")
	}
	return out.AccessToken, nil
}

func verifyToken(base, tok string) error {
	k, err := keyfunc.NewDefaultCtx(context.Background(), []string{base + "/.well-known/jwks.json"})
	if err != nil {
		return err
	}
	_, err = jwt.Parse(tok, k.Keyfunc)
	return err
}

func registerAndCall(base, tok string) error {
	body := `{"method":"GET","path":"/api/v2/users/auth0|demo","response":{"status":200,"body":{"user_id":"auth0|demo","email":"demo@x"}}}`
	expReq, _ := http.NewRequest("POST", base+"/admin0/expectations", strings.NewReader(body))
	expReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(expReq)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("/admin0/expectations: expected 204, got %d", resp.StatusCode)
	}

	req, _ := http.NewRequest("GET", base+"/api/v2/users/auth0|demo", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		buf, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("expected 200, got %d: %s", resp.StatusCode, string(buf))
	}
	return nil
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
