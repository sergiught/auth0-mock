package matches

import (
	"errors"
	"fmt"
)

// Values of RequestMatcher.Arrays. An omitted value means ArraysExact.
const (
	// ArraysExact compares body arrays for deep equality.
	ArraysExact = "exact"
	// ArraysContains lets every wanted array item pair with a different
	// request item, in any order, with extra request items allowed.
	ArraysContains = "contains"
)

var errArraysNotString = errors.New("arrays must be a string")

// validateArrays reports an arrays value that is not "exact" or
// "contains", or one that was not a JSON string. A nil matcher is valid.
func (rm *RequestMatcher) validateArrays() error {
	if rm == nil {
		return nil
	}
	if rm.arraysErr != nil {
		return rm.arraysErr
	}
	switch rm.Arrays {
	case "", ArraysExact, ArraysContains:
		return nil
	}
	return fmt.Errorf("arrays %q: must be %q or %q", rm.Arrays, ArraysExact, ArraysContains)
}

// arrayContains reports whether every item of want can be paired with a
// different item of got that matches it (objects subset-matched, arrays
// by arrayContains, scalars equal).
func arrayContains(want, got []any) bool {
	return assignItems(want, got, make([]bool, len(got)))
}

// assignItems pairs want[0] with each unused matching item of got in
// turn and recurses on the rest, backtracking when a pairing dead-ends.
func assignItems(want, got []any, used []bool) bool {
	if len(want) == 0 {
		return true
	}
	for i, g := range got {
		if used[i] || len(explainValue("", want[0], g, true)) > 0 {
			continue
		}
		used[i] = true
		if assignItems(want[1:], got, used) {
			return true
		}
		used[i] = false
	}
	return false
}
