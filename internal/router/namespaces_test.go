package router_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
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
	"github.com/sergiught/auth0-mock/internal/namespace"
	"github.com/sergiught/auth0-mock/internal/permissions"
	"github.com/sergiught/auth0-mock/internal/pkce"
	"github.com/sergiught/auth0-mock/internal/router"
	"github.com/sergiught/auth0-mock/internal/seed"
	"github.com/sergiught/auth0-mock/internal/spec"
)

const nsIssuer = "https://mock/"

func newNamespaceRouter(t *testing.T, limit int, seedFile string) http.Handler {
	t.Helper()
	return newNamespaceRouterWith(t, limit, seedFile, nil)
}

func newNamespaceRouterWith(t *testing.T, limit int, seedFile string, tweak func(*router.Deps)) http.Handler {
	t.Helper()
	openapiSpec, err := spec.Load(api.ManagementOpenAPIJSON)
	require.NoError(t, err)
	validator, err := spec.NewValidator(openapiSpec)
	require.NoError(t, err)
	clk := clock.NewControlled()
	ks, err := jwks.NewKeySet(jwks.Config{
		Issuer: nsIssuer, AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour, Now: clk.Now,
	})
	require.NoError(t, err)
	store := matches.NewStore()
	claimsStore := claims.NewStore()
	mappings := claims.NewMappingStore()
	perms := permissions.NewStore()
	mfaStore := mfa.NewStore(mfa.WithNow(clk.Now))
	var mgr *seed.Manager
	if seedFile != "" {
		mgr, err = seed.NewManager(seedFile, validator, seed.Stores{
			Matches: store, Claims: claimsStore, ClaimMappings: mappings, Permissions: perms, MFA: mfaStore,
		}, zerolog.Nop())
		require.NoError(t, err)
	}
	deps := router.Deps{
		Log: zerolog.Nop(), Store: store, Claims: claimsStore, ClaimMappings: mappings,
		Permissions: perms, PKCE: pkce.NewStore(pkce.WithNow(clk.Now)), MFA: mfaStore,
		Keys: ks, Spec: openapiSpec, Validator: validator, Clock: clk,
		Issuer: nsIssuer, DefaultAudience: nsIssuer + "api/v2/",
		Journal: journal.New(10), EventsReplayBuffer: 10, Seed: mgr, NamespaceMax: limit,
		CORSAllowedOrigins: []string{"http://localhost:3000"},
	}
	if tweak != nil {
		tweak(&deps)
	}
	h, err := router.New(deps)
	require.NoError(t, err)
	return h
}

