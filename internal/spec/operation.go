package spec

import (
	"iter"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// Operation is one (method, path, openapi3.Operation) triple from the spec.
type Operation struct {
	Method   string // upper-case HTTP verb, e.g. "GET"
	Template string // full path including BasePath, e.g. "/api/v2/users/{id}"
	Op       *openapi3.Operation
}

// Operations yields every operation in the spec. The Template includes the
// spec's BasePath so callers don't have to recombine.
func (s *Spec) Operations() iter.Seq[Operation] {
	return func(yield func(Operation) bool) {
		for path, item := range s.Doc.Paths.Map() {
			full := s.BasePath + path
			for method, op := range item.Operations() {
				m := strings.ToUpper(method)
				if !yield(Operation{Method: m, Template: full, Op: op}) {
					return
				}
			}
		}
	}
}
