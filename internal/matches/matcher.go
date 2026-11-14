package matches

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
)

// MatchableRequest carries the parts of an incoming request that a
// RequestMatcher is compared against.
type MatchableRequest struct {
	Query   url.Values
	Headers http.Header
	Body    []byte
}

// IsEmpty reports whether the matcher carries no criteria. An empty matcher is
// equivalent to a nil catch-all and is normalized to nil on registration.
func (rm *RequestMatcher) IsEmpty() bool {
	if rm == nil {
		return true
	}
	body := bytes.TrimSpace(rm.Body)
	return len(rm.Query) == 0 &&
		len(rm.Headers) == 0 &&
		len(rm.Absent) == 0 &&
		(len(body) == 0 || string(body) == "null")
}

// Matches reports whether req satisfies matcher, i.e. Explain finds no
// differences. A nil matcher (catch-all) always matches. Header keys are
// subset-matched (every matcher key must be present with an equal value;
// extras allowed). A query value matches when any of the request's values
// for that key equals it; a query list needs every listed value present.
// The body is subset-matched recursively. Every path in Absent must be
// missing from the request.
//
// Header lookup is case-insensitive — http.Header.Get canonicalises the key
// against the canonical MIME header form, so matcher entry "X-Tenant: acme"
// matches an incoming header named "x-tenant" or "X-TENANT".
func (rm *RequestMatcher) Matches(req MatchableRequest) bool {
	return len(rm.Explain(req)) == 0
}

// requestMatcherEqual reports whether two matchers are equivalent. Two nil
// (catch-all) matchers are equal; otherwise the query and header maps must
// be equal (query lists as multisets), absent paths the same set, and the bodies must be semantically equal JSON.
func requestMatcherEqual(a, b *RequestMatcher) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if !queryEqual(a.Query, b.Query) {
		return false
	}
	if !reflect.DeepEqual(a.Headers, b.Headers) {
		return false
	}
	if !absentEqual(a.Absent, b.Absent) {
		return false
	}
	return jsonEqual(a.Body, b.Body)
}

// jsonEqual reports whether two raw JSON messages are semantically equal.
// Empty / unparseable inputs compare equal only to each other.
func jsonEqual(a, b json.RawMessage) bool {
	var av, bv any
	ae := json.Unmarshal(bytes.TrimSpace(a), &av)
	be := json.Unmarshal(bytes.TrimSpace(b), &bv)
	if ae != nil || be != nil {
		return ae != nil && be != nil
	}
	return reflect.DeepEqual(av, bv)
}
