---
title: Go quickstart
description: Test a Go API and a Management API client against the mock with go-jwt-middleware and the go-auth0 SDK.
sidebar:
  label: Go
  order: 4
---

Test a Go API and a Management API client against the mock, using Auth0's `go-jwt-middleware` and `go-auth0` SDK.

This page is about testing your own Go code. For auth0-mock's own typed client and in-process helpers, see the [Go SDK guide](/auth0-mock/guides/go-sdk/).

## Start the mock

```bash
mkdir -p .auth0-mock
docker run -d --rm --name auth0-mock --user "$(id -u):$(id -g)" \
  -p 127.0.0.1:8443:8443 -p 127.0.0.1:8080:8080 \
  -e ADMIN_ALLOW_REMOTE=true -e TLS_CACHE_DIR=/tls -v "$PWD/.auth0-mock:/tls" \
  ghcr.io/sergiught/auth0-mock:latest
until [ -s .auth0-mock/tls.crt ] && curl -sf -o /dev/null --cacert .auth0-mock/tls.crt https://localhost:8443/healthz; do sleep 0.5; done
```

The `--user` flag lets the container write the certificate into your `.auth0-mock/` folder.
Run `mkdir -p .auth0-mock` first, otherwise Docker creates the folder as root and the mock cannot write to it.
If the loop never finishes, check `docker logs auth0-mock`.
Add `.auth0-mock/` to your own `.gitignore`: it holds the mock's private key.

## Different port

If you map a different host port (for example `-p 127.0.0.1:9443:8443`), also set `-e ISSUER_URL=https://localhost:9443/` and `AUTH0_MOCK_URL=https://localhost:9443/`. Otherwise the `iss` claim in tokens will not match the URL you call. Use that port in the commands below too.
From release 0.234.0 you can set `-e ISSUER_URL=auto` instead of the fixed `ISSUER_URL`. The mock then builds the issuer from the address of each request, so any mapped port works. `AUTH0_MOCK_URL` still has to match the port you mapped.

## Trust its certificate

The mock serves HTTPS with a self-signed certificate. Run `go mod download` first, then tell Go to trust it:

```bash
export SSL_CERT_FILE="$PWD/.auth0-mock/tls.crt"
```

Without it, the first call to the mock fails with `x509: certificate signed by unknown authority`.

On macOS and Windows, Go uses `SSL_CERT_FILE` only with Go 1.27 or newer, and only if your `go.mod` says `go 1.27` or later or you set `GODEBUG=x509sslcertoverrideplatform=1`. Otherwise add `.auth0-mock/tls.crt` to the system trust store.

Download modules first because, with `SSL_CERT_FILE` set, Go may trust only the mock's certificate (on macOS and Windows, when the override above is in effect), and fetching modules then fails. If that happens, run `env -u SSL_CERT_FILE go mod download`.

## Your API checks tokens

The API from [`examples/go/app.go`](https://github.com/sergiught/auth0-mock/blob/main/examples/go/app.go) protects `GET /private` with `go-jwt-middleware`. It loads the signing keys from the issuer's JWKS and accepts only RS256 tokens with the right issuer and audience:

```go
package example

import (
	"encoding/json"
	"net/http"
	"net/url"

	jwtmiddleware "github.com/auth0/go-jwt-middleware/v3"
	"github.com/auth0/go-jwt-middleware/v3/jwks"
	"github.com/auth0/go-jwt-middleware/v3/validator"
)

// NewApp serves GET /private, which only accepts access tokens issued by
// issuerURL for the given audience.
func NewApp(issuerURL, audience string) (http.Handler, error) {
	issuer, err := url.Parse(issuerURL)
	if err != nil {
		return nil, err
	}
	// The provider fetches the signing keys from the issuer's JWKS.
	provider, err := jwks.NewCachingProvider(jwks.WithIssuerURL(issuer))
	if err != nil {
		return nil, err
	}
	jwtValidator, err := validator.New(
		validator.WithKeyFunc(provider.KeyFunc),
		validator.WithAlgorithm(validator.RS256),
		validator.WithIssuer(issuer.String()),
		validator.WithAudience(audience),
	)
	if err != nil {
		return nil, err
	}
	middleware, err := jwtmiddleware.New(jwtmiddleware.WithValidator(jwtValidator))
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle("GET /private", middleware.CheckJWT(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, err := jwtmiddleware.GetClaims[*validator.ValidatedClaims](r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"sub": claims.RegisteredClaims.Subject})
	})))
	return mux, nil
}
```

The tests get a token from the mock with `mintToken` in [`examples/go/mock_test.go`](https://github.com/sergiught/auth0-mock/blob/main/examples/go/mock_test.go):

```go
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
```

Point the API at the mock and check the good, missing and wrong-audience cases ([`examples/go/api_test.go`](https://github.com/sergiught/auth0-mock/blob/main/examples/go/api_test.go)):

```go
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
```

## Your app calls the Management API

Stub the response with `stub` from `mock_test.go`, shown above. Then call it through the `go-auth0` management client ([`examples/go/management_test.go`](https://github.com/sergiught/auth0-mock/blob/main/examples/go/management_test.go)). `client.New` takes the mock's host, and `option.WithClientCredentials` makes the client fetch its own token from the mock using the client ID and secret:

```go
package example

import (
	"context"
	"net/url"
	"testing"

	client "github.com/auth0/go-auth0/v3/management/client"
	"github.com/auth0/go-auth0/v3/management/option"
)

func TestSDKReadsAStubbedUser(t *testing.T) {
	stub(t, map[string]any{
		"method": "GET",
		"path":   "/api/v2/users/auth0|123",
		"response": map[string]any{
			"status": 200,
			"body":   map[string]string{"user_id": "auth0|123", "email": "alice@example.com"},
		},
	})

	mock, err := url.Parse(mockURL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	mgmt, err := client.New(mock.Host, option.WithClientCredentials(ctx, "demo", "x"))
	if err != nil {
		t.Fatal(err)
	}
	user, err := mgmt.Users.Get(ctx, "auth0|123", nil)
	if err != nil {
		t.Fatal(err)
	}

	if got := *user.UserID; got != "auth0|123" {
		t.Fatalf("user_id = %q", got)
	}
	if got := *user.Email; got != "alice@example.com" {
		t.Fatalf("email = %q", got)
	}
}
```

## Run the example

From a clone of this repo: download the modules once, then run the tests. CI does the same with `make test-examples`.

```bash
cd examples/go
go mod download
SSL_CERT_FILE=../../.auth0-mock/tls.crt go test ./...
```

On macOS or Windows, use this instead:

```bash
GODEBUG=x509sslcertoverrideplatform=1 SSL_CERT_FILE=../../.auth0-mock/tls.crt go test ./...
```

The full files are in [`examples/go/`](https://github.com/sergiught/auth0-mock/tree/main/examples/go/).

## Stop the mock

```bash
docker stop auth0-mock
```

With `--rm` this also removes the container.

## Other languages

- [curl quickstart](/auth0-mock/quickstarts/http/)
- [Node.js quickstart](/auth0-mock/quickstarts/node/)
- [Python quickstart](/auth0-mock/quickstarts/python/)
