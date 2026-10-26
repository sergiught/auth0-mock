package router_test

import (
	"context"
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
	"github.com/sergiught/auth0-mock/internal/journal"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/permissions"
	"github.com/sergiught/auth0-mock/internal/router"
	"github.com/sergiught/auth0-mock/internal/spec"
)

// TestNew_DefaultsClaimMappings pins the off-by-default safety net: a
// Deps that never wires a ClaimMappings store must still serve the
// /admin0/claims/mappings routes (empty map = feature off) instead of
// mounting them over a nil store that panics on first request.
func TestNew_DefaultsClaimMappings(t *testing.T) {
	t.Parallel()
	openapiSpec, err := spec.Load(api.ManagementOpenAPIJSON)
	require.NoError(t, err)
	validator, err := spec.NewValidator(openapiSpec)
	require.NoError(t, err)
	ks, err := jwks.NewKeySet(jwks.Config{
		Issuer: "https://mock/", AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour,
	})
	require.NoError(t, err)

	h, err := router.New(router.Deps{
		Log:             zerolog.Nop(),
		Store:           matches.NewStore(),
		Claims:          claims.NewStore(),
		Permissions:     permissions.NewStore(),
		Keys:            ks,
		Spec:            openapiSpec,
		Validator:       validator,
		Issuer:          "https://mock/",
		DefaultAudience: "https://mock/api/v2/",
		// ClaimMappings deliberately omitted.
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin0/claims/mappings", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{}`, w.Body.String())
}

// TestNew_JournalRecordsRecoveredPanic pins Recovery inside the journal
// middleware: a handler panic (here, a nil match store) still leaves a 500
// entry in the journal.
func TestNew_JournalRecordsRecoveredPanic(t *testing.T) {
	t.Parallel()
	openapiSpec, err := spec.Load(api.ManagementOpenAPIJSON)
	require.NoError(t, err)
	validator, err := spec.NewValidator(openapiSpec)
	require.NoError(t, err)
	ks, err := jwks.NewKeySet(jwks.Config{
		Issuer: "https://mock/", AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour,
	})
	require.NoError(t, err)
	j := journal.New(10)

	h, err := router.New(router.Deps{
		Log:             zerolog.Nop(),
		Claims:          claims.NewStore(),
		Permissions:     permissions.NewStore(),
		Keys:            ks,
		Spec:            openapiSpec,
		Validator:       validator,
		Issuer:          "https://mock/",
		DefaultAudience: "https://mock/api/v2/",
		Journal:         j,
		// Store deliberately omitted so the lookup panics.
	})
	require.NoError(t, err)
	tok, err := ks.Mint(jwks.MintOpts{Subject: "test", Audience: []string{"a"}, TTL: time.Hour})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/users/auth0|1", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)

	entries := j.List(journal.Filter{})
	require.Len(t, entries, 1)
	assert.Equal(t, http.StatusInternalServerError, entries[0].Status)
	assert.Equal(t, journal.OutcomeRejected, entries[0].Outcome)
}

// newCORSRouter builds a router with the given CORS allow-list and returns
// it with its journal and a valid bearer token.
func newCORSRouter(t *testing.T, origins []string) (http.Handler, *journal.Journal, string) {
	t.Helper()
	openapiSpec, err := spec.Load(api.ManagementOpenAPIJSON)
	require.NoError(t, err)
	validator, err := spec.NewValidator(openapiSpec)
	require.NoError(t, err)
	ks, err := jwks.NewKeySet(jwks.Config{
		Issuer: "https://mock/", AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour,
	})
	require.NoError(t, err)
	j := journal.New(10)

	h, err := router.New(router.Deps{
		Log:                zerolog.Nop(),
		Store:              matches.NewStore(),
		Claims:             claims.NewStore(),
		Permissions:        permissions.NewStore(),
		Keys:               ks,
		Spec:               openapiSpec,
		Validator:          validator,
		Issuer:             "https://mock/",
		DefaultAudience:    "https://mock/api/v2/",
		Journal:            j,
		EventsReplayBuffer: 10,
		CORSAllowedOrigins: origins,
	})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	tok, err := ks.Mint(jwks.MintOpts{Subject: "test", Audience: []string{"a"}, TTL: time.Hour})
	require.NoError(t, err)
	return h, j, tok
}

// TestRouter_CORSPreflightSkipsJournal pins CORS ahead of the journal: a
// preflight is answered without leaving an entry that would pollute
// /admin0/journal/unmatched.
func TestRouter_CORSPreflightSkipsJournal(t *testing.T) {
	t.Parallel()
	h, j, _ := newCORSRouter(t, nil)

	req := httptest.NewRequest(http.MethodOptions, "/api/v2/users", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "http://localhost:3000", w.Header().Get("Access-Control-Allow-Origin"))
	for _, e := range j.List(journal.Filter{}) {
		assert.False(t, e.Method == http.MethodOptions && e.Path == "/api/v2/users",
			"preflight was journaled: %+v", e)
	}
}

// TestRouter_CORSOnErrorResponses pins that error bodies carry the CORS
// headers, otherwise the browser hides them from the SPA.
func TestRouter_CORSOnErrorResponses(t *testing.T) {
	t.Parallel()
	h, _, tok := newCORSRouter(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/userinfo", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Authorization", "Bearer nope")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "http://localhost:3000", w.Header().Get("Access-Control-Allow-Origin"))

	req = httptest.NewRequest(http.MethodGet, "/api/v2/users", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Authorization", "Bearer "+tok)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "http://localhost:3000", w.Header().Get("Access-Control-Allow-Origin"))
}

// TestRouter_CORSStubVaryKeepsOrigin pins that a registered Vary header is
// merged with, not replacing, the Vary: Origin the CORS middleware sets.
func TestRouter_CORSStubVaryKeepsOrigin(t *testing.T) {
	t.Parallel()
	h, _, tok := newCORSRouter(t, nil)

	stub := `{"method":"GET","path":"/api/v2/users/auth0|v","response":{"status":200,` +
		`"headers":{"Vary":"Accept-Encoding"},"body":{"user_id":"auth0|v"}}}`
	req := httptest.NewRequest(http.MethodPost, "/admin0/expectations", strings.NewReader(stub))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Less(t, w.Code, 300, w.Body.String())

	req = httptest.NewRequest(http.MethodGet, "/api/v2/users/auth0|v", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.ElementsMatch(t, []string{"Origin", "Accept-Encoding"}, w.Header().Values("Vary"))
}

// TestRouter_CORSEventsStillStream pins that the CORS middleware does not
// buffer the SSE stream.
func TestRouter_CORSEventsStillStream(t *testing.T) {
	t.Parallel()
	h, _, tok := newCORSRouter(t, nil)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v2/events", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "http://localhost:3000", resp.Header.Get("Access-Control-Allow-Origin"))
	buf := make([]byte, 1)
	n, err := resp.Body.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

// TestRouter_CORSInvalidOriginFailsStartup pins that a bad allow-list entry
// is a router construction error naming the entry.
func TestRouter_CORSInvalidOriginFailsStartup(t *testing.T) {
	t.Parallel()
	openapiSpec, err := spec.Load(api.ManagementOpenAPIJSON)
	require.NoError(t, err)
	validator, err := spec.NewValidator(openapiSpec)
	require.NoError(t, err)
	ks, err := jwks.NewKeySet(jwks.Config{
		Issuer: "https://mock/", AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour,
	})
	require.NoError(t, err)

	_, err = router.New(router.Deps{
		Log:                zerolog.Nop(),
		Store:              matches.NewStore(),
		Claims:             claims.NewStore(),
		Permissions:        permissions.NewStore(),
		Keys:               ks,
		Spec:               openapiSpec,
		Validator:          validator,
		Issuer:             "https://mock/",
		DefaultAudience:    "https://mock/api/v2/",
		CORSAllowedOrigins: []string{"https://a.*.com"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "https://a.*.com")
}
