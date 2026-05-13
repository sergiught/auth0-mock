package spec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
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
func NewValidator(s *Spec) *Validator {
	r, err := legacyrouter.NewRouter(s.Doc,
		openapi3.DisableSchemaPatternValidation(),
		openapi3.DisableSchemaDefaultsValidation(),
	)
	if err != nil {
		// Fail loudly at startup. Boot will catch this.
		panic(fmt.Errorf("build openapi router: %w", err))
	}
	return &Validator{spec: s, router: r}
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
