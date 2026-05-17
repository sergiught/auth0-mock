package auth0mock_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/pkg/auth0mock"
)

// captureRecorder is the test stub every expectations test composes
// against. Records every incoming method, path, content-type, and body
// so individual tests can assert on the exact wire shape the SDK
// produced. Concurrency-safe so tests can run with t.Parallel.
type captureRecorder struct {
	mu      sync.Mutex
	calls   []recordedCall
	respond func(w http.ResponseWriter, r *http.Request)
}

type recordedCall struct {
	Method string
	// Path is r.URL.Path — the URL-decoded form, what the server's
	// chi handler would see post-routing.
	Path string
	// RawPath is r.URL.RawPath when the URL has an encoded path,
	// falling back to r.URL.Path. Use this to assert on what went on
	// the wire (e.g. %3F escaping for "?" in audience segments).
	RawPath     string
	ContentType string
	Body        []byte
}

func (c *captureRecorder) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		// EscapedPath() reconstructs the on-wire (URL-encoded) path
		// — RawPath alone is empty when the parser thinks decoded
		// and encoded forms match, which makes it unreliable for
		// asserting "what went on the wire". EscapedPath always
		// returns the encoded form.
		c.mu.Lock()
		c.calls = append(c.calls, recordedCall{
			Method:      r.Method,
			Path:        r.URL.Path,
			RawPath:     r.URL.EscapedPath(),
			ContentType: r.Header.Get("Content-Type"),
			Body:        body,
		})
		c.mu.Unlock()
		if c.respond != nil {
			c.respond(w, r)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (c *captureRecorder) last(t *testing.T) recordedCall {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	require.NotEmpty(t, c.calls, "no calls recorded")
	return c.calls[len(c.calls)-1]
}

func newStub(t *testing.T) (*captureRecorder, *auth0mock.Client) {
	t.Helper()
	rec := &captureRecorder{}
	srv := httptest.NewServer(rec.handler())
	t.Cleanup(srv.Close)

	// Per-test Transport so the idle-connection pool can be drained
	// explicitly before the server shuts down. The default
	// http.Transport is process-shared, and Go 1.26 tightened the race
	// between srv.Close() and the client reusing a pooled keep-alive
	// connection — without this, the suite occasionally fails with
	// "transport connection broken: http: CloseIdleConnections called"
	// when an in-flight request lands on a connection the server-side
	// Close just yanked.
	//
	// T.Cleanup runs in LIFO order. Registering CloseIdleConnections
	// AFTER srv.Close means it fires FIRST at test end, so the
	// client's pool is empty by the time srv.Close runs.
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

func TestExpectations_Add_WireShape(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)

	exp := auth0mock.Expectation{
		Method: "GET",
		Path:   "/api/v2/users/auth0|123",
		Response: auth0mock.ResponseDef{
			Status: 200,
			Body:   auth0mock.MustJSON(map[string]any{"user_id": "auth0|123", "email": "alice@example.com"}),
		},
	}
	_, err := c.Expectations.Add(context.Background(), exp)
	require.NoError(t, err)

	call := rec.last(t)
	assert.Equal(t, http.MethodPost, call.Method)
	assert.Equal(t, "/admin0/expectations", call.Path)
	assert.Equal(t, "application/json", call.ContentType)

	// Decode the on-wire body and assert it round-trips back to the
	// same Expectation shape — locks the JSON contract without making
	// the test brittle to field ordering.
	var got auth0mock.Expectation
	require.NoError(t, json.Unmarshal(call.Body, &got))
	assert.Equal(t, exp, got)
}

func TestExpectations_Add_WithRequestMatcher(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)

	exp := auth0mock.Expectation{
		Method: "POST",
		Path:   "/api/v2/users",
		Request: &auth0mock.RequestMatcher{
			Body: auth0mock.MustJSON(map[string]any{"name": "alice"}),
		},
		Response: auth0mock.ResponseDef{
			Status: 201,
			Body:   auth0mock.MustJSON(map[string]any{"id": "u_42"}),
		},
	}
	_, err := c.Expectations.Add(context.Background(), exp)
	require.NoError(t, err)

	call := rec.last(t)
	// The request matcher must serialize as a nested object, not
	// hoisted to top-level. The server distinguishes "no matcher"
	// (catch-all) from "matcher present" by the field's existence.
	assert.Contains(t, string(call.Body), `"request":{`)
}

