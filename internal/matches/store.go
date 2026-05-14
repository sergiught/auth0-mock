package matches

import (
	"sort"
	"sync"
)

// Store holds registered Expectation entries in memory as an ordered list
// (oldest to newest) per (method, path) key. Safe for concurrent use.
type Store struct {
	mu       sync.RWMutex
	exact    map[key][]Expectation
	template map[key][]Expectation
}

type key struct {
	Method string
	Path   string
}

// NewStore returns an empty Store.
func NewStore() *Store {
	return &Store{
		exact:    make(map[key][]Expectation),
		template: make(map[key][]Expectation),
	}
}

func (s *Store) bucket(k Kind) map[key][]Expectation {
	if k == KindTemplate {
		return s.template
	}
	return s.exact
}

// Put inserts exp into its (method, path) list. If an entry with an equal
// Request matcher already exists for that key it is removed first, so the
// re-registered expectation becomes the newest. The path is treated as exact
// or template based on exp.Kind.
func (s *Store) Put(exp Expectation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.bucket(exp.Kind)
	k := key{Method: exp.Method, Path: exp.Path}
	var kept []Expectation
	for _, e := range m[k] {
		if !requestMatcherEqual(e.Request, exp.Request) {
			kept = append(kept, e)
		}
	}
	m[k] = append(kept, exp)
}

// Find returns the Expectation that should serve (method, concretePath) given
// the incoming request. It considers the exact-path list and the template
// list registered against opTemplate, keeps only entries whose Request matches
// req, and applies a 4-tier precedence:
//
//  1. exact path, request matcher, newest
//  2. exact path, catch-all, newest
//  3. template path, request matcher, newest
//  4. template path, catch-all, newest
//
// Returns nil when nothing matches.
func (s *Store) Find(method, concretePath, opTemplate string, req MatchableRequest) *Expectation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	exact := s.exact[key{Method: method, Path: concretePath}]
	tmpl := s.template[key{Method: method, Path: opTemplate}]
	if e := newestMatch(exact, req, false); e != nil {
		return e
	}
	if e := newestMatch(exact, req, true); e != nil {
		return e
	}
	if e := newestMatch(tmpl, req, false); e != nil {
		return e
	}
	return newestMatch(tmpl, req, true)
}

// newestMatch scans list newest-first and returns the first entry that both
// matches req and has the requested catch-all-ness (catchAll == (Request is
// nil)). Returns nil if none qualify.
func newestMatch(list []Expectation, req MatchableRequest, catchAll bool) *Expectation {
	for i := len(list) - 1; i >= 0; i-- {
		e := list[i]
		if (e.Request == nil) != catchAll {
			continue
		}
		if e.Request.Matches(req) {
			out := e
			return &out
		}
	}
	return nil
}

// List returns every registered Expectation, sorted by (method, path).
func (s *Store) List() []Expectation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Expectation, 0, len(s.exact)+len(s.template))
	for _, list := range s.exact {
		out = append(out, list...)
	}
	for _, list := range s.template {
		out = append(out, list...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Method != out[j].Method {
			return out[i].Method < out[j].Method
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// ResetEndpoint removes every Expectation keyed by (method, path) within the
// map indicated by kind. Calls for unregistered keys are no-ops.
func (s *Store) ResetEndpoint(method, path string, kind Kind) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.bucket(kind), key{Method: method, Path: path})
}

// ResetAll clears every registered Expectation.
func (s *Store) ResetAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exact = make(map[key][]Expectation)
	s.template = make(map[key][]Expectation)
}
