package matches

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sergiught/auth0-mock/internal/spec"
	"github.com/sergiught/auth0-mock/internal/tmpl"
)

// Registration is the payload that describes one expectation: the target
// operation (method + path), an optional request matcher, the canned
// response returned when the matcher applies, plus optional later answers.
// It is the body of POST /admin0/expectations and the shape of a seed-file
// entry.
type Registration struct {
	Method   string          `json:"method"`
	Path     string          `json:"path"`
	Request  *RequestMatcher `json:"request,omitempty"`
	Response ResponseDef     `json:"response"`
	Then     []ResponseDef   `json:"then,omitempty"`
}

// RegistrationError is the validation failure Prepare returns. Message and
// Code are the text and machine code reported to the caller.
type RegistrationError struct {
	Message string
	Code    string
}

func (e *RegistrationError) Error() string { return e.Message }

// Prepare validates r against the Management API spec and returns the
// Expectation ready for Store.Put: method upper-cased, an empty matcher
// normalised to nil, Kind set, and Path set to the spec template for
// template paths. The error is always a *RegistrationError.
func Prepare(v *spec.Validator, r Registration) (Expectation, error) {
	r.Method = strings.ToUpper(r.Method)
	if r.Method == "" || r.Path == "" {
		return Expectation{}, &RegistrationError{"method and path are required", "invalid_body"}
	}
	if r.Response.Status == 0 {
		return Expectation{}, &RegistrationError{"response.status is required", "invalid_body"}
	}
	op, err := v.Resolve(r.Method, r.Path)
	if err != nil {
		return Expectation{}, &RegistrationError{"no Management API operation for " + r.Method + " " + r.Path, "unknown_operation"}
	}
	// The event stream's success answer is the live stream itself, fed by
	// POST /admin0/events. A canned 2xx would silently replace it with a
	// static body, so only error stubs may stand in for it.
	if op.Op.OperationID == spec.SubscribeEventsOperationID && r.Response.Status < http.StatusBadRequest {
		return Expectation{}, &RegistrationError{
			"GET /api/v2/events accepts only error stubs (status >= 400); push events with POST /admin0/events instead",
			"invalid_match",
		}
	}
	if msg, code := prepareEntry(v, op, r.Path, "response", &r.Response); msg != "" {
		return Expectation{}, &RegistrationError{msg, code}
	}
	r.Then = slices.Clone(r.Then)
	if msg, code := validateThen(v, op, r.Path, r.Then); msg != "" {
		return Expectation{}, &RegistrationError{msg, code}
	}
	// Normalize an empty request matcher to nil so a catch-all has
	// exactly one representation in the store.
	req, err := prepareMatcher(v, op, r.Request)
	if err != nil {
		return Expectation{}, err
	}
	kind := KindOf(r.Path)
	storePath := r.Path
	if kind == KindTemplate {
		storePath = op.Template
	}
	return Expectation{
		Method:   r.Method,
		Path:     storePath,
		Kind:     kind,
		Request:  req,
		Response: r.Response,
		Then:     r.Then,
	}, nil
}

// prepareMatcher validates a request matcher against the operation and returns
// its stored form: nil when it carries no criteria, with empty fields dropped
// and absent header paths canonicalised. The error is always a
// *RegistrationError.
func prepareMatcher(v *spec.Validator, op spec.Operation, in *RequestMatcher) (*RequestMatcher, error) {
	req := in
	if err := req.validateQuery(); err != nil {
		return nil, &RegistrationError{
			"request matcher query is invalid: " + err.Error(), "invalid_request_match",
		}
	}
	if err := errors.Join(req.validateAbsent(), req.validateAbsentForOp(op)); err != nil {
		return nil, &RegistrationError{
			"request matcher absent is invalid: " + err.Error(), "invalid_request_match",
		}
	}
	if req.IsEmpty() {
		req = nil
	}
	req = dropEmptyMaps(req)
	if req != nil && len(req.Absent) > 0 {
		c := *req
		c.Absent = canonicalAbsentList(req.Absent)
		req = &c
	}
	if req != nil {
		if err := v.ValidateRequestMatcher(op, req.Body); err != nil {
			return nil, &RegistrationError{
				"request matcher body violates request schema: " + err.Error(), "invalid_request_match",
			}
		}
		if err := v.ValidateQueryMatcher(op, append(req.queryKeys(), req.absentQueryKeys()...)); err != nil {
			return nil, &RegistrationError{
				"request matcher query is invalid: " + err.Error(), "invalid_request_match",
			}
		}
	}
	return req, nil
}

