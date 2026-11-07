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

	mu         sync.Mutex // guards everything below, for the whole of Reset and Poll
	wipe       func(context.Context)
	current    *Seed
	loadedHash [sha256.Size]byte
	badHash    [sha256.Size]byte
	hasBad     bool
	missing    bool
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
	s.Apply(st)
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
		m.current.Apply(m.st)
	}
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
		return
	}
	m.missing = false
	h := sha256.Sum256(data)
	if h == m.loadedHash {
		m.hasBad = false
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
	m.wipe(ctx)
	s.Apply(m.st)
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
