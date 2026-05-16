package spec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	legacyrouter "github.com/getkin/kin-openapi/routers/legacy"
)

// Validator wraps openapi3filter for request and registration-payload
// validation.
type Validator struct {
	spec   *Spec
	router routers.Router
}

// NewValidator constructs a Validator. The underlying router is built once.
// DisableSchemaPatternValidation is required because Auth0's spec contains
// Perl-syntax lookahead patterns (e.g. `(?=`) that Go's regexp engine does
// not support. Route matching is unaffected; only pattern constraints are
// skipped during router construction.
//
// Returns an error if the embedded spec cannot be turned into a router; the
// caller (cmd/api boot) should surface that to the user instead of crashing.
func NewValidator(s *Spec) (*Validator, error) {
	r, err := legacyrouter.NewRouter(s.Doc,
		openapi3.DisableSchemaPatternValidation(),
		openapi3.DisableSchemaDefaultsValidation(),
	)
	if err != nil {
		return nil, fmt.Errorf("build openapi router: %w", err)
	}
	return &Validator{spec: s, router: r}, nil
}

// ValidateRequest checks an incoming request against the operation it routed
// to. It returns nil on success or an error suitable for a 400 response body.
func (v *Validator) ValidateRequest(r *http.Request, _ Operation) error {
	route, pathParams, err := v.router.FindRoute(r)
	if err != nil {
		return fmt.Errorf("route: %w", err)
	}
	input := &openapi3filter.RequestValidationInput{
		Request:    r,
		PathParams: pathParams,
		Route:      route,
		Options: &openapi3filter.Options{
			AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
		},
	}
	return openapi3filter.ValidateRequest(r.Context(), input)
}

// Resolve finds the spec Operation that a (method, path) pair routes to. The
// path may be concrete ("/api/v2/users/auth0|1") or a template
// ("/api/v2/users/{id}") — the router treats a literal "{id}" segment as just
// another path-parameter value, so both forms resolve to the same operation.
// Returns an error when no operation matches. Used by the /admin0/expectations
// handler, which receives method+path in a request body rather than from a
// routed URL.
func (v *Validator) Resolve(method, path string) (Operation, error) {
	req := &http.Request{Method: method, URL: &url.URL{Path: path}, Header: make(http.Header)}
	route, _, err := v.router.FindRoute(req)
	if err != nil {
		return Operation{}, fmt.Errorf("no operation for %s %s: %w", method, path, err)
	}
	return Operation{
		Method:   route.Method,
		Template: v.spec.BasePath + route.Path,
		Op:       route.Operation,
	}, nil
}

// ValidateRegistration checks a registered response payload against the
// operation's response schema for the chosen status. Status must be present in
// op.Responses (or "default"); body is unmarshaled JSON.
func (v *Validator) ValidateRegistration(op Operation, status int, body json.RawMessage) error {
	resp := selectResponse(op.Op.Responses, status)
	if resp == nil || resp.Value == nil {
		return fmt.Errorf("status %d not declared for operation %s", status, op.Op.OperationID)
	}
	// Status with no content is fine only if body is empty.
	if len(resp.Value.Content) == 0 {
		if len(bytes.TrimSpace(body)) == 0 {
			return nil
		}
		return fmt.Errorf("status %d declares no body but a body was provided", status)
	}
	media := resp.Value.Content.Get("application/json")
	if media == nil || media.Schema == nil {
		// Without an application/json schema we can't validate; accept.
		return nil
	}
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return fmt.Errorf("body json: %w", err)
	}
	if err := media.Schema.Value.VisitJSON(decoded, openapi3.MultiErrors()); err != nil {
		return fmt.Errorf("body schema: %w", err)
	}
	return nil
}

// ValidateResponse checks an outgoing response. Used as defense-in-depth at
// serve time. Errors here mean a registered match was somehow stored despite
// failing ValidateRegistration; in strict mode the caller returns 500.
func (v *Validator) ValidateResponse(op Operation, status int, headers map[string]string, body []byte) error {
	resp := selectResponse(op.Op.Responses, status)
	if resp == nil || resp.Value == nil {
		return fmt.Errorf("status %d not declared", status)
	}
	if resp.Value.Content == nil {
		return nil
	}
	media := resp.Value.Content.Get("application/json")
	if media == nil || media.Schema == nil {
		return nil
	}
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return fmt.Errorf("body json: %w", err)
	}
	return media.Schema.Value.VisitJSON(decoded, openapi3.MultiErrors())
}

// selectResponse returns op.Responses[status] or op.Responses["default"].
func selectResponse(rs *openapi3.Responses, status int) *openapi3.ResponseRef {
	if rs == nil {
		return nil
	}
	if r := rs.Value(strconv.Itoa(status)); r != nil {
		return r
	}
	return rs.Default()
}

// ValidateRequestMatcher checks a request-body matcher against the operation's
// request body schema, with `required` constraints relaxed — a matcher is
// intentionally partial, so absent required fields are fine, but unknown
// fields (the request schema is typically additionalProperties:false) and
// mistyped known fields are still rejected. An empty matcher body is a no-op;
// an operation that declares no JSON request body rejects any non-empty body.
func (v *Validator) ValidateRequestMatcher(op Operation, body json.RawMessage) error {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	if op.Op.RequestBody == nil || op.Op.RequestBody.Value == nil {
		return fmt.Errorf("operation %s declares no request body", op.Op.OperationID)
	}
	media := op.Op.RequestBody.Value.Content.Get("application/json")
	if media == nil || media.Schema == nil || media.Schema.Value == nil {
		return fmt.Errorf("operation %s has no application/json request schema", op.Op.OperationID)
	}
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return fmt.Errorf("matcher body json: %w", err)
	}
	err := media.Schema.Value.VisitJSON(decoded, openapi3.MultiErrors())
	if err := filterRequiredErrors(err); err != nil {
		return fmt.Errorf("matcher body schema: %w", err)
	}
	return nil
}

// ValidateQueryMatcher checks that every key in a query matcher names a query
// parameter declared by the operation.
func (v *Validator) ValidateQueryMatcher(op Operation, query map[string]string) error {
	if len(query) == 0 {
		return nil
	}
	declared := make(map[string]bool)
	for _, p := range op.Op.Parameters {
		if p.Value != nil && p.Value.In == openapi3.ParameterInQuery {
			declared[p.Value.Name] = true
		}
	}
	for k := range query {
		if !declared[k] {
			return fmt.Errorf("unknown query parameter %q for operation %s", k, op.Op.OperationID)
		}
	}
	return nil
}

// filterRequiredErrors drops "required"-field violations from a schema
// validation error, returning nil if only required-field errors remain. This
// is what relaxes `required` for partial request matchers.
func filterRequiredErrors(err error) error {
	if err == nil {
		return nil
	}
	var me openapi3.MultiError
	ok := errors.As(err, &me)
	if !ok {
		return err
	}
	var kept openapi3.MultiError
	for _, e := range me {
		var se *openapi3.SchemaError
		if errors.As(e, &se) && se.SchemaField == "required" {
			continue
		}
		kept = append(kept, e)
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}
