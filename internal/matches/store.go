package matches

import "sync"

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