// dropEmptyMaps returns m with an empty query, headers or absent field set to nil, so
// {} and an omitted key are one matcher. It copies rather than modifies m.
func dropEmptyMaps(m *RequestMatcher) *RequestMatcher {
	if m == nil || (m.Query == nil || len(m.Query) > 0) && (m.Headers == nil || len(m.Headers) > 0) &&
		(m.Absent == nil || len(m.Absent) > 0) {
		return m
	}
	c := *m
	if len(c.Query) == 0 {
		c.Query = nil
	}
	if len(c.Headers) == 0 {
		c.Headers = nil
	}
	if len(c.Absent) == 0 {
		c.Absent = nil
	}
	return &c
}

// prepareEntry checks one response entry (named at: "response" or "then[i]")
// against the spec. A templated entry is parsed, rendered with sample values
// and checked with its body placeholders skipped; the parsed template is kept
// on e. It returns a message and code, or "" when the entry is valid.
func prepareEntry(v *spec.Validator, op spec.Operation, path, at string, e *ResponseDef) (msg, code string) {
	status := strconv.Itoa(e.Status)
	schemaMsg := "registered " + at + " violates schema for status " + status + ": "
	if !e.Template {
		if err := v.ValidateRegistration(op, e.Status, e.Body); err != nil {
			return schemaMsg + err.Error(), "invalid_match"
		}
		return "", ""
	}
	names := tmpl.PathNames(op.Template)
	t, err := tmpl.Parse(e.Body, e.Headers, names)
	if err != nil {
		return at + " template: " + err.Error(), "invalid_template"
	}
	pathVars := tmpl.PathValues(op.Template, escapedPath(path))
	if pathVars == nil {
		pathVars = map[string]string{}
	}
	// A parameter the registration leaves as {name} is any value when
	// served: it renders as a sample now and is checked then.
	var unresolved []string
	for _, name := range names {
		if val, ok := pathVars[name]; !ok || val == "{"+name+"}" {
			pathVars[name] = "sample"
			unresolved = append(unresolved, name)
		}
	}
	rendered, _, err := t.Render(tmpl.Vars{
		UUID:   "00000000-0000-4000-8000-000000000000",
		Now:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Path:   pathVars,
		Sample: true,
	})
	if err != nil {
		return at + " template: " + err.Error(), "invalid_template"
	}
	if err := v.ValidateRegistrationDeferring(op, e.Status, rendered, t.BodyPointers(), t.TextPointers(unresolved)); err != nil {
		return schemaMsg + err.Error(), "invalid_match"
	}
	e.tmpl = t
	return "", ""
}

// escapedPath re-encodes a decoded registration path segment by segment, so
// that PathValues decodes it exactly once, as it does a request's escaped path.
func escapedPath(path string) string {
	segs := strings.Split(path, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

// validateThen checks each sequence entry like the primary response and
// returns the first problem message naming the entry plus an error code, or
// "" when valid.
func validateThen(v *spec.Validator, op spec.Operation, path string, then []ResponseDef) (msg, code string) {
	for i, t := range then {
		at := "then[" + strconv.Itoa(i) + "]"
		switch {
		case t.Status == 0:
			return at + ".status is required", "invalid_body"
		case op.Op.OperationID == spec.SubscribeEventsOperationID && t.Status < http.StatusBadRequest:
			return "GET /api/v2/events accepts only error stubs (status >= 400); " + at + " has status " + strconv.Itoa(t.Status), "invalid_match"
		}
		if msg, code := prepareEntry(v, op, path, at, &then[i]); msg != "" {
			return msg, code
		}
	}
	return "", ""
}

// SameTarget reports whether a and b address the same registration slot:
// equal method, path and kind, and an equivalent request matcher.
func SameTarget(a, b Expectation) bool {
	return a.Method == b.Method && a.Path == b.Path && a.Kind == b.Kind &&
		requestMatcherEqual(a.Request, b.Request)
}
