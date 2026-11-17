package seed

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/sergiught/auth0-mock/internal/spec"
)

// Manager owns the seed file: it applies it at startup, re-applies it on
// reset and reloads it when its content changes.
type Manager struct {
	path string
	v    *spec.Validator
	st   Stores
	log  zerolog.Logger

	mu         sync.Mutex // Guards everything below, for the whole of Reset and Poll.
	wipe       func(context.Context)
	current    *Seed
	loadedHash [sha256.Size]byte
	badHash    [sha256.Size]byte
	hasBad     bool
	// A changed content seen once is acted on only when the next poll sees
	// it again, so a half-written file is never loaded.
	pendingHash [sha256.Size]byte
	hasPending  bool
	missing     bool
}

// NewManager reads and parses the seed file at path and applies it to st.
func NewManager(path string, v *spec.Validator, st Stores, log zerolog.Logger) (*Manager, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the operator chooses the seed path
	if err != nil {
		return nil, fmt.Errorf("seed file %s: %w", path, err)
	}
	s, err := Parse(data, v)
	if err != nil {
		return nil, fmt.Errorf("seed file %s: %w", path, err)
	}
	if err := checkManagement(s, st, log); err != nil {
		return nil, fmt.Errorf("seed file %s: %w", path, err)
	}
	if err := s.ApplyChecked(st); err != nil {
		return nil, fmt.Errorf("seed file %s: %w", path, err)
	}
	log.Info().Str("file", path).Msgf("seed loaded: %s", s.Summary())
	return &Manager{
		path:       path,
		v:          v,
		st:         st,
		log:        log,
		current:    s,
		loadedHash: sha256.Sum256(data),
	}, nil
}

// checkManagement dry-runs the management section against st before anything
// is wiped or applied. With the store off it warns and ignores the section.
func checkManagement(s *Seed, st Stores, log zerolog.Logger) error {
	if len(s.Management) == 0 {
		return nil
	}
	if st.Mgmt == nil {
		log.Warn().Msg("seed file has management data but MGMT_STORE is off; ignoring it")
		return nil
	}
	return st.Mgmt.CheckSeed(s.Management)
}

// Bind sets the function that clears every store. It must be called before
// Reset, Poll or Watch.
func (m *Manager) Bind(wipe func(context.Context)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wipe = wipe
}

// Reset clears every store and, if keepSeed, re-applies the current seed.
func (m *Manager) Reset(ctx context.Context, keepSeed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wipe(ctx)
	if keepSeed {
		m.warnApply(m.current.ApplyChecked(m.st))
	}
}

// ApplyTo applies the current seed to st without wiping it or touching the
// manager's own stores. A manager with no seed does nothing.
func (m *Manager) ApplyTo(st Stores) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil {
		return
	}
	m.warnApply(m.current.ApplyChecked(st))
}

// warnApply logs an Apply error: Reset and ApplyTo have no error path.
func (m *Manager) warnApply(err error) {
	if err != nil {
		m.log.Warn().Msgf("seed file %s: %v", m.path, err)
	}
}

// Export snapshots the stores in seed file shape while holding the lock Reset
// and Poll hold, so it never sees a half-applied reload.
func (m *Manager) Export(detail bool) File {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Export(m.st, detail)
}

// Poll checks the seed file once and reloads it if its content changed.
func (m *Manager) Poll(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, err := os.ReadFile(m.path)
	if err != nil {
		if !m.missing {
			m.log.Warn().Msgf("seed file %s unreadable, keeping the current seed: %v", m.path, err)
			m.missing = true
		}
		m.hasPending = false
		return
	}
	m.missing = false
	h := sha256.Sum256(data)
	if h == m.loadedHash {
		m.hasBad = false
		m.hasPending = false
		return
	}
	if !m.hasPending || h != m.pendingHash {
		m.pendingHash, m.hasPending = h, true
		return
	}
	if m.hasBad && h == m.badHash {
		return
	}
	s, err := Parse(data, m.v)
	if err != nil {
		m.log.Error().Msgf("seed file %s: %v", m.path, err)
		m.badHash, m.hasBad = h, true
		return
	}
	if err := checkManagement(s, m.st, m.log); err != nil {
		m.log.Error().Msgf("seed file %s: %v", m.path, err)
		m.badHash, m.hasBad = h, true
		return
	}
	m.wipe(ctx)
	m.warnApply(s.ApplyChecked(m.st))
	m.current = s
	m.loadedHash = h
	m.hasBad = false
	m.log.Info().Str("file", m.path).Msgf("seed loaded: %s", s.Summary())
}

// Watch polls the seed file every interval until ctx is done. Start it only
// after Bind.
func (m *Manager) Watch(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.Poll(ctx)
		}
	}
}
