package auth0mock_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/pkg/auth0mock"
)

func TestFluent_MinimalGet(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	_, err := c.ExpectGet("/api/v2/users/auth0|123").
		Respond(200).
		JSON(map[string]any{"user_id": "auth0|123", "email": "alice@example.com"}).
		Apply(context.Background())
	require.NoError(t, err)

	call := rec.last(t)
	assert.Equal(t, http.MethodPost, call.Method, "the fluent builder must hit the Add endpoint")
	assert.Equal(t, "/admin0/expectations", call.Path)

	var sent auth0mock.Expectation
	require.NoError(t, json.Unmarshal(call.Body, &sent))
	assert.Equal(t, "GET", sent.Method)
	assert.Equal(t, "/api/v2/users/auth0|123", sent.Path)
	assert.Equal(t, 200, sent.Response.Status)
	assert.JSONEq(t, `{"user_id":"auth0|123","email":"alice@example.com"}`, string(sent.Response.Body))
	// No request matcher set → must be omitted, not sent as `null` or
	// an empty object. The server distinguishes catch-all from
	// matcher-present by field presence.
	assert.Nil(t, sent.Request)
}

func TestFluent_WithQueryAndBodyAndHeaders(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	_, err := c.ExpectPost("/api/v2/users").
		WithQuery("connection", "Username-Password-Authentication").
		WithBodyJSON(map[string]any{"email": "alice@example.com"}).
		Respond(201).
		Header("X-Auth0-Mock", "stub").
		JSON(map[string]any{"id": "u_42"}).
		Apply(context.Background())
	require.NoError(t, err)

	var sent auth0mock.Expectation
	require.NoError(t, json.Unmarshal(rec.last(t).Body, &sent))
	require.NotNil(t, sent.Request, "WithQuery/WithBodyJSON must materialise Request")
	assert.Equal(t, "Username-Password-Authentication", sent.Request.Query["connection"])
	assert.JSONEq(t, `{"email":"alice@example.com"}`, string(sent.Request.Body))
	assert.Equal(t, "stub", sent.Response.Headers["X-Auth0-Mock"])
	assert.JSONEq(t, `{"id":"u_42"}`, string(sent.Response.Body))
}

func TestFluent_AllVerbs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		start      func(*auth0mock.Client) *auth0mock.ExpectationBuilder
		wantMethod string
	}{
		{"GET", func(c *auth0mock.Client) *auth0mock.ExpectationBuilder { return c.ExpectGet("/p") }, "GET"},
		{"POST", func(c *auth0mock.Client) *auth0mock.ExpectationBuilder { return c.ExpectPost("/p") }, "POST"},
		{"PUT", func(c *auth0mock.Client) *auth0mock.ExpectationBuilder { return c.ExpectPut("/p") }, "PUT"},
		{"PATCH", func(c *auth0mock.Client) *auth0mock.ExpectationBuilder { return c.ExpectPatch("/p") }, "PATCH"},
		{"DELETE", func(c *auth0mock.Client) *auth0mock.ExpectationBuilder { return c.ExpectDelete("/p") }, "DELETE"},
		{"Expect-arbitrary", func(c *auth0mock.Client) *auth0mock.ExpectationBuilder {
			return c.Expect("OPTIONS", "/p")
		}, "OPTIONS"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec, c := newStub(t)
			_, err := tc.start(c).Respond(204).Apply(context.Background())
			require.NoError(t, err)
			var sent auth0mock.Expectation
			require.NoError(t, json.Unmarshal(rec.last(t).Body, &sent))
			assert.Equal(t, tc.wantMethod, sent.Method)
			assert.Equal(t, "/p", sent.Path)
			assert.Equal(t, 204, sent.Response.Status)
		})
	}
}

func TestFluent_QueryReplaceSemantics(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	_, err := c.ExpectGet("/p").
		WithQuery("k", "first").
		WithQuery("k", "second"). // Later wins.
		WithQuery("other", "v").
		Respond(200).
		Apply(context.Background())
	require.NoError(t, err)

	var sent auth0mock.Expectation
	require.NoError(t, json.Unmarshal(rec.last(t).Body, &sent))
	require.NotNil(t, sent.Request)
	assert.Equal(t, "second", sent.Request.Query["k"], "repeated WithQuery on same key must overwrite, not append")
	assert.Equal(t, "v", sent.Request.Query["other"])
}

func TestFluent_HeaderReplaceSemantics(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	_, err := c.ExpectGet("/p").
		Respond(200).
		Header("X-Trace", "first").
		Header("X-Trace", "second").
		Apply(context.Background())
	require.NoError(t, err)

	var sent auth0mock.Expectation
	require.NoError(t, json.Unmarshal(rec.last(t).Body, &sent))
	assert.Equal(t, "second", sent.Response.Headers["X-Trace"])
}

