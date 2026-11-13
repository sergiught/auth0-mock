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
// by arrayContains, scalars equal). It finds a maximum pairing with
// augmenting paths, so a large request array cannot make it blow up.
func arrayContains(want, got []any) bool {
	if len(want) > len(got) {
		return false
	}
	fits := make([][]int, len(want))
	for w := range want {
		for g := range got {
			if len(explainValue("", want[w], got[g], true)) == 0 {
				fits[w] = append(fits[w], g)
			}
		}
		if len(fits[w]) == 0 {
			return false
		}
	}
	owner := make([]int, len(got))
	for g := range owner {
		owner[g] = -1
	}
	for w := range want {
		if !augment(w, fits, owner, make([]bool, len(got))) {
			return false
		}
	}
	return true
}

// augment tries to give want item w a request item, moving earlier
// pairings to other items they also fit when needed.
func augment(w int, fits [][]int, owner []int, seen []bool) bool {
	for _, g := range fits[w] {
		if seen[g] {
			continue
		}
		seen[g] = true
		if owner[g] == -1 || augment(owner[g], fits, owner, seen) {
			owner[g] = w
			return true
		}
	}
	return false
}
