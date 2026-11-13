package matches

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
)

// wireMatcher is the JSON shape of a RequestMatcher. Query values are raw so
// each can be a string or a list of strings. Later matcher fields belong here
// too, so MarshalJSON and UnmarshalJSON stay the single place that knows the
// wire format.
type wireMatcher struct {
	Query   map[string]json.RawMessage `json:"query,omitempty"`
	Headers map[string]string          `json:"headers,omitempty"`
	Body    json.RawMessage            `json:"body,omitempty"`
	Absent  []string                   `json:"absent,omitempty"`
	Arrays  json.RawMessage            `json:"arrays,omitempty"`
}

// MarshalJSON writes a one-element query list as a plain string, so a matcher
// registered with strings reads back unchanged.
func (rm RequestMatcher) MarshalJSON() ([]byte, error) {
	w := wireMatcher{Headers: rm.Headers, Body: rm.Body, Absent: rm.Absent}
	if rm.Arrays != "" {
		w.Arrays, _ = json.Marshal(rm.Arrays)
	}
	if len(rm.Query) > 0 {
		w.Query = make(map[string]json.RawMessage, len(rm.Query))
		for k, vals := range rm.Query {
			var v any = vals
			if len(vals) == 1 {
				v = vals[0]
			}
			b, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			w.Query[k] = b
		}
	}
	return json.Marshal(w)
}

// wireMatcherIn reads absent raw, so a mistyped list is reported like a
// mistyped query value rather than failing the whole decode.
type wireMatcherIn struct {
	wireMatcher
	Absent json.RawMessage `json:"absent,omitempty"`
}

// UnmarshalJSON reads each query value as a string or a list of strings. An
// invalid query or absent value does not fail the decode; it is remembered
// and Prepare turns it into an invalid_request_match error.
func (rm *RequestMatcher) UnmarshalJSON(data []byte) error {
	var w wireMatcherIn
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*rm = RequestMatcher{Headers: w.Headers, Body: w.Body}
	if len(w.Absent) > 0 {
		var items []*string
		if err := json.Unmarshal(w.Absent, &items); err != nil || items == nil || slices.Contains(items, nil) {
			rm.decodeErr = errors.New("absent: must be a list of strings")
		}
		for _, v := range items {
			if v != nil {
				rm.Absent = append(rm.Absent, *v)
			}
		}
	}
	if len(w.Arrays) > 0 && json.Unmarshal(w.Arrays, &rm.Arrays) != nil {
		rm.arraysErr = errArraysNotString
	}
	if w.Query == nil {
		return nil
	}
	rm.Query = make(map[string][]string, len(w.Query))
	for _, k := range slices.Sorted(maps.Keys(w.Query)) {
		vals, err := decodeQueryValue(w.Query[k])
		if err != nil {
			rm.decodeErr = errors.Join(rm.decodeErr, fmt.Errorf("query %q: %w", k, err))
			continue
		}
		rm.Query[k] = vals
	}
	return nil
}

func decodeQueryValue(raw json.RawMessage) ([]string, error) {
	raw = bytes.TrimSpace(raw)
	switch {
	case len(raw) > 0 && raw[0] == '"':
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return []string{s}, nil
	case len(raw) > 0 && raw[0] == '[':
		var items []*string
		if err := json.Unmarshal(raw, &items); err != nil || slices.Contains(items, nil) {
			return nil, errors.New("list entries must all be strings")
		}
		vals := make([]string, len(items))
		for i, v := range items {
			vals[i] = *v
		}
		return vals, nil
	}
	return nil, errors.New("value must be a string or a list of strings")
}

// validateQuery reports an invalid query value met while decoding, or an
// empty list. A nil matcher is valid.
func (rm *RequestMatcher) validateQuery() error {
	if rm == nil {
		return nil
	}
	if rm.decodeErr != nil {
		return rm.decodeErr
	}
	for _, k := range rm.queryKeys() {
		if len(rm.Query[k]) == 0 {
			return fmt.Errorf("query %q: list must not be empty", k)
		}
	}
	return nil
}

// queryKeys returns the matcher's query keys, sorted.
func (rm *RequestMatcher) queryKeys() []string {
	return slices.Sorted(maps.Keys(rm.Query))
}

// containsAll reports whether got holds every value in want, counting
// duplicates: want ["a","a"] needs "a" twice. Order and extra values in got
// do not matter.
func containsAll(got, want []string) bool {
	if len(want) > len(got) {
		return false
	}
	counts := make(map[string]int, len(got))
	for _, g := range got {
		counts[g]++
	}
	for _, w := range want {
		if counts[w] == 0 {
			return false
		}
		counts[w]--
	}
	return true
}

// queryEqual reports whether two query maps hold the same values per key,
// ignoring the order of each list.
func queryEqual(a, b map[string][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || len(av) != len(bv) || !containsAll(av, bv) {
			return false
		}
	}
	return true
}

// rawValues renders values for a near-miss: a single value as a JSON string,
// anything else as a JSON list.
func rawValues(vals []string) json.RawMessage {
	if len(vals) == 1 {
		return rawString(vals[0])
	}
	if vals == nil {
		vals = []string{}
	}
	return rawJSON(vals)
}
