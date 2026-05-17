package auth0mock

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// ExpectationBuilder is the request-side phase of the fluent stub
// registration API. Construct one via the per-method shortcuts
// (Client.ExpectGet, ExpectPost) or the generic Client.Expect, then
// chain WithQuery / WithBodyJSON / WithBody to narrow the matcher and
// Respond to enter the response-side phase.
//
// All methods mutate and return the receiver — the same pattern
// strings.Builder uses. Don't share a Builder across goroutines
// mid-build; do whatever you want with the *Client itself.
//
// Marshal errors from WithBodyJSON / Respond.JSON do NOT panic mid-
// chain; they're captured on the builder and surfaced by Apply(ctx)
// alongside transport errors. This keeps the fluent path readable
// and means a bad value in test setup fails one test cleanly instead
// of crashing the whole test binary.
type ExpectationBuilder struct {
	client *Client
	exp    Expectation
	err    error // First marshal error seen anywhere in the chain.
}

// ExpectGet starts a stub for an incoming GET to path. Convenience
// wrapper around Expect("GET", path).
func (c *Client) ExpectGet(path string) *ExpectationBuilder { return c.Expect(http.MethodGet, path) }

// ExpectPost starts a stub for an incoming POST to path.
func (c *Client) ExpectPost(path string) *ExpectationBuilder { return c.Expect(http.MethodPost, path) }

// ExpectPut starts a stub for an incoming PUT to path.
func (c *Client) ExpectPut(path string) *ExpectationBuilder { return c.Expect(http.MethodPut, path) }

// ExpectPatch starts a stub for an incoming PATCH to path.
func (c *Client) ExpectPatch(path string) *ExpectationBuilder {
	return c.Expect(http.MethodPatch, path)
}

// ExpectDelete starts a stub for an incoming DELETE to path.
func (c *Client) ExpectDelete(path string) *ExpectationBuilder {
	return c.Expect(http.MethodDelete, path)
}

// Expect starts a stub for the given method + path. Method is
// upper-cased server-side, so callers can pass either case.
func (c *Client) Expect(method, path string) *ExpectationBuilder {
	return &ExpectationBuilder{
		client: c,
		exp:    Expectation{Method: method, Path: path},
	}
}

// WithQuery narrows the matcher to requests that include this
// query-string key + value. Repeated calls add more keys; later
// values for the same key overwrite earlier ones. Subset semantics —
// extra incoming query keys don't disqualify a match.
func (b *ExpectationBuilder) WithQuery(key, value string) *ExpectationBuilder {
	b.ensureRequest()
	if b.exp.Request.Query == nil {
		b.exp.Request.Query = map[string]string{}
	}
	b.exp.Request.Query[key] = value
	return b
}

// WithBodyJSON narrows the matcher to requests whose JSON body is a
// superset of v. V is marshalled with json.Marshal — if marshalling
// fails (chan / func / cyclic value) the error is captured on the
// builder and returned by Apply(ctx) instead of panicking mid-chain.
// For pre-encoded bodies use WithBody.
func (b *ExpectationBuilder) WithBodyJSON(v any) *ExpectationBuilder {
	if b.err != nil {
		return b
	}
	raw, err := json.Marshal(v)
	if err != nil {
		b.err = fmt.Errorf("auth0mock: WithBodyJSON: %w", err)
		return b
	}
	b.ensureRequest()
	b.exp.Request.Body = raw
	return b
}

// WithBody narrows the matcher to requests whose JSON body is a
// superset of raw. Use this when you have a pre-encoded json.RawMessage
// or need to defer marshalling.
func (b *ExpectationBuilder) WithBody(raw json.RawMessage) *ExpectationBuilder {
	b.ensureRequest()
	b.exp.Request.Body = raw
	return b
}

// WithHeader narrows the matcher to requests that carry this header
// with the given value. Repeated calls add more headers; later values
// for the same key overwrite earlier ones. Comparison is
// case-insensitive server-side (canonical MIME header form), so
// "X-Tenant" and "x-tenant" address the same header.
//
// Subset semantics — extra incoming headers don't disqualify a match.
func (b *ExpectationBuilder) WithHeader(key, value string) *ExpectationBuilder {
	b.ensureRequest()
	if b.exp.Request.Headers == nil {
		b.exp.Request.Headers = map[string]string{}
	}
	b.exp.Request.Headers[key] = value
	return b
}

// ensureRequest lazily allocates Request so each With* call doesn't
// have to nil-check.
func (b *ExpectationBuilder) ensureRequest() {
	if b.exp.Request == nil {
		b.exp.Request = &RequestMatcher{}
	}
}

// Respond enters the response-side phase. Status is mandatory — the
// server returns 400 invalid_body for a stub with status 0.
func (b *ExpectationBuilder) Respond(status int) *ResponseBuilder {
	return &ResponseBuilder{
		builder:  b,
		response: ResponseDef{Status: status},
	}
}

// ResponseBuilder is the response-side phase. Chain JSON / Body /
// Header to populate the canned response, then Apply to send the
// registration to the mock.
type ResponseBuilder struct {
	builder  *ExpectationBuilder
	response ResponseDef
}

// JSON sets the response body by marshalling v with json.Marshal.
// A marshal error is captured on the parent builder and surfaced by
// Apply(ctx) — JSON never panics on its own. For pre-encoded bytes
// or non-JSON payloads, use Body.
func (r *ResponseBuilder) JSON(v any) *ResponseBuilder {
	if r.builder.err != nil {
		return r
	}
	raw, err := json.Marshal(v)
	if err != nil {
		r.builder.err = fmt.Errorf("auth0mock: Respond.JSON: %w", err)
		return r
	}
	r.response.Body = raw
	return r
}

// Body sets the response body to the pre-encoded raw bytes. Useful
// when you've already marshalled the body elsewhere or you want to
// send something other than JSON (the server doesn't enforce a
// content-type — it'll happily return raw bytes verbatim).
func (r *ResponseBuilder) Body(raw json.RawMessage) *ResponseBuilder {
	r.response.Body = raw
	return r
}

// Header sets a response header. Repeated calls add more headers;
// later values for the same key overwrite earlier ones.
func (r *ResponseBuilder) Header(key, value string) *ResponseBuilder {
	if r.response.Headers == nil {
		r.response.Headers = map[string]string{}
	}
	r.response.Headers[key] = value
	return r
}

// Apply registers the assembled Expectation with the mock and returns
// a handle to the stored entry. Equivalent to Client.Expectations.Add
// composed with the fluent chain — discard the handle (assign to `_`)
// if you don't need per-stub operations later.
//
// Returns:
//   - any marshal error captured earlier in the chain by
//     WithBodyJSON or Respond.JSON (wrapped with the call-site name),
//   - or *APIError for server-side validation failures,
//   - or a transport error from the http.Client.
func (r *ResponseBuilder) Apply(ctx context.Context) (*RegisteredExpectation, error) {
	if r.builder.err != nil {
		return nil, r.builder.err
	}
	r.builder.exp.Response = r.response
	return r.builder.client.Expectations.Add(ctx, r.builder.exp)
}
