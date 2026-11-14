package matches

import (
	"fmt"
	"reflect"
	"slices"
)

// Values of RequestMatcher.Arrays. An omitted value means ArraysExact.
const (
	// ArraysExact compares body arrays for deep equality.
	ArraysExact = "exact"
	// ArraysContains lets every wanted array item pair with a different
	// request item, in any order, with extra request items allowed.
	ArraysContains = "contains"
)

// errArraysInvalid reports an arrays value sent as something other than a
// non-empty JSON string, such as null, "" or a number.
var errArraysInvalid = fmt.Errorf("arrays: must be %q or %q", ArraysExact, ArraysContains)

// validateArrays reports an arrays value that is not "exact" or
// "contains", or one sent as null, "" or a non-string. A nil matcher is
// valid.
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

// maxContainsWork caps the work one contains-mode body match may do: every
// array item looked at, value compared by fitsValue and pairing step costs
// one unit. A
// body that needs more does not match, so a huge request array cannot hold
// the store's read lock for long.
const maxContainsWork = 100_000

// containsBudget is the work a contains-mode body match has left. A nil
// budget means arrays are compared exactly.
type containsBudget struct{ left int }

func newContainsBudget() *containsBudget { return &containsBudget{left: maxContainsWork} }

// spend takes n units and reports whether the budget covered them.
func (b *containsBudget) spend(n int) bool {
	b.left -= n
	return b.left >= 0
}

// arrayContains reports whether every item of want can be paired with a
// different item of got that matches it (objects subset-matched, arrays
// by arrayContains, scalars equal). A scalar only fits an equal scalar, so
// scalars are paired by counting; objects and arrays are paired with
// augmenting paths. It gives up, reporting no match, once cb runs out.
func arrayContains(want, got []any, cb *containsBudget) bool {
	if len(want) > len(got) || !cb.spend(len(want)+len(got)) {
		return false
	}
	scalars := make(map[any]int)
	var gotComposite []any
	for _, g := range got {
		if isComposite(g) {
			gotComposite = append(gotComposite, g)
		} else {
			scalars[g]++
		}
	}
	var wantComposite []any
	for _, w := range want {
		if isComposite(w) {
			wantComposite = append(wantComposite, w)
			continue
		}
		if scalars[w] == 0 {
			return false
		}
		scalars[w]--
	}
	return pairComposites(wantComposite, gotComposite, cb)
}

// fitsValue reports whether got matches want in contains mode: objects
// subset-matched, arrays by arrayContains, everything else deeply equal.
// Unlike explainValue it builds no differences, so comparing a candidate
// pair costs only the values it visits, each one unit of cb.
func fitsValue(want, got any, cb *containsBudget) bool {
	if !cb.spend(1) {
		return false
	}
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return false
		}
		// Sorted keys keep the budget spent in the same order on every
		// run, so a near-cap comparison always gives the same answer. The
		// stack buffer spares small objects an allocation per candidate.
		var buf [16]string
		keys := buf[:0]
		for k := range w {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			gv, present := g[k]
			if !present || !fitsValue(w[k], gv, cb) {
				return false
			}
		}
		return true
	case []any:
		g, ok := got.([]any)
		return ok && arrayContains(w, g, cb)
	}
	return reflect.DeepEqual(want, got)
}

// isComposite reports whether v is a JSON object or array.
func isComposite(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return true
	}
	return false
}

// pairComposites reports whether every object or array in want pairs with
// a different item of got that it matches.
func pairComposites(want, got []any, cb *containsBudget) bool {
	if len(want) > len(got) {
		return false
	}
	fits := make([][]int, len(want))
	for w := range want {
		for g := range got {
			fit := fitsValue(want[w], got[g], cb)
			if cb.left < 0 {
				return false
			}
			if fit {
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
		if !augment(w, fits, owner, make([]bool, len(got)), cb) {
			return false
		}
	}
	return true
}

// augment tries to give want item w a request item, moving earlier
// pairings to other items they also fit when needed. Each item it looks at
// costs one unit of cb.
func augment(w int, fits [][]int, owner []int, seen []bool, cb *containsBudget) bool {
	for _, g := range fits[w] {
		if seen[g] {
			continue
		}
		if !cb.spend(1) {
			return false
		}
		seen[g] = true
		if owner[g] == -1 || augment(owner[g], fits, owner, seen, cb) {
			owner[g] = w
			return true
		}
	}
	return false
}
