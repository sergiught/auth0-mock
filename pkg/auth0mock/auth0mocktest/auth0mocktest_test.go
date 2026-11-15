package auth0mocktest_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
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
	"github.com/sergiught/auth0-mock/pkg/auth0mock/auth0mocktest"
)

// captureRecorder is a local copy of the recorder pattern used in
// pkg/auth0mock's own tests — duplicated here rather than exported
// from the SDK because it's pure test scaffolding (and the SDK
// shouldn't ship public test helpers that aren't testing.TB-aware).
type captureRecorder struct {
	mu      sync.Mutex
	calls   int
	respond func(w http.ResponseWriter, r *http.Request)
}

func (c *captureRecorder) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		c.mu.Lock()
		c.calls++
		c.mu.Unlock()
		if c.respond != nil {
			c.respond(w, r)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (c *captureRecorder) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func newStub(t *testing.T) (*captureRecorder, *auth0mock.Client) {
	t.Helper()
	rec := &captureRecorder{}
	srv := httptest.NewServer(rec.handler())
	t.Cleanup(srv.Close)

	// Per-test Transport so the idle-connection pool can be drained
	// explicitly before the server shuts down — see the comment on
	// the sibling newStub in pkg/auth0mock/expectations_test.go for
	// the full rationale (Go 1.26 keep-alive race with srv.Close).
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)

	c, err := auth0mock.NewClient(srv.URL,
		auth0mock.WithHTTPClient(&http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		}))
	require.NoError(t, err)
	return rec, c
}

// fakeT is the minimal testing.TB surface the helpers actually use.
// Lets us assert that ResetOnCleanup wired t.Cleanup correctly without
// having to drive a real *testing.T from inside another test.
type fakeT struct {
	testing.TB
	fatalCalled  atomic.Bool
	fatalMessage string
	fatalText    string // Fully formatted message.
	cleanups     []func()
}

func (f *fakeT) Helper()      {}
func (f *fakeT) Name() string { return "fake" }
func (f *fakeT) Fatalf(format string, args ...any) {
	f.fatalCalled.Store(true)
	f.fatalText = fmt.Sprintf(format, args...)
	if len(args) > 0 {
		f.fatalMessage = format
	}
}
func (f *fakeT) Cleanup(fn func()) { f.cleanups = append(f.cleanups, fn) }

func TestResetOnCleanup_HappyPath(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)

	ft := &fakeT{}
	auth0mocktest.ResetOnCleanup(ft, c)

	assert.Equal(t, 1, rec.count(), "pre-test reset must fire immediately")
	assert.False(t, ft.fatalCalled.Load())
	require.Len(t, ft.cleanups, 1, "cleanup callback must be registered")

	ft.cleanups[0]()
	assert.Equal(t, 2, rec.count(), "cleanup must POST /admin0/reset a second time")
}

func TestResetOnCleanup_FatalsOnPreReset(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}

	ft := &fakeT{}
	auth0mocktest.ResetOnCleanup(ft, c)
	assert.True(t, ft.fatalCalled.Load(), "a failing pre-test reset must Fatalf so the test bails before running setup")
}

func TestResetOnCleanup_CleanupSwallowsErrors(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)

	ft := &fakeT{}
	auth0mocktest.ResetOnCleanup(ft, c)
	require.Len(t, ft.cleanups, 1)

	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}
	ft.fatalCalled.Store(false)
	require.NotPanics(t, ft.cleanups[0])
	assert.False(t, ft.fatalCalled.Load(),
		"cleanup must not Fatalf — by the time it runs, the test verdict is already decided")
}

func TestMustReset_FatalsOnError(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}

	ft := &fakeT{}
	auth0mocktest.MustReset(ft, c)
	assert.True(t, ft.fatalCalled.Load())
}