func TestExpectations_Add_OmitsNilRequest(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)

	exp := auth0mock.Expectation{
		Method:   "GET",
		Path:     "/api/v2/users",
		Response: auth0mock.ResponseDef{Status: 200, Body: auth0mock.MustJSON([]any{})},
	}
	_, err := c.Expectations.Add(context.Background(), exp)
	require.NoError(t, err)

	// A nil RequestMatcher must not appear on the wire — the
	// `omitempty` tag has to stick or every catch-all stub looks
	// request-matched from the server's perspective.
	assert.NotContains(t, string(rec.last(t).Body), `"request"`)
}

func TestExpectations_Add_PropagatesAPIError(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"statusCode":400,"error":"Bad Request","message":"response.status is required","errorCode":"invalid_body"}`))
	}

	_, err := c.Expectations.Add(context.Background(), auth0mock.Expectation{
		Method: "GET", Path: "/api/v2/users", Response: auth0mock.ResponseDef{},
	})
	var apiErr *auth0mock.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "invalid_body", apiErr.ErrorCode)
}

func TestExpectations_List(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"expectations":[
            {"method":"GET","path":"/api/v2/users/123","response":{"status":200,"body":{"id":"123"}}},
            {"method":"POST","path":"/api/v2/users","response":{"status":201}}
        ]}`))
	}

	got, err := c.Expectations.List(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "GET", got[0].Method)
	assert.Equal(t, "/api/v2/users/123", got[0].Path)
	assert.Equal(t, 200, got[0].Response.Status)
	assert.JSONEq(t, `{"id":"123"}`, string(got[0].Response.Body))
	assert.Equal(t, "POST", got[1].Method)
	assert.Equal(t, 201, got[1].Response.Status)

	call := rec.last(t)
	assert.Equal(t, http.MethodGet, call.Method)
	assert.Equal(t, "/admin0/expectations", call.Path)
	assert.Empty(t, call.ContentType, "GET requests must not advertise a request Content-Type")
}

func TestExpectations_Clear(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	require.NoError(t, c.Expectations.Clear(context.Background()))

	call := rec.last(t)
	assert.Equal(t, http.MethodDelete, call.Method)
	assert.Equal(t, "/admin0/expectations", call.Path)
	assert.Empty(t, call.Body, "Clear must send no body — empty body means 'clear all' to the server")
}

// TestExpectations_Add_ReturnsHandle locks the new contract: Add
// returns a *RegisteredExpectation whose ID is whatever the server
// echoed in the response body. The handle is what callers use for
// per-stub operations (Clear today; Hits / MustHit in batch 6d).
func TestExpectations_Add_ReturnsHandle(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"server-assigned-uuid"}`))
	}

	reg, err := c.Expectations.Add(context.Background(), auth0mock.Expectation{
		Method: "GET", Path: "/p",
		Response: auth0mock.ResponseDef{Status: 200},
	})
	require.NoError(t, err)
	require.NotNil(t, reg)
	assert.Equal(t, "server-assigned-uuid", reg.ID)
}

// TestRegisteredExpectation_Clear locks the wire shape for the per-ID
// DELETE: /admin0/expectations/<id>, no body. Idempotent on the
// server, but the SDK doesn't enforce that — a real 404 would still
// surface as *APIError.
func TestRegisteredExpectation_Clear(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)

	reg := &struct {
		ID string
	}{ID: "abc-123"}
	// Hand-construct a RegisteredExpectation pointing at our stub so we
	// can test Clear in isolation (without first running Add).
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"` + reg.ID + `"}`))
	}
	stored, err := c.Expectations.Add(context.Background(), auth0mock.Expectation{
		Method: "GET", Path: "/p",
		Response: auth0mock.ResponseDef{Status: 200},
	})
	require.NoError(t, err)
	require.Equal(t, reg.ID, stored.ID)

	// Now Clear — assert the second recorded call hits the per-ID
	// DELETE endpoint with no body.
	rec.respond = nil // Back to 204 default.
	require.NoError(t, stored.Clear(context.Background()))
	call := rec.last(t)
	assert.Equal(t, http.MethodDelete, call.Method)
	assert.Equal(t, "/admin0/expectations/abc-123", call.Path)
	assert.Empty(t, call.Body, "per-ID Clear sends no body")
}

func TestExpectations_ClearOp(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	require.NoError(t, c.Expectations.ClearOp(context.Background(), "GET", "/api/v2/users/123"))

	call := rec.last(t)
	assert.Equal(t, http.MethodDelete, call.Method)
	assert.JSONEq(t, `{"method":"GET","path":"/api/v2/users/123"}`, string(call.Body))
}

