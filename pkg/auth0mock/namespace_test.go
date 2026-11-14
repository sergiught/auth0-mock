package auth0mock_test

import (
	"encoding/json"
	"io"
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
	"github.com/sergiught/auth0-mock/internal/journal"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/mfa"
	"github.com/sergiught/auth0-mock/internal/permissions"
	"github.com/sergiught/auth0-mock/internal/pkce"
	"github.com/sergiught/auth0-mock/internal/router"
	"github.com/sergiught/auth0-mock/internal/spec"
	"github.com/sergiught/auth0-mock/pkg/auth0mock"
)

// newNamespaceServer runs the real router with namespaces enabled.
func newNamespaceServer(t *testing.T) (*httptest.Server, *auth0mock.Client) {
	t.Helper()
	const issuer = "https://mock/"
	openapiSpec, err := spec.Load(api.ManagementOpenAPIJSON)
	require.NoError(t, err)
	validator, err := spec.NewValidator(openapiSpec)
	require.NoError(t, err)
	clk := clock.NewControlled()
	ks, err := jwks.NewKeySet(jwks.Config{Issuer: issuer, AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour, Now: clk.Now})
	require.NoError(t, err)
	h, err := router.New(router.Deps{
		Log: zerolog.Nop(), Store: matches.NewStore(), Claims: claims.NewStore(), ClaimMappings: claims.NewMappingStore(),
		Permissions: permissions.NewStore(), PKCE: pkce.NewStore(pkce.WithNow(clk.Now)), MFA: mfa.NewStore(mfa.WithNow(clk.Now)),
		Keys: ks, Spec: openapiSpec, Validator: validator, Clock: clk,
		Issuer: issuer, DefaultAudience: issuer + "api/v2/",
		Journal: journal.New(10), EventsReplayBuffer: 10, NamespaceMax: 4,
	})
	require.NoError(t, err)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := auth0mock.NewClient(srv.URL)
	require.NoError(t, err)
	return srv, c
}

func mintToken(t *testing.T, base string) string {
	t.Helper()
	return mintTokenWith(t, http.DefaultClient, base)
}

func mintTokenWith(t *testing.T, hc *http.Client, base string) string {
	t.Helper()
	resp, err := hc.Post(base+"/oauth/token", "application/x-www-form-urlencoded",
		strings.NewReader("grant_type=client_credentials&client_id=c&client_secret=s&audience=https://mock/api/v2/"))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.NotEmpty(t, out.AccessToken)
	return out.AccessToken
}

// getStatus GETs path under base+prefix with a token minted in that same
// scope (a prefix-mode token's issuer must match the prefix it is used under).
func getStatus(t *testing.T, hc *http.Client, base, prefix, path string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+prefix+path, http.NoBody)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+mintToken(t, base+prefix))
	resp, err := hc.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestNamespace_IsolatedExpectations(t *testing.T) {
	t.Parallel()
	srv, c := newNamespaceServer(t)
	ns, err := c.Namespace("t1")
	require.NoError(t, err)
	assert.Equal(t, "t1", ns.NamespaceID())
	assert.Equal(t, "", c.NamespaceID())

	_, err = ns.ExpectGet("/api/v2/users/1").Respond(200).JSON(map[string]any{"name": "stub"}).Apply(t.Context())
	require.NoError(t, err)

	code, _ := getStatus(t, http.DefaultClient, srv.URL, "", "/api/v2/users/1")
	assert.Equal(t, http.StatusNotFound, code)
	code, body := getStatus(t, http.DefaultClient, srv.URL, "/ns/t1", "/api/v2/users/1")
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, "stub")
}

func TestNamespace_TransportRoutesSUT(t *testing.T) {
	t.Parallel()
	srv, c := newNamespaceServer(t)
	ns, err := c.Namespace("t1")
	require.NoError(t, err)
	_, err = ns.ExpectGet("/api/v2/users/1").Respond(200).JSON(map[string]any{"name": "stub"}).Apply(t.Context())
	require.NoError(t, err)

	sut := &http.Client{Transport: ns.Transport(nil)}
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v2/users/1", http.NoBody)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+mintTokenWith(t, sut, srv.URL))
	resp, err := sut.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, req.Header.Get(auth0mock.NamespaceHeader), "caller's request must not be mutated")

	assert.Equal(t, http.DefaultTransport, c.Transport(nil), "root client leaves the transport unchanged")
}

func TestNamespace_Delete(t *testing.T) {
	t.Parallel()
	srv, c := newNamespaceServer(t)
	ns, err := c.Namespace("t1")
	require.NoError(t, err)
	_, err = ns.ExpectGet("/api/v2/users/1").Respond(200).JSON(map[string]any{"name": "stub"}).Apply(t.Context())
	require.NoError(t, err)

	require.NoError(t, ns.DeleteNamespace(t.Context()))
	code, _ := getStatus(t, http.DefaultClient, srv.URL, "/ns/t1", "/api/v2/users/1")
	assert.Equal(t, http.StatusNotFound, code)
	require.NoError(t, ns.DeleteNamespace(t.Context()))
}

func TestNamespace_InvalidAndNested(t *testing.T) {
	t.Parallel()
	_, c := newNamespaceServer(t)
	_, err := c.Namespace("a.b")
	require.Error(t, err)
	ns, err := c.Namespace("ok")
	require.NoError(t, err)
	_, err = ns.Namespace("x")
	require.Error(t, err)
	require.Error(t, c.DeleteNamespace(t.Context()))
}

func TestNamespace_ResetAndVerifyScoped(t *testing.T) {
	t.Parallel()
	srv, c := newNamespaceServer(t)
	_, err := c.ExpectGet("/api/v2/users/1").Respond(200).JSON(map[string]any{"name": "stub"}).Apply(t.Context())
	require.NoError(t, err)
	ns, err := c.Namespace("t1")
	require.NoError(t, err)

	require.NoError(t, ns.Reset(t.Context()))
	code, _ := getStatus(t, http.DefaultClient, srv.URL, "", "/api/v2/users/1")
	assert.Equal(t, http.StatusOK, code, "namespace reset must not touch the root stub")

	require.NoError(t, ns.Expectations.Verify(t.Context()))
	re, err := ns.ExpectGet("/api/v2/users/1").Respond(200).JSON(map[string]any{"name": "stub"}).Apply(t.Context())
	require.NoError(t, err)
	re.Times(1)
	require.Error(t, ns.Expectations.Verify(t.Context()))
}

type idleRT struct{ closed bool }

func (r *idleRT) RoundTrip(*http.Request) (*http.Response, error) { return nil, io.EOF }
func (r *idleRT) CloseIdleConnections()                           { r.closed = true }

func TestNamespace_TransportCloseIdleConnections(t *testing.T) {
	t.Parallel()
	_, c := newNamespaceServer(t)
	ns, err := c.Namespace("t1")
	require.NoError(t, err)
	base := &idleRT{}
	(&http.Client{Transport: ns.Transport(base)}).CloseIdleConnections()
	assert.True(t, base.closed)
}
