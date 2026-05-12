package matches

import (
	"sort"
	"sync"
)

// Store holds registered Match entries in memory. Safe for concurrent use.
type Store struct {
	mu       sync.RWMutex
	exact    map[key]Match
	template map[key]Match
}

type key struct {
	Method string
	Path   string
}

// NewStore returns an empty Store.
func NewStore() *Store {
	return &Store{
		exact:    make(map[key]Match),
		template: make(map[key]Match),
	}
}

// Put inserts or overwrites a Match. The path is treated as exact or
// template based on m.Kind.
func (s *Store) Put(m Match) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key{Method: m.Method, Path: m.Path}
	if m.Kind == KindTemplate {
		s.template[k] = m
		return
	}
	s.exact[k] = m
}

// Find returns the registered Match for (method, concretePath) preferring
// an exact-path match; if none is found it falls back to the template
// registered against opTemplate. Returns nil if neither is registered.
func (s *Store) Find(method, concretePath, opTemplate string) *Match {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if m, ok := s.exact[key{Method: method, Path: concretePath}]; ok {
		return &m
	}
	if m, ok := s.template[key{Method: method, Path: opTemplate}]; ok {
		return &m
	}
	return nil
}

// List returns every registered Match, sorted by (method, path).
func (s *Store) List() []Match {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Match, 0, len(s.exact)+len(s.template))
	for _, m := range s.exact {
		out = append(out, m)
	}
	for _, m := range s.template {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Method != out[j].Method {
			return out[i].Method < out[j].Method
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// ResetEndpoint removes the registered Match keyed by (method, path) within
// the map indicated by kind. Calls for unregistered keys are no-ops.
func (s *Store) ResetEndpoint(method, path string, kind Kind) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key{Method: method, Path: path}
	if kind == KindTemplate {
		delete(s.template, k)
		return
	}
	delete(s.exact, k)
}

// ResetAll clears every registered Match.
func (s *Store) ResetAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exact = make(map[key]Match)
	s.template = make(map[key]Match)
}