// TestVerify_DetectsResetBeforeVerify is the safety-net for the
// canonical cleanup-ordering bug: if Reset wipes a non-empty ledger
// before Verify runs, the constraints can never be checked. Without
// the guard, Verify would see len(tracked) == 0 and silently return
// nil. The guard turns that into an actionable error pointing at
// auth0mocktest.Bracket.
func TestVerify_DetectsResetBeforeVerify(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = listOrAddRespond(t, "abc", nil)

	_, err := c.Expectations.Add(context.Background(), auth0mock.Expectation{
		Method: "GET", Path: "/p", Response: auth0mock.ResponseDef{Status: 200},
	})
	require.NoError(t, err)

	// Reset wipes the ledger while pendingAdds > 0 → safety-net flag fires.
	require.NoError(t, c.Reset(context.Background()))

	// Verify must NOT silently pass — it should return the cleanup-ordering error.
	err = c.Expectations.Verify(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Reset() ran before Verify()")
	assert.Contains(t, err.Error(), "auth0mocktest.Bracket")

	// The flag is single-shot — a subsequent Verify (with no fresh
	// unverified adds) is clean.
	require.NoError(t, c.Expectations.Verify(context.Background()))
}

// TestVerify_AddResetAddVerify_LegitimatePatternSucceeds locks the
// fix for the "safety net fires on a legitimate pattern" bug found
// in the fourth review pass. A user may legitimately do:
//
//	Add(default stubs) → Reset(start clean) → Add(test-specific stubs) → Verify
//
// The first Add's constraints are intentionally discarded by Reset
// (that's the user's intent), and the second Add's constraints should
// be checked normally. The safety-net flag from the first Reset must
// not poison the post-Reset verification.
func TestVerify_AddResetAddVerify_LegitimatePatternSucceeds(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = listOrAddRespond(t, "abc", []auth0mock.Expectation{{
		ID: "abc", Method: "GET", Path: "/p",
		Response: auth0mock.ResponseDef{Status: 200},
		Hits:     1,
	}})

	// First Add (default setup), then Reset (clean slate before the test
	// body), then Add again (test-specific stub with constraint).
	_, err := c.Expectations.Add(context.Background(), auth0mock.Expectation{
		Method: "GET", Path: "/p", Response: auth0mock.ResponseDef{Status: 200},
	})
	require.NoError(t, err)
	require.NoError(t, c.Reset(context.Background()))

	reg, err := c.Expectations.Add(context.Background(), auth0mock.Expectation{
		Method: "GET", Path: "/p", Response: auth0mock.ResponseDef{Status: 200},
	})
	require.NoError(t, err)
	reg.Times(1)

	// Verify must check the SECOND Add's constraint, NOT fire the
	// safety net for the FIRST Reset.
	assert.NoError(t, c.Expectations.Verify(context.Background()))
}

// TestVerify_AddClearReset_NoSafetyNetMisfire locks the fix for the
// stale-counter bug: Add (pendingAdds=1) → reg.Clear (untracks the
// only handle) → Reset → Verify should not trip the safety net,
// because Clear intentionally retracted the constraint before Reset
// ran. Pre-fix: pendingAdds stayed at 1 after Clear, so Reset's
// Swap(0) returned non-zero and latched the flag.
func TestVerify_AddClearReset_NoSafetyNetMisfire(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"abc"}`))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}

	reg, err := c.Expectations.Add(context.Background(), auth0mock.Expectation{
		Method: "GET", Path: "/p", Response: auth0mock.ResponseDef{Status: 200},
	})
	require.NoError(t, err)
	reg.Times(1) // Would be unmet, but we Clear before Reset.

	require.NoError(t, reg.Clear(context.Background()))
	require.NoError(t, c.Reset(context.Background()))

	// Safety net must not fire — the constraint was intentionally retracted.
	assert.NoError(t, c.Expectations.Verify(context.Background()))
}

// TestVerify_FlagClearedOnSuccess locks the contract that a happy
// Verify run clears the pending-adds counter so the next Reset on
// the same Client doesn't re-trip the guard.
func TestVerify_FlagClearedOnSuccess(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = listOrAddRespond(t, "abc", []auth0mock.Expectation{{
		ID: "abc", Method: "GET", Path: "/p",
		Response: auth0mock.ResponseDef{Status: 200},
		Hits:     1,
	}})

	reg, err := c.Expectations.Add(context.Background(), auth0mock.Expectation{
		Method: "GET", Path: "/p", Response: auth0mock.ResponseDef{Status: 200},
	})
	require.NoError(t, err)
	reg.Times(1)

	// Successful Verify consumes the pending-adds counter.
	require.NoError(t, c.Expectations.Verify(context.Background()))

	// Subsequent Reset must not flag the safety net (counter was zeroed by Verify).
	require.NoError(t, c.Reset(context.Background()))
	require.NoError(t, c.Expectations.Verify(context.Background()))
}

// TestRegisteredExpectation_Hits_ClearedStubReturns404 locks the
// post-Clear behaviour: once Clear runs server-side, Hits returns
// *APIError with statusCode 404 + errorCode "unknown_id". A future
// refactor that quietly swallows the error or returns 0 hits would
// be a regression — tests would silently pass against missing stubs.
func TestRegisteredExpectation_Hits_ClearedStubReturns404(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost: // Add.
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"abc"}`))
		case http.MethodGet: // Hits.
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"statusCode":404,"error":"Not Found","message":"no expectation with id abc","errorCode":"unknown_id"}`))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}

	reg, err := c.Expectations.Add(context.Background(), auth0mock.Expectation{
		Method: "GET", Path: "/p", Response: auth0mock.ResponseDef{Status: 200},
	})
	require.NoError(t, err)

	hits, err := reg.Hits(context.Background())
	require.Error(t, err)
	assert.Equal(t, int64(0), hits, "cleared stub returns zero hits alongside the error")

	var apiErr *auth0mock.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	assert.Equal(t, "unknown_id", apiErr.ErrorCode)
}

// TestVerify_DoubleResetWithInterleavedAdd locks the edge case the
// fourth-pass reviewer raised: Add → Reset → Add → Reset → Verify.
// The first Reset latches the safety net (Add #1 was lost); the
// second Reset re-latches it (Add #2 was also lost). The single
// Verify call should fire the safety net once — the flag is a
// one-shot signal, not an additive counter.
func TestVerify_DoubleResetWithInterleavedAdd(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = listOrAddRespond(t, "abc", nil)

	for cycle := 0; cycle < 2; cycle++ {
		_, err := c.Expectations.Add(context.Background(), auth0mock.Expectation{
			Method: "GET", Path: "/p", Response: auth0mock.ResponseDef{Status: 200},
		})
		require.NoError(t, err)
		require.NoError(t, c.Reset(context.Background()))
	}

	// Both unverified adds were silently dropped by the two Resets —
	// safety net fires.
	err := c.Expectations.Verify(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Reset() ran before Verify()")
}

// TestRegisteredExpectation_Hits locks the per-stub Hits read: the
// SDK hits GET /admin0/expectations/{id} and decodes the response's
// `hits` field. Useful for one-off "did this stub fire?" assertions
// without running full Verify.
func TestRegisteredExpectation_Hits(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)

	// Sequence the stub: first respond to Add with the ID, then to
	// the Hits GET with the expectation object including hits=3.
	calls := 0
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"abc"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"abc","method":"GET","path":"/p","response":{"status":200},"hits":3}`))
	}

	reg, err := c.Expectations.Add(context.Background(), auth0mock.Expectation{
		Method: "GET", Path: "/p",
		Response: auth0mock.ResponseDef{Status: 200},
	})
	require.NoError(t, err)

	hits, err := reg.Hits(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(3), hits)
	// And the wire shape — GET /admin0/expectations/abc.
	assert.Equal(t, http.MethodGet, rec.last(t).Method)
	assert.Equal(t, "/admin0/expectations/abc", rec.last(t).Path)
}

