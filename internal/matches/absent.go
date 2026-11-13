package matches

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/sergiught/auth0-mock/internal/spec"
)

const (
	absentQueryPrefix  = "query."
	absentHeaderPrefix = "headers."
	absentBodyPrefix   = "body."
)

// unmatchableHeaders are headers Go moves out of the request's header map
// (Host into Request.Host; Transfer-Encoding and Trailer while reading the
// body), so the matcher would always see them as absent.
var unmatchableHeaders = map[string]bool{"Host": true, "Transfer-Encoding": true, "Trailer": true}

// validateAbsentForOp rejects body.* absent paths on the event stream: its
// stubs are looked up without reading a body, so such a path would always
// count as absent.
func (rm *RequestMatcher) validateAbsentForOp(op spec.Operation) error {
	if rm == nil || op.Op.OperationID != spec.SubscribeEventsOperationID {
		return nil
	}
	for _, p := range rm.Absent {
		if strings.HasPrefix(p, absentBodyPrefix) {
			return fmt.Errorf("absent %q: GET /api/v2/events stubs are matched without a body", p)
		}
	}
	return nil
}

// canonicalAbsent returns an absent path with a header name in canonical form,
// so "headers.x-foo" and "headers.X-Foo" are the same path. Other paths are
// returned unchanged.
func canonicalAbsent(path string) string {
	if name, ok := strings.CutPrefix(path, absentHeaderPrefix); ok {
		return absentHeaderPrefix + http.CanonicalHeaderKey(name)
	}
	return path
}

// canonicalAbsentList returns the paths in canonical form, keeping the
// first occurrence of each in order and dropping repeats.
func canonicalAbsentList(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if c := canonicalAbsent(p); !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	return out
}

// absentEqual reports whether two absent lists hold the same paths,
// ignoring order and duplicates.
func absentEqual(a, b []string) bool {
	norm := func(l []string) []string {
		l = canonicalAbsentList(l)
		slices.Sort(l)
		return slices.Compact(l)
	}
	return slices.Equal(norm(a), norm(b))
}

// absentQueryKeys returns the query keys named by "query.<key>" absent paths.
func (rm *RequestMatcher) absentQueryKeys() []string {
	var keys []string
	for _, p := range rm.Absent {
		if k, ok := strings.CutPrefix(p, absentQueryPrefix); ok {
			keys = append(keys, k)
		}
	}
	return keys
}

// validateAbsent checks every absent path: it needs a known prefix and no
// empty segment, and must not name something the matcher also requires. A nil
// matcher is valid.
func (rm *RequestMatcher) validateAbsent() error {
	if rm == nil {
		return nil
	}
	var body any
	hasBody := false
	if b := bytes.TrimSpace(rm.Body); len(b) > 0 && string(b) != "null" {
		hasBody = json.Unmarshal(b, &body) == nil
	}
	for _, p := range rm.Absent {
		if err := rm.validateAbsentPath(p, body, hasBody); err != nil {
			return fmt.Errorf("absent %q: %w", p, err)
		}
	}
	return nil
}

func (rm *RequestMatcher) validateAbsentPath(p string, body any, hasBody bool) error {
	switch {
	case strings.HasPrefix(p, absentQueryPrefix):
		key := strings.TrimPrefix(p, absentQueryPrefix)
		if key == "" {
			return errors.New("query key is empty")
		}
		if _, ok := rm.Query[key]; ok {
			return errors.New("the matcher also requires this query parameter")
		}
	case strings.HasPrefix(p, absentHeaderPrefix):
		name := http.CanonicalHeaderKey(strings.TrimPrefix(p, absentHeaderPrefix))
		if name == "" {
			return errors.New("header name is empty")
		}
		if unmatchableHeaders[name] {
			return fmt.Errorf("the %s header cannot be checked for absence", name)
		}
		for h := range rm.Headers {
			if http.CanonicalHeaderKey(h) == name {
				return errors.New("the matcher also requires this header")
			}
		}
	case strings.HasPrefix(p, absentBodyPrefix):
		segs := strings.Split(strings.TrimPrefix(p, absentBodyPrefix), ".")
		if slices.Contains(segs, "") {
			return errors.New("path has an empty segment")
		}
		if hasBody && bodySets(body, segs) {
			return errors.New("the matcher's body also sets this path")
		}
	default:
		return errors.New("path must start with query, headers or body followed by a dot")
	}
	return nil
}

// bodySets reports whether the matcher body sets the value at segs, or an
// ancestor of it. A non-object met on the way down has no fields, so
// everything below it is absent and does not count as set.
func bodySets(body any, segs []string) bool {
	for _, s := range segs {
		obj, isObj := body.(map[string]any)
		if !isObj {
			return false
		}
		next, present := obj[s]
		if !present {
			return false
		}
		body = next
	}
	return true
}

// bodyValue returns the value at the dotted path in a request body, JSON null
// included. A missing or non-JSON body holds nothing. Numbers stay
// json.Number, so one too large for float64 cannot hide the path and the
// value is reported as sent.
func bodyValue(raw []byte, path string) (any, bool) {
	var cur any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if dec.Decode(&cur) != nil {
		return nil, false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, false
	}
	for _, s := range strings.Split(strings.TrimPrefix(path, absentBodyPrefix), ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = obj[s]; !ok {
			return nil, false
		}
	}
	return cur, true
}

// explainAbsent returns a "present" difference for each absent path the
// request does carry.
func (rm *RequestMatcher) explainAbsent(req MatchableRequest) []Difference {
	var diffs []Difference
	for _, p := range canonicalAbsentList(rm.Absent) {
		var got json.RawMessage
		var present bool
		switch {
		case strings.HasPrefix(p, absentQueryPrefix):
			var vals []string
			if vals, present = req.Query[strings.TrimPrefix(p, absentQueryPrefix)]; present {
				got = rawValues(vals)
			}
		case strings.HasPrefix(p, absentHeaderPrefix):
			name := strings.TrimPrefix(p, absentHeaderPrefix)
			var vals []string
			if vals, present = req.Headers[name]; present {
				got = rawString(strings.Join(vals, ", "))
				if IsRedactedHeader(name) {
					got = rawString(RedactedValue)
				}
			}
		case strings.HasPrefix(p, absentBodyPrefix):
			var v any
			if v, present = bodyValue(req.Body, p); present {
				got = rawJSON(v)
			}
		}
		if present {
			diffs = append(diffs, Difference{Field: p, Reason: "present", Got: got})
		}
	}
	return diffs
}