// nsDo sends a request to h; ns is "" (default), "a" (prefix) or "header:b".
func nsDo(t *testing.T, h http.Handler, method, path, ns, body string, hdr ...string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	switch {
	case strings.HasPrefix(ns, "header:"):
	case ns != "":
		path = "/ns/" + ns + path
	}
	req := httptest.NewRequest(method, path, rdr)
	if strings.HasPrefix(ns, "header:") {
		req.Header.Set(namespace.Header, strings.TrimPrefix(ns, "header:"))
	}
	if strings.HasPrefix(body, "grant_type=") {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func nsToken(t *testing.T, h http.Handler, ns string) string {
	t.Helper()
	rec := nsDo(t, h, http.MethodPost, "/oauth/token", ns,
		"grant_type=client_credentials&client_id=c&client_secret=s&audience="+nsIssuer+"api/v2/")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out.AccessToken
}

func jwtClaims(t *testing.T, tok string) map[string]any {
	t.Helper()
	parts := strings.Split(tok, ".")
	require.Len(t, parts, 3)
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	return m
}

func stub(t *testing.T, h http.Handler, ns, name string) {
	t.Helper()
	rec := nsDo(t, h, http.MethodPost, "/admin0/expectations", ns,
		`{"method":"GET","path":"/api/v2/users/1","response":{"status":200,"body":{"name":"`+name+`"}}}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
}

func getUser(t *testing.T, h http.Handler, ns string) *httptest.ResponseRecorder {
	t.Helper()
	return nsDo(t, h, http.MethodGet, "/api/v2/users/1", ns, "", "Authorization", "Bearer "+nsToken(t, h, ns))
}

func TestNamespaces_StubsIsolated(t *testing.T) {
	t.Parallel()
	h := newNamespaceRouter(t, 3, "")
	stub(t, h, "a", "alpha")
	stub(t, h, "header:b", "beta")

	assert.Contains(t, getUser(t, h, "a").Body.String(), "alpha")
	assert.Contains(t, getUser(t, h, "header:b").Body.String(), "beta")
	assert.Equal(t, http.StatusNotFound, getUser(t, h, "").Code)
}

func TestNamespaces_ResetIsolated(t *testing.T) {
	t.Parallel()
	h := newNamespaceRouter(t, 3, "")
	stub(t, h, "", "default")
	stub(t, h, "a", "alpha")

	require.Equal(t, http.StatusNoContent, nsDo(t, h, http.MethodPost, "/admin0/reset", "", "").Code)
	assert.Contains(t, getUser(t, h, "a").Body.String(), "alpha")
	assert.Equal(t, http.StatusNotFound, getUser(t, h, "").Code)

	stub(t, h, "", "default")
	require.Equal(t, http.StatusNoContent, nsDo(t, h, http.MethodPost, "/admin0/reset", "a", "").Code)
	assert.Contains(t, getUser(t, h, "").Body.String(), "default")
	assert.Equal(t, http.StatusNotFound, getUser(t, h, "a").Code)
}

func TestNamespaces_ClockIsolated(t *testing.T) {
	t.Parallel()
	h := newNamespaceRouter(t, 3, "")
	rec := nsDo(t, h, http.MethodPut, "/admin0/clock", "a", `{"now":"2030-01-01T00:00:00Z"}`)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	iat := func(ns string) time.Time {
		return time.Unix(int64(jwtClaims(t, nsToken(t, h, ns))["iat"].(float64)), 0).UTC()
	}
	assert.Equal(t, time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), iat("a"))
	assert.WithinDuration(t, time.Now(), iat(""), time.Minute)
	assert.WithinDuration(t, time.Now(), iat("header:b"), time.Minute)
}

func TestNamespaces_PrefixIssuer(t *testing.T) {
	t.Parallel()
	h := newNamespaceRouter(t, 3, "")
	tok := nsToken(t, h, "a")
	assert.Equal(t, "https://mock/ns/a/", jwtClaims(t, tok)["iss"])

	stub(t, h, "a", "alpha")
	assert.Equal(t, http.StatusOK, getUser(t, h, "a").Code)

	rec := nsDo(t, h, http.MethodGet, "/.well-known/openid-configuration", "a", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "https://mock/ns/a/")
}

func TestNamespaces_SeedApplied(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "seed.yaml")
	require.NoError(t, os.WriteFile(path, []byte("expectations:\n  - method: GET\n    path: /api/v2/users/1\n    response: {status: 200, body: {name: seeded}}\n"), 0o600))
	h := newNamespaceRouter(t, 3, path)

	assert.Contains(t, getUser(t, h, "a").Body.String(), "seeded")
	require.Equal(t, http.StatusNoContent, nsDo(t, h, http.MethodPost, "/admin0/reset?keep_seed=false", "a", "").Code)
	assert.Equal(t, http.StatusNotFound, getUser(t, h, "a").Code)
	assert.Contains(t, getUser(t, h, "").Body.String(), "seeded")
}

func TestNamespaces_SeedApplied_PlainResetReseeds(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "seed.yaml")
	require.NoError(t, os.WriteFile(path, []byte("expectations:\n  - method: GET\n    path: /api/v2/users/1\n    response: {status: 200, body: {name: seeded}}\n"), 0o600))
	h := newNamespaceRouter(t, 3, path)

	rec := nsDo(t, h, http.MethodPost, "/admin0/expectations", "a",
		`{"method":"GET","path":"/api/v2/users/2","response":{"status":200,"body":{"name":"extra"}}}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	tok := nsToken(t, h, "a")
	get := func(p string) *httptest.ResponseRecorder {
		return nsDo(t, h, http.MethodGet, p, "a", "", "Authorization", "Bearer "+tok)
	}
	require.Equal(t, http.StatusOK, get("/api/v2/users/2").Code)

	require.Equal(t, http.StatusNoContent, nsDo(t, h, http.MethodPost, "/admin0/reset", "a", "").Code)
	assert.Contains(t, get("/api/v2/users/1").Body.String(), "seeded")
	assert.Equal(t, http.StatusNotFound, get("/api/v2/users/2").Code)
}

func TestNamespaces_JournalIsolated(t *testing.T) {
	t.Parallel()
	h := newNamespaceRouter(t, 3, "")
	tokA, tokD := nsToken(t, h, "a"), nsToken(t, h, "")
	nsDo(t, h, http.MethodGet, "/api/v2/users/in-a", "a", "", "Authorization", "Bearer "+tokA)
	nsDo(t, h, http.MethodGet, "/api/v2/users/in-default", "", "", "Authorization", "Bearer "+tokD)

	inA := nsDo(t, h, http.MethodGet, "/admin0/journal", "a", "").Body.String()
	inDef := nsDo(t, h, http.MethodGet, "/admin0/journal", "", "").Body.String()
	assert.Contains(t, inA, "in-a")
	assert.NotContains(t, inA, "in-default")
	assert.Contains(t, inDef, "in-default")
	assert.NotContains(t, inDef, "in-a")
}

func TestNamespaces_AuthorizeErrorsIsolated(t *testing.T) {
	t.Parallel()
	h := newNamespaceRouter(t, 3, "")
	rec := nsDo(t, h, http.MethodPost, "/admin0/authorize-errors", "a",
		`{"match":{"client_id":"abc"},"error":"access_denied"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	const q = "/authorize?client_id=abc&redirect_uri=https%3A%2F%2Fapp%2Fcb&state=s1&response_type=code"
	inA := nsDo(t, h, http.MethodGet, q, "a", "")
	assert.Contains(t, inA.Header().Get("Location"), "error=access_denied")
	for _, ns := range []string{"", "header:b"} {
		assert.NotContains(t, nsDo(t, h, http.MethodGet, q, ns, "").Header().Get("Location"), "access_denied", ns)
	}
}

func TestNamespaces_DeleteClosesEvents(t *testing.T) {
	t.Parallel()
	h := newNamespaceRouter(t, 3, "")
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	tok := nsToken(t, h, "a")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/ns/a/api/v2/events", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusOK, resp.StatusCode)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(io.Discard, resp.Body)
	}()
	del, err := http.NewRequestWithContext(ctx, http.MethodDelete, srv.URL+"/admin0/namespaces/a", nil)
	require.NoError(t, err)
	dr, err := http.DefaultClient.Do(del)
	require.NoError(t, err)
	_ = dr.Body.Close()
	require.Equal(t, http.StatusNoContent, dr.StatusCode)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("event stream did not end after namespace delete")
	}
}

func TestNamespaces_Disabled(t *testing.T) {
	t.Parallel()
	h := newNamespaceRouter(t, 0, "")
	assert.Equal(t, http.StatusNotFound, nsDo(t, h, http.MethodPost, "/oauth/token", "a",
		"grant_type=client_credentials&client_id=c&client_secret=s").Code)

	stub(t, h, "", "default")
	assert.Contains(t, getUser(t, h, "header:b").Body.String(), "default")
}

func TestNamespaces_DispatcherErrorsGetMiddleware(t *testing.T) {
	h := newNamespaceRouter(t, 5, "")

	rec := nsDo(t, h, http.MethodGet, "/oauth/token", "header:bad id!", "",
		"Origin", "http://localhost:3000")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "http://localhost:3000", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.NotEmpty(t, rec.Header().Get("X-Request-Id"))
}

func TestNamespaces_LifecyclePreflight(t *testing.T) {
	h := newNamespaceRouter(t, 5, "")

	rec := nsDo(t, h, http.MethodOptions, "/admin0/namespaces/a", "", "",
		"Origin", "http://localhost:3000", "Access-Control-Request-Method", "DELETE")
	assert.NotEqual(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, "http://localhost:3000", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, rec.Header().Get("Access-Control-Allow-Methods"), "DELETE")
}

func TestNamespaces_DispatcherErrorsBoundDebugBody(t *testing.T) {
	var logs bytes.Buffer
	h := newNamespaceRouterWith(t, 5, "", func(d *router.Deps) {
		d.Debug = true
		d.Log = zerolog.New(&logs)
		d.MaxRequestBodyBytes = 64
	})
	rec := nsDo(t, h, http.MethodPost, "/oauth/token", "header:bad id!", strings.Repeat("X", 20000))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	// DebugDump reads the body it logs; the limit must cap that read.
	assert.Contains(t, logs.String(), `"body_bytes":64`)
	assert.NotContains(t, logs.String(), `"body_bytes":20000`)
}

func TestNamespaceMFAUserKeyDecodedOnce(t *testing.T) {
	h := newNamespaceRouter(t, 3, "")
	cases := []struct{ path, key string }{
		{"50%25off", "50%off"},
		{"a%2Fb", "a/b"},
		{"auth0%7C123", "auth0|123"},
		{"auth0%7c123", "auth0|123"},
	}
	for _, c := range cases {
		rec := nsDo(t, h, http.MethodPut, "/admin0/mfa/users/"+c.path, "a", `{"authenticators":[{"type":"otp"}]}`)
		require.Equal(t, http.StatusOK, rec.Code, c.path+" "+rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"user":`+strconv.Quote(c.key))
	}
}

func TestNamespaces_LoginPageIsolated(t *testing.T) {
	t.Parallel()
	h := newNamespaceRouterWith(t, 3, "", func(d *router.Deps) { d.LoginPage = true })
	const q = "/authorize?client_id=app&redirect_uri=https%3A%2F%2Fapp%2Fcb&state=s1&response_type=code"
	start := func() string {
		loc := nsDo(t, h, http.MethodGet, q, "t1", "").Header().Get("Location")
		require.True(t, strings.HasPrefix(loc, "u/login?state="), loc)
		return loc
	}
	post := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("username=jane%40example.com&password=pw&action=default"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := post("/ns/t1/" + start())
	require.Equal(t, http.StatusFound, rec.Code, rec.Body.String())
	assert.True(t, strings.HasPrefix(rec.Header().Get("Location"), "https://app/cb?code="))

	rec = post("/" + start())
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestNamespaces_LoginPageIssuerMatchesAutoApprove(t *testing.T) {
	t.Parallel()
	const q = "/authorize?client_id=app&redirect_uri=https%3A%2F%2Fapp%2Fcb&state=s1&response_type=code&scope=openid"
	idIssuer := func(h http.Handler, loc string, rec *httptest.ResponseRecorder) string {
		t.Helper()
		if rec != nil {
			req := httptest.NewRequest(http.MethodPost, "/ns/t1/"+loc, strings.NewReader("username=jane%40example.com&password=pw&action=default"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			h.ServeHTTP(rec, req)
		}
		u, err := url.Parse(rec.Header().Get("Location"))
		require.NoError(t, err)
		code := u.Query().Get("code")
		require.NotEmpty(t, code, rec.Header().Get("Location"))
		tok := nsDo(t, h, http.MethodPost, "/oauth/token", "t1",
			"grant_type=authorization_code&client_id=app&redirect_uri=https%3A%2F%2Fapp%2Fcb&code="+code)
		require.Equal(t, http.StatusOK, tok.Code, tok.Body.String())
		var out struct {
			IDToken string `json:"id_token"`
		}
		require.NoError(t, json.Unmarshal(tok.Body.Bytes(), &out))
		iss, _ := jwtClaims(t, out.IDToken)["iss"].(string)
		return iss
	}

	on := newNamespaceRouterWith(t, 3, "", func(d *router.Deps) { d.LoginPage = true })
	loc := nsDo(t, on, http.MethodGet, q, "t1", "").Header().Get("Location")
	pageIss := idIssuer(on, loc, httptest.NewRecorder())

	off := newNamespaceRouter(t, 3, "")
	autoIss := idIssuer(off, "", nsDo(t, off, http.MethodGet, q, "t1", ""))

	assert.Equal(t, "https://mock/ns/t1/", autoIss)
	assert.Equal(t, autoIss, pageIss)
}
