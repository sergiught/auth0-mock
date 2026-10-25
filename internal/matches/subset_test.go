package matches

import "reflect"

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
