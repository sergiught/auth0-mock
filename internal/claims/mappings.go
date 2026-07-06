package claims

import (
	"maps"
	"slices"
	"sync"
)

// MappingStore holds the active request-parameter → claim-name mappings.
// When a mapping is registered, /oauth/token projects the value of the
// named request-body parameter into the minted access token under the
// mapped claim name. An empty store means the feature is off and tokens
// mint exactly as before. Safe for concurrent use.
//
// Tests use PUT /admin0/claims/mappings to shape the mapping at runtime
// without restarting the service.
type MappingStore struct {
	mu       sync.RWMutex
	mappings map[string]string
}

// NewMappingStore returns an empty MappingStore.
func NewMappingStore() *MappingStore {
	return &MappingStore{mappings: make(map[string]string)}
}

// Set replaces the entire mapping.
func (s *MappingStore) Set(mappings map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make(map[string]string, len(mappings))
	maps.Copy(cp, mappings)
	s.mappings = cp
}

// Get returns a snapshot of the current mapping.
func (s *MappingStore) Get() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make(map[string]string, len(s.mappings))
	maps.Copy(cp, s.mappings)
	return cp
}

// Clear removes all mappings.
func (s *MappingStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mappings = make(map[string]string)
}

// Project copies each mapped request parameter present in params into dst
// under its claim name. Parameters absent from params are skipped, and
// params keys with no registered mapping are ignored — the mapping is an
// allowlist, so arbitrary request keys can't inject claims. Projected
// values overwrite existing keys in dst, giving per-request parameters
// final precedence over the global claims store.
//
// Parameters are applied in lexicographic order so projection is
// deterministic: if two mapped parameters target the same claim and both
// appear in the request, the parameter that sorts last wins. (Random map
// iteration order would make such a mint a coin flip — poison for a
// testing tool.)
func (s *MappingStore) Project(params map[string]any, dst map[string]any) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, param := range slices.Sorted(maps.Keys(s.mappings)) {
		if v, ok := params[param]; ok {
			dst[s.mappings[param]] = v
		}
	}
}