func TestMustApply_HappyAndError(t *testing.T) {
	t.Parallel()

	_, c := newStub(t)
	ft := &fakeT{}
	auth0mocktest.MustApply(ft, c.ExpectGet("/p").Respond(200).JSON(map[string]any{"ok": true}))
	assert.False(t, ft.fatalCalled.Load())

	rec2, c2 := newStub(t)
	rec2.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"statusCode":400,"errorCode":"invalid_body","message":"bad"}`))
	}
	ft2 := &fakeT{}
	auth0mocktest.MustApply(ft2, c2.ExpectGet("/p").Respond(200))
	assert.True(t, ft2.fatalCalled.Load())
}

func TestMustAdd(t *testing.T) {
	t.Parallel()
	_, c := newStub(t)
	ft := &fakeT{}
	auth0mocktest.MustAdd(ft, c, auth0mock.Expectation{
		Method: "GET", Path: "/p",
		Response: auth0mock.ResponseDef{Status: 200},
	})
	assert.False(t, ft.fatalCalled.Load())
}

// TestBracket_CatchesUnmetConstraint is the real test for Bracket's
// cleanup ordering. Sets Times(1) on a stub, never hits it, then
// fires the cleanups in real LIFO order (last-registered first).
// If Bracket registers them in the wrong order — Reset runs before
// Verify — Reset's call to dropRegistered() empties the ledger and
// Verify silently passes. With the right order, Verify fires first,
// sees the unmet constraint, and Fatalfs.
func TestBracket_CatchesUnmetConstraint(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	// Return a server-shaped {"id":"abc"} on POST so Add gets a real
	// handle. The default 204 leaves the ID empty and Verify can't
	// distinguish "cleared" from "never registered".
	rec.respond = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"abc"}`))
		case http.MethodGet:
			// List response — return the stub with zero hits so
			// the Times(1) constraint reads as violated.
			_, _ = w.Write([]byte(`{"expectations":[{"id":"abc","method":"GET","path":"/p","response":{"status":200},"hits":0}]}`))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}

	ft := &fakeT{}
	auth0mocktest.Bracket(ft, c)

	// Register a stub with Times(1) — but never let anything "hit" it
	// (List returns hits=0 above). Verify should fail.
	reg, err := c.Expectations.Add(t.Context(), auth0mock.Expectation{
		Method: "GET", Path: "/p",
		Response: auth0mock.ResponseDef{Status: 200},
	})
	require.NoError(t, err)
	reg.Times(1)

	// Fire cleanups in real LIFO order: last-registered first.
	require.Len(t, ft.cleanups, 2)
	for _, cleanup := range slices.Backward(ft.cleanups) {
		cleanup()
	}

	assert.True(t, ft.fatalCalled.Load(),
		"Bracket must register Verify cleanup LAST so it fires FIRST (before Reset wipes the ledger); a Times(1) constraint with zero hits should Fatalf")
	assert.Contains(t, ft.fatalMessage, "auth0mocktest: verify expectations",
		"Fatalf should come from MustVerify, not from a downstream Reset")
}

// TestBracket_HappyPath sanity-checks that Bracket doesn't Fatalf
// when every constraint is satisfied (or none is set).
func TestBracket_HappyPath(t *testing.T) {
	t.Parallel()
	_, c := newStub(t)

	ft := &fakeT{}
	auth0mocktest.Bracket(ft, c)
	// No constraints set → both cleanups succeed.

	require.Len(t, ft.cleanups, 2)
	for _, cleanup := range slices.Backward(ft.cleanups) {
		cleanup()
	}
	assert.False(t, ft.fatalCalled.Load())
}

func journalEntryJSON(id, method, path string, status int, outcome, near string) string {
	return `{"id":"` + id + `","receivedAt":"2026-10-25T13:04:05.123Z","method":"` + method + `","path":"` + path +
		`","status":` + fmt.Sprint(status) + `,"outcome":"` + outcome + `"` + near + `}`
}

func TestMustFindCall_ReturnsMostRecent(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"entries":[` +
			journalEntryJSON("old", "PATCH", "/api/v2/users/1", 200, "matched", "") + `,` +
			journalEntryJSON("new", "PATCH", "/api/v2/users/1", 200, "matched", "") + `]}`))
	}

	ft := &fakeT{}
	got := auth0mocktest.MustFindCall(ft, c, "PATCH", "/api/v2/users/1")
	assert.False(t, ft.fatalCalled.Load())
	assert.Equal(t, "new", got.ID)
}

func TestMustFindCall_FailsListingSameMethodCalls(t *testing.T) {
	t.Parallel()
	near := `,"nearMisses":[{"expectationId":"e1","method":"PATCH","path":"/api/v2/users/{id}",` +
		`"differences":[{"field":"body.blocked","reason":"mismatch","want":true,"got":false}]}]`
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("path") != "" {
			_, _ = w.Write([]byte(`{"entries":[]}`))
			return
		}
		var entries []string
		for i := 1; i <= 7; i++ {
			n := ""
			if i == 7 {
				n = near
			}
			entries = append(entries, journalEntryJSON(fmt.Sprint(i), "PATCH", fmt.Sprintf("/api/v2/users/%d", i), 404, "no_match", n))
		}
		_, _ = w.Write([]byte(`{"entries":[` + strings.Join(entries, ",") + `]}`))
	}

	ft := &fakeT{}
	auth0mocktest.MustFindCall(ft, c, "PATCH", "/api/v2/users/123")
	require.True(t, ft.fatalCalled.Load())
	msg := ft.fatalText
	assert.Contains(t, msg, "auth0mocktest: no PATCH /api/v2/users/123 call recorded; recent PATCH calls:")
	assert.Contains(t, msg, "PATCH /api/v2/users/7 → 404 no_match at 13:04:05.123")
	assert.Contains(t, msg, "body.blocked: want true, got false")
	assert.Contains(t, msg, "/api/v2/users/3 ")
	assert.NotContains(t, msg, "/api/v2/users/2 ", "only the newest 5 are listed")
}

func TestMustFindCall_FailsWithNone(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"entries":[]}`))
	}

	ft := &fakeT{}
	auth0mocktest.MustFindCall(ft, c, "GET", "/api/v2/users")
	require.True(t, ft.fatalCalled.Load())
	assert.Contains(t, ft.fatalText, "(none)")
}

// newNamespaceMock runs the real router with room for many namespaces.
func newNamespaceMock(t *testing.T) (*httptest.Server, *auth0mock.Client) {
	t.Helper()
	return newNamespaceMockMax(t, 16)
}

