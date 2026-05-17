package auth0mocktest_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	cleanups     []func()
}

func (f *fakeT) Helper() {}
func (f *fakeT) Fatalf(format string, args ...any) {
	f.fatalCalled.Store(true)
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
	reg, err := c.Expectations.Add(context.Background(), auth0mock.Expectation{
		Method: "GET", Path: "/p",
		Response: auth0mock.ResponseDef{Status: 200},
	})
	require.NoError(t, err)
	reg.Times(1)

	// Fire cleanups in real LIFO order: last-registered first.
	require.Len(t, ft.cleanups, 2)
	for i := len(ft.cleanups) - 1; i >= 0; i-- {
		ft.cleanups[i]()
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
	for i := len(ft.cleanups) - 1; i >= 0; i-- {
		ft.cleanups[i]()
	}
	assert.False(t, ft.fatalCalled.Load())
}
