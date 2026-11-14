package matches

import "reflect"

// subsetMatch reports whether want is a recursive subset of got: every object
// key in want must be present in got with a subset-matching value; arrays and
// scalars must be deeply equal.
func subsetMatch(want, got any) bool {
	return subsetMatchArrays(want, got, false)
}

// subsetMatchArrays is subsetMatch with an arrays mode. With contains set, a
// want array matches a got array when some injective choice of got items
// pairs with every want item; it is found by brute-force permutation, as an
// oracle independent of arrayContains.
func subsetMatchArrays(want, got any, contains bool) bool {
	if w, ok := want.(map[string]any); ok {
		g, ok := got.(map[string]any)
		if !ok {
			return false
		}
		for k, wv := range w {
			gv, ok := g[k]
			if !ok || !subsetMatchArrays(wv, gv, contains) {
				return false
			}
		}
		return true
	}
	if w, ok := want.([]any); ok && contains {
		g, ok := got.([]any)
		if !ok {
			return false
		}
		return permute(w, g, map[int]bool{})
	}
	return reflect.DeepEqual(want, got)
}

func permute(want, got []any, used map[int]bool) bool {
	if len(want) == 0 {
		return true
	}
	for i := range got {
		if used[i] || !subsetMatchArrays(want[0], got[i], true) {
			continue
		}
		used[i] = true
		ok := permute(want[1:], got, used)
		delete(used, i)
		if ok {
			return true
		}
	}
	return false
}
