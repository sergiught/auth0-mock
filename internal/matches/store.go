package matches

import (
	"sort"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
)

// Store holds registered Expectation entries in memory as an ordered list
// (oldest to newest) per (method, path) key. Safe for concurrent use.
//
// Hits is an independent map of per-ID atomic counters — kept off
// Expectation itself because Expectation is passed by value and
// atomically-incremented fields on a copy are meaningless. Read via
// hits[id].Load(), written via hits[id].Add(1), guarded against
// concurrent map mutation by mu (only Put / DeleteByID / ResetAll
// touch the map shape).
type Store struct {
	mu       sync.RWMutex
	exact    map[key][]Expectation
	template map[key][]Expectation
	hits     map[string]*atomic.Int64
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
		hits:     make(map[string]*atomic.Int64),
	}
}

// bucket returns the map that holds expectations of kind k.
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
//
// Generates a fresh UUID for exp.ID before storing — any inbound ID on exp
// is overwritten so callers can't forge or reuse IDs. Returns the stored
// Expectation (carrying the assigned ID) so callers can hand it to the
// network layer without re-locking.
func (s *Store) Put(exp Expectation) Expectation {
	if exp.Request.IsEmpty() {
		exp.Request = nil
	}
	exp.ID = uuid.NewString()
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.bucket(exp.Kind)
	k := key{Method: exp.Method, Path: exp.Path}
	var kept []Expectation
	for _, e := range m[k] {
		if !requestMatcherEqual(e.Request, exp.Request) {
			kept = append(kept, e)
		} else {
			// Re-registration of the same matcher tuple — drop the
			// old hit counter alongside the superseded entry so the
			// fresh registration starts at zero.
			delete(s.hits, e.ID)
		}
	}
	m[k] = append(kept, exp)
	s.hits[exp.ID] = &atomic.Int64{}
	return exp
}

// DeleteByID removes the expectation with the given id from any bucket
// and drops its hit counter. Returns true if an entry was removed,
// false if no expectation with that id existed (idempotent — callers
// don't need to pre-check).
func (s *Store) DeleteByID(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range []map[key][]Expectation{s.exact, s.template} {
		for k, list := range m {
			for i, e := range list {
				if e.ID == id {
					m[k] = append(list[:i], list[i+1:]...)
					if len(m[k]) == 0 {
						delete(m, k)
					}
					delete(s.hits, id)
					return true
				}
			}
		}
	}
	return false
}

// GetByID returns the expectation with the given id (a copy) with its
// current Hits populated, or nil if no such expectation exists.
func (s *Store) GetByID(id string) *Expectation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, m := range []map[key][]Expectation{s.exact, s.template} {
		for _, list := range m {
			for _, e := range list {
				if e.ID == id {
					out := e
					if counter := s.hits[id]; counter != nil {
						out.Hits = counter.Load()
					}
					return &out
				}
			}
		}
	}
	return nil
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
// Returns nil when nothing matches. Also atomically increments the matched
// expectation's hit counter — the returned *Expectation carries the
// post-increment Hits value so callers can log "served call #N" without
// a second lookup.
func (s *Store) Find(method, concretePath, opTemplate string, req MatchableRequest) *Expectation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	exact := s.exact[key{Method: method, Path: concretePath}]
	tmpl := s.template[key{Method: method, Path: opTemplate}]
	matched := firstNonNil(
		newestMatch(exact, req, false),
		newestMatch(exact, req, true),
		newestMatch(tmpl, req, false),
		newestMatch(tmpl, req, true),
	)
	if matched != nil {
		if counter := s.hits[matched.ID]; counter != nil {
			matched.Hits = counter.Add(1)
		}
	}
	return matched
}

// firstNonNil returns the first non-nil *Expectation in args, or nil
// if all are nil. Keeps the precedence tiers readable in Find without
// nesting four if-blocks.
func firstNonNil(xs ...*Expectation) *Expectation {
	for _, x := range xs {
		if x != nil {
			return x
		}
	}
	return nil
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

// List returns every registered Expectation, sorted by (method, path),
// with each entry's Hits populated from the store's per-ID counter.
func (s *Store) List() []Expectation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := 0
	for _, list := range s.exact {
		total += len(list)
	}
	for _, list := range s.template {
		total += len(list)
	}
	out := make([]Expectation, 0, total)
	for _, list := range s.exact {
		out = append(out, list...)
	}
	for _, list := range s.template {
		out = append(out, list...)
	}
	for i := range out {
		if counter := s.hits[out[i].ID]; counter != nil {
			out[i].Hits = counter.Load()
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Method != out[j].Method {
			return out[i].Method < out[j].Method
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// ResetEndpoint removes every Expectation keyed by (method, path) within the
// map indicated by kind, dropping each expectation's hit counter too.
// Calls for unregistered keys are no-ops.
func (s *Store) ResetEndpoint(method, path string, kind Kind) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key{Method: method, Path: path}
	for _, e := range s.bucket(kind)[k] {
		delete(s.hits, e.ID)
	}
	delete(s.bucket(kind), k)
}

// ResetAll clears every registered Expectation and its hit counter.
func (s *Store) ResetAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exact = make(map[key][]Expectation)
	s.template = make(map[key][]Expectation)
	s.hits = make(map[string]*atomic.Int64)
}