func newNamespaceMockMax(t *testing.T, namespaceMax int) (*httptest.Server, *auth0mock.Client) {
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
		Journal: journal.New(10), EventsReplayBuffer: 10, NamespaceMax: namespaceMax,
	})
	require.NoError(t, err)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := auth0mock.NewClient(srv.URL)
	require.NoError(t, err)
	return srv, c
}

func TestIsolate_ParallelSubtests(t *testing.T) {
	srv, root := newNamespaceMock(t)

	t.Run("group", func(t *testing.T) {
		for i := range 8 {
			t.Run(fmt.Sprintf("case %d", i), func(t *testing.T) {
				t.Parallel()
				ns := auth0mocktest.Isolate(t, root)
				want := fmt.Sprintf("body-%d", i)
				auth0mocktest.MustApply(t, ns.ExpectGet("/api/v2/users/1").
					Respond(200).JSON(map[string]any{"name": want}))

				hc := &http.Client{Transport: ns.Transport(nil)}
				tok, err := hc.Post(srv.URL+"/oauth/token", "application/x-www-form-urlencoded",
					strings.NewReader("grant_type=client_credentials&client_id=c&client_secret=s&audience=https://mock/api/v2/"))
				require.NoError(t, err)
				var tokBody struct {
					AccessToken string `json:"access_token"`
				}
				require.NoError(t, json.NewDecoder(tok.Body).Decode(&tokBody))
				_ = tok.Body.Close()
				req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v2/users/1", http.NoBody)
				require.NoError(t, err)
				req.Header.Set("Authorization", "Bearer "+tokBody.AccessToken)
				resp, err := hc.Do(req)
				require.NoError(t, err)
				defer func() { _ = resp.Body.Close() }()
				var got map[string]string
				require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
				assert.Equal(t, want, got["name"])
			})
		}
	})

	resp, err := http.Get(srv.URL + "/admin0/namespaces")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var list []map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&list))
	assert.Empty(t, list)
}

func TestIsolate_IDShape(t *testing.T) {
	_, root := newNamespaceMock(t)
	t.Run("a name/with spaces "+strings.Repeat("x", 100), func(t *testing.T) {
		ns := auth0mocktest.Isolate(t, root)
		assert.Regexp(t, regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`), ns.NamespaceID())
	})
}

func TestIsolate_FailsWhenNamespacesOff(t *testing.T) {
	_, root := newNamespaceMockMax(t, 0)
	ft := &fakeT{}
	auth0mocktest.Isolate(ft, root)
	require.True(t, ft.fatalCalled.Load())
	assert.Contains(t, ft.fatalText, "the mock does not serve namespaces")
}

// exitT stops the calling goroutine on Fatalf like a real *testing.T, so a
// test can see which cleanups were registered before the failure.
type exitT struct {
	fakeT
	errorText string
}

func (e *exitT) Fatalf(format string, args ...any) {
	e.fakeT.Fatalf(format, args...)
	runtime.Goexit()
}

func (e *exitT) Errorf(format string, args ...any) { e.errorText = fmt.Sprintf(format, args...) }

func runIsolate(t *testing.T, et *exitT, c *auth0mock.Client) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		auth0mocktest.Isolate(et, c)
	}()
	<-done
}

func TestIsolate_FailsAtCapWithServerMessage(t *testing.T) {
	_, root := newNamespaceMockMax(t, 1)
	taken, err := root.Namespace("taken")
	require.NoError(t, err)
	_, err = taken.Expectations.List(t.Context())
	require.NoError(t, err)

	et := &exitT{}
	runIsolate(t, et, root)
	require.True(t, et.fatalCalled.Load())
	assert.Contains(t, et.fatalText, "auth0mocktest: Isolate:")
	assert.NotContains(t, et.fatalText, "does not serve namespaces")
	// Only the delete cleanup is registered; verify is not.
	assert.Len(t, et.cleanups, 1)
}

func TestIsolate_FailsOnNamespacedClient(t *testing.T) {
	_, root := newNamespaceMock(t)
	ns, err := root.Namespace("outer")
	require.NoError(t, err)

	et := &exitT{}
	runIsolate(t, et, ns)
	require.True(t, et.fatalCalled.Load())
	assert.Contains(t, et.fatalText, "auth0mocktest: create namespace:")
	assert.Empty(t, et.cleanups)
}

func TestIsolate_DeleteFailureReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"statusCode":500,"error":"Internal Server Error","message":"boom"}`))
			return
		}
		_, _ = w.Write([]byte(`{"expectations":[]}`))
	}))
	t.Cleanup(srv.Close)
	root, err := auth0mock.NewClient(srv.URL)
	require.NoError(t, err)

	et := &exitT{}
	runIsolate(t, et, root)
	require.False(t, et.fatalCalled.Load(), et.fatalText)
	require.Len(t, et.cleanups, 2)
	for i := len(et.cleanups) - 1; i >= 0; i-- {
		et.cleanups[i]()
	}
	assert.Contains(t, et.errorText, "auth0mocktest: delete namespace")
}
