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
		(len(body) == 0 || string(body) == "null")
}

// Matches reports whether req satisfies the matcher. A nil matcher (catch-all)
// always matches. Query and header keys are subset-matched (every matcher key
// must be present with an equal value; extras are allowed); the body is
// subset-matched via subsetMatch.
//
// Header lookup is case-insensitive — http.Header.Get canonicalises the key
// against the canonical MIME header form, so a matcher entry "X-Tenant: acme"
// matches an incoming header named "x-tenant" or "X-TENANT".
func (rm *RequestMatcher) Matches(req MatchableRequest) bool {
	if rm == nil {
		return true
	}
	for k, v := range rm.Query {
		if req.Query.Get(k) != v {
			return false
		}
	}
	for k, v := range rm.Headers {
		if req.Headers.Get(k) != v {
			return false
		}
	}
	body := bytes.TrimSpace(rm.Body)
	if len(body) == 0 || string(body) == "null" {
		return true
	}
	var want, got any
	if err := json.Unmarshal(body, &want); err != nil {
		return false
	}
	if err := json.Unmarshal(req.Body, &got); err != nil {
		return false
	}
	return subsetMatch(want, got)
}

// subsetMatch reports whether want is a recursive subset of got: every object
// key in want must be present in got with a subset-matching value; arrays and
// scalars must be deeply equal.
func subsetMatch(want, got any) bool {
	if w, ok := want.(map[string]any); ok {
		g, ok := got.(map[string]any)
		if !ok {
			return false
		}
		for k, wv := range w {
			gv, ok := g[k]
			if !ok || !subsetMatch(wv, gv) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(want, got)
}

// requestMatcherEqual reports whether two matchers are equivalent. Two nil
// (catch-all) matchers are equal; otherwise the query and header maps must
// be equal and the bodies must be semantically equal JSON.
func requestMatcherEqual(a, b *RequestMatcher) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if !reflect.DeepEqual(a.Query, b.Query) {
		return false
	}
	if !reflect.DeepEqual(a.Headers, b.Headers) {
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