// TestFluent_MarshalErrorDeferredToApply locks the no-panic contract:
// a non-encodable value passed to WithBodyJSON or Respond.JSON is
// captured on the builder and surfaced by Apply, NOT panicked
// mid-chain. Pre-batch-3 behaviour was MustJSON-panic, which crashed
// the whole test binary instead of failing one t.Run subtest cleanly.
func TestFluent_MarshalErrorDeferredToApply(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		// Build mutates the chain so the marshal error originates
		// from the call we're stress-testing.
		build        func(*auth0mock.Client) *auth0mock.ResponseBuilder
		wantContains string
	}{
		{
			name: "WithBodyJSON",
			build: func(c *auth0mock.Client) *auth0mock.ResponseBuilder {
				return c.ExpectPost("/p").
					WithBodyJSON(make(chan int)). // Unencodable.
					Respond(200)
			},
			wantContains: "WithBodyJSON",
		},
		{
			name: "Respond.JSON",
			build: func(c *auth0mock.Client) *auth0mock.ResponseBuilder {
				return c.ExpectGet("/p").
					Respond(200).
					JSON(make(chan int)) // Unencodable.
			},
			wantContains: "Respond.JSON",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec, c := newStub(t)
			require.NotPanics(t, func() {
				_, err := tc.build(c).Apply(context.Background())
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantContains)
				assert.Contains(t, err.Error(), "unsupported type")
			})
			// Crucially, the HTTP layer must not have fired — a marshal
			// error means there's nothing valid to send.
			assert.Equal(t, 0, len(rec.calls), "Apply must short-circuit before the HTTP call when a marshal error is queued")
		})
	}
}

// TestFluent_FirstErrorWinsInChain locks the "first error wins"
// contract — once an error is queued on the builder, subsequent
// chain methods must be no-ops so a single Apply error tells the
// whole story.
func TestFluent_FirstErrorWinsInChain(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	_, err := c.ExpectPost("/p").
		WithBodyJSON(make(chan int)).             // Error #1 (this wins).
		WithBodyJSON(map[string]any{"ok": true}). // Would otherwise overwrite.
		Respond(200).
		JSON(make(chan float64)). // Error #2 (suppressed).
		Apply(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "WithBodyJSON")
	assert.NotContains(t, err.Error(), "Respond.JSON")
	assert.Equal(t, 0, len(rec.calls))
}

// TestFluent_WithHeader_WireShape locks the header-matcher contract:
// WithHeader populates the request-side Headers map (separate from
// the response-side Headers populated by ResponseBuilder.Header), and
// the JSON tag on the wire is "headers" — the server matches
// incoming headers against this set with subset semantics.
func TestFluent_WithHeader_WireShape(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	_, err := c.ExpectPost("/api/v2/users").
		WithHeader("Authorization", "Bearer test-token").
		WithHeader("X-Tenant", "acme").
		Respond(201).
		JSON(map[string]any{"id": "u_42"}).
		Apply(context.Background())
	require.NoError(t, err)

	var sent auth0mock.Expectation
	require.NoError(t, json.Unmarshal(rec.last(t).Body, &sent))
	require.NotNil(t, sent.Request, "WithHeader must materialise Request")
	assert.Equal(t, "Bearer test-token", sent.Request.Headers["Authorization"])
	assert.Equal(t, "acme", sent.Request.Headers["X-Tenant"])
	// Headers and Response.Headers are distinct fields on the wire.
	assert.Empty(t, sent.Response.Headers, "WithHeader populates Request.Headers, not Response.Headers")
}

// TestFluent_WithHeader_ReplaceSemantics — repeated WithHeader on the
// same key overwrites, matching WithQuery's behaviour.
func TestFluent_WithHeader_ReplaceSemantics(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	_, err := c.ExpectGet("/p").
		WithHeader("X-Trace", "first").
		WithHeader("X-Trace", "second").
		Respond(200).
		Apply(context.Background())
	require.NoError(t, err)
	var sent auth0mock.Expectation
	require.NoError(t, json.Unmarshal(rec.last(t).Body, &sent))
	assert.Equal(t, "second", sent.Request.Headers["X-Trace"])
}

func TestFluent_BodyAcceptsPreEncoded(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	raw := json.RawMessage(`{"already":"encoded"}`)
	_, err := c.ExpectGet("/p").
		Respond(200).
		Body(raw).
		Apply(context.Background())
	require.NoError(t, err)

	var sent auth0mock.Expectation
	require.NoError(t, json.Unmarshal(rec.last(t).Body, &sent))
	assert.JSONEq(t, `{"already":"encoded"}`, string(sent.Response.Body))
}