// TestVerify_HappyAndViolations is the headline test for the
// Times / AtLeast / AtMost contract. Exercises every constraint mode
// across one Verify call, and locks the violation-message shape.
func TestVerify_HappyAndViolations(t *testing.T) {
	t.Parallel()

	t.Run("no constraints set → no-op", func(t *testing.T) {
		t.Parallel()
		_, c := newStub(t)
		// Add a stub without setting any constraint.
		_, err := c.Expectations.Add(context.Background(), auth0mock.Expectation{
			Method: "GET", Path: "/p",
			Response: auth0mock.ResponseDef{Status: 200},
		})
		require.NoError(t, err)
		// Verify must succeed even though the stub was never matched —
		// without a constraint, the SDK doesn't care.
		assert.NoError(t, c.Expectations.Verify(context.Background()))
	})

	t.Run("Times — happy + violation", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			name      string
			actual    int64
			want      int64
			expectErr bool
		}{
			{"exact match", 3, 3, false},
			{"too few", 2, 3, true},
			{"too many", 4, 3, true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				rec, c := newStub(t)
				rec.respond = listOrAddRespond(t, "abc", []auth0mock.Expectation{{
					ID: "abc", Method: "GET", Path: "/p",
					Response: auth0mock.ResponseDef{Status: 200},
					Hits:     tc.actual,
				}})
				reg, err := c.Expectations.Add(context.Background(), auth0mock.Expectation{
					Method: "GET", Path: "/p",
					Response: auth0mock.ResponseDef{Status: 200},
				})
				require.NoError(t, err)
				reg.Times(tc.want)

				err = c.Expectations.Verify(context.Background())
				if tc.expectErr {
					require.Error(t, err)
					assert.Contains(t, err.Error(), fmt.Sprintf("expected exactly %d, got %d", tc.want, tc.actual))
				} else {
					assert.NoError(t, err)
				}
			})
		}
	})

	t.Run("AtLeast — happy + violation", func(t *testing.T) {
		t.Parallel()
		rec, c := newStub(t)
		rec.respond = listOrAddRespond(t, "x", []auth0mock.Expectation{{
			ID: "x", Method: "GET", Path: "/p",
			Response: auth0mock.ResponseDef{Status: 200},
			Hits:     1,
		}})
		reg, err := c.Expectations.Add(context.Background(), auth0mock.Expectation{
			Method: "GET", Path: "/p",
			Response: auth0mock.ResponseDef{Status: 200},
		})
		require.NoError(t, err)
		reg.AtLeast(2)
		err = c.Expectations.Verify(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected at least 2, got 1")
	})

	t.Run("AtMost — happy + violation", func(t *testing.T) {
		t.Parallel()
		rec, c := newStub(t)
		rec.respond = listOrAddRespond(t, "y", []auth0mock.Expectation{{
			ID: "y", Method: "GET", Path: "/p",
			Response: auth0mock.ResponseDef{Status: 200},
			Hits:     5,
		}})
		reg, err := c.Expectations.Add(context.Background(), auth0mock.Expectation{
			Method: "GET", Path: "/p",
			Response: auth0mock.ResponseDef{Status: 200},
		})
		require.NoError(t, err)
		reg.AtMost(2)
		err = c.Expectations.Verify(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected at most 2, got 5")
	})

	t.Run("AnyTimes clears prior constraint", func(t *testing.T) {
		t.Parallel()
		rec, c := newStub(t)
		rec.respond = listOrAddRespond(t, "z", []auth0mock.Expectation{{
			ID: "z", Method: "GET", Path: "/p",
			Response: auth0mock.ResponseDef{Status: 200},
			Hits:     0,
		}})
		reg, err := c.Expectations.Add(context.Background(), auth0mock.Expectation{
			Method: "GET", Path: "/p",
			Response: auth0mock.ResponseDef{Status: 200},
		})
		require.NoError(t, err)
		reg.Times(5).AnyTimes()
		assert.NoError(t, c.Expectations.Verify(context.Background()))
	})

	t.Run("cleared stub with constraint → violation", func(t *testing.T) {
		t.Parallel()
		rec, c := newStub(t)
		// List returns NO entries — the stub has been cleared
		// server-side, but the local handle still expects exact:1.
		rec.respond = listOrAddRespond(t, "ghost", nil)
		reg, err := c.Expectations.Add(context.Background(), auth0mock.Expectation{
			Method: "GET", Path: "/p",
			Response: auth0mock.ResponseDef{Status: 200},
		})
		require.NoError(t, err)
		reg.Times(1)
		err = c.Expectations.Verify(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cleared")
		assert.Contains(t, err.Error(), "expected exactly 1")
	})
}

// listOrAddRespond returns an httptest.HandlerFunc that distinguishes
// the POST /admin0/expectations (Add) call from the subsequent
// GET /admin0/expectations (Verify's List). Reduces the noise in
// table-driven Verify tests above.
func listOrAddRespond(_ *testing.T, addID string, listed []auth0mock.Expectation) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"` + addID + `"}`))
		case http.MethodGet:
			resp := struct {
				Expectations []auth0mock.Expectation `json:"expectations"`
			}{Expectations: listed}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

func TestMustJSON_Panics(t *testing.T) {
	t.Parallel()
	// Channels are not JSON-encodable — assert MustJSON panics with a
	// useful message rather than swallowing the error or producing a
	// silent zero-value RawMessage.
	assert.PanicsWithValue(t, "auth0mock.MustJSON: json: unsupported type: chan int", func() {
		_ = auth0mock.MustJSON(make(chan int))
	})
}

func TestMustJSON_Happy(t *testing.T) {
	t.Parallel()
	got := auth0mock.MustJSON(map[string]any{"a": 1, "b": "two"})
	assert.JSONEq(t, `{"a":1,"b":"two"}`, string(got))
}
