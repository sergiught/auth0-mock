package matches

import (
	"bytes"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"
)

// RedactedValue replaces the value of sensitive headers wherever they are
// echoed back (stored journal headers and near-miss want/got values).
const RedactedValue = "[REDACTED]"

// IsRedactedHeader reports whether the header's value must never be echoed.
func IsRedactedHeader(name string) bool {
	return strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "Cookie")
}

// Difference describes one way a request failed to satisfy a stub.
//
// Field is "path", "query.<key>", "headers.<Canonical-Key>", "body" or a
// dotted body path such as "body.a.b". Reason is "mismatch", "missing" or
// "invalid_json". Got is omitted when Reason is "missing".
type Difference struct {
	Field  string          `json:"field"`
	Reason string          `json:"reason"`
	Want   json.RawMessage `json:"want,omitempty"`
	Got    json.RawMessage `json:"got,omitempty"`
}

// NearMiss is a stub that shares the request's method and operation but did
// not match, together with what differed. Path is the stub's registered path
// (concrete or template).
type NearMiss struct {
	ExpectationID string       `json:"expectationId"`
	Method        string       `json:"method"`
	Path          string       `json:"path"`
	Differences   []Difference `json:"differences"`
}

// Explain returns every way req fails to satisfy the matcher, sorted by
// Field. An empty result means the matcher matches; a nil matcher
// (catch-all) always does.
func (rm *RequestMatcher) Explain(req MatchableRequest) []Difference {
	if rm == nil {
		return nil
	}
	var diffs []Difference
	for k, want := range rm.Query {
		got := req.Query.Get(k)
		if got == want {
			continue
		}
		d := Difference{Field: "query." + k, Reason: "mismatch", Want: rawString(want), Got: rawString(got)}
		if _, ok := req.Query[k]; !ok {
			d.Reason, d.Got = "missing", nil
		}
		diffs = append(diffs, d)
	}
	for k, want := range rm.Headers {
		canonical := http.CanonicalHeaderKey(k)
		got := req.Headers.Get(k)
		if got == want {
			continue
		}
		d := Difference{Field: "headers." + canonical, Reason: "mismatch", Want: rawString(want), Got: rawString(got)}
		if _, ok := req.Headers[canonical]; !ok {
			d.Reason, d.Got = "missing", nil
		}
		if IsRedactedHeader(canonical) {
			d.Want = rawString(RedactedValue)
			if d.Got != nil {
				d.Got = rawString(RedactedValue)
			}
		}
		diffs = append(diffs, d)
	}
	diffs = append(diffs, rm.explainBody(req.Body)...)
	slices.SortFunc(diffs, func(a, b Difference) int { return strings.Compare(a.Field, b.Field) })
	return diffs
}

func (rm *RequestMatcher) explainBody(gotBody []byte) []Difference {
	body := bytes.TrimSpace(rm.Body)
	if len(body) == 0 || string(body) == "null" {
		return nil
	}
	var want, got any
	if err := json.Unmarshal(body, &want); err != nil {
		return []Difference{{Field: "body", Reason: "invalid_json"}}
	}
	if err := json.Unmarshal(gotBody, &got); err != nil {
		return []Difference{{Field: "body", Reason: "invalid_json"}}
	}
	return explainValue("body", want, got)
}

// explainValue walks want against got: object keys recurse (subset
// semantics), everything else must be deeply equal.
func explainValue(field string, want, got any) []Difference {
	wantObj, ok := want.(map[string]any)
	if !ok {
		if reflect.DeepEqual(want, got) {
			return nil
		}
		return []Difference{{Field: field, Reason: "mismatch", Want: rawJSON(want), Got: rawJSON(got)}}
	}
	gotObj, ok := got.(map[string]any)
	if !ok {
		return []Difference{{Field: field, Reason: "mismatch", Want: rawJSON(want), Got: rawJSON(got)}}
	}
	var diffs []Difference
	for k, w := range wantObj {
		g, present := gotObj[k]
		if !present {
			diffs = append(diffs, Difference{Field: field + "." + k, Reason: "missing", Want: rawJSON(w)})
			continue
		}
		diffs = append(diffs, explainValue(field+"."+k, w, g)...)
	}
	return diffs
}

func rawString(s string) json.RawMessage { return rawJSON(s) }

func rawJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
