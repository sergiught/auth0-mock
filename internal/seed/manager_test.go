package seed_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/seed"
)

func stub(status string) string {
	return "expectations:\n  - method: GET\n    path: /api/v2/users\n    response: {status: " + status + "}\n"
}

type fixture struct {
	t     *testing.T
	path  string
	st    allStores
	log   *syncBuf
	wipes atomic.Int32
	m     *seed.Manager
}

// syncBuf is a goroutine-safe log sink.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func (f *fixture) write(content string) {
	f.t.Helper()
	tmp := f.path + ".tmp"
	require.NoError(f.t, os.WriteFile(tmp, []byte(content), 0o600))
	require.NoError(f.t, os.Rename(tmp, f.path))
}

func newFixture(t *testing.T, content string) *fixture {
	t.Helper()
	f := &fixture{t: t, path: filepath.Join(t.TempDir(), "seed.yaml"), st: newStores(), log: &syncBuf{}}
	f.write(content)
	m, err := seed.NewManager(f.path, newValidator(t), f.st.Stores, zerolog.New(f.log))
	require.NoError(t, err)
	m.Bind(func(context.Context) {
		f.wipes.Add(1)
		f.st.reset()
	})
	f.m = m
	return f
}

func (f *fixture) statuses() []int {
	var out []int
	for _, e := range f.st.Matches.List() {
		out = append(out, e.Response.Status)
	}
	return out
}

func TestNewManagerAppliesAndLogs(t *testing.T) {
	f := newFixture(t, stub("429"))
	require.Equal(t, []int{429}, f.statuses())
	require.Contains(t, f.log.String(), "seed loaded: 1 expectation")
}

func TestNewManagerMissingFile(t *testing.T) {
	_, err := seed.NewManager(filepath.Join(t.TempDir(), "nope.yaml"), newValidator(t), newStores().Stores, zerolog.Nop())
	require.Error(t, err)
	require.True(t, strings.HasPrefix(err.Error(), "seed file "), err.Error())
	require.Contains(t, err.Error(), "no such file")
}

func TestNewManagerBadFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "seed.yaml")
	require.NoError(t, os.WriteFile(p, []byte("bogus: 1\n"), 0o600))
	_, err := seed.NewManager(p, newValidator(t), newStores().Stores, zerolog.Nop())
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown key")
}

func (f *fixture) putRuntime() {
	f.st.Matches.Put(matches.Expectation{Method: "GET", Path: "/api/v2/clients", Response: matches.ResponseDef{Status: 200}})
}

func TestResetKeepSeed(t *testing.T) {
	f := newFixture(t, stub("429"))
	f.putRuntime()
	require.Len(t, f.st.Matches.List(), 2)
	f.m.Reset(context.Background(), true)
	list := f.st.Matches.List()
	require.Len(t, list, 1)
	require.Equal(t, 429, list[0].Response.Status)
	require.Zero(t, list[0].Hits)
}

func TestResetWipe(t *testing.T) {
	f := newFixture(t, stub("429"))
	f.m.Reset(context.Background(), false)
	require.Empty(t, f.st.Matches.List())
	f.m.Poll(context.Background())
	require.Empty(t, f.st.Matches.List())
}

func TestPollReloadsOnChange(t *testing.T) {
	f := newFixture(t, stub("429"))
	f.write(stub("503"))
	f.m.Poll(context.Background())
	require.Equal(t, []int{429}, f.statuses())
	f.m.Poll(context.Background())
	require.Equal(t, []int{503}, f.statuses())
	require.Equal(t, 2, strings.Count(f.log.String(), "seed loaded"))
}

func TestPollNoopSave(t *testing.T) {
	f := newFixture(t, stub("429"))
	f.write(stub("429"))
	f.m.Poll(context.Background())
	require.Zero(t, f.wipes.Load())
}

func TestPollBrokenEditKeepsState(t *testing.T) {
	f := newFixture(t, stub("429"))
	f.write("bogus: 1\n")
	f.m.Poll(context.Background())
	f.m.Poll(context.Background())
	f.m.Poll(context.Background())
	require.Equal(t, []int{429}, f.statuses())
	n := 0
	for _, l := range strings.Split(f.log.String(), "\n") {
		if strings.Contains(l, `"level":"error"`) && strings.Contains(l, "seed file ") {
			n++
		}
	}
	require.Equal(t, 1, n, f.log.String())
	f.write(stub("503"))
	f.m.Poll(context.Background())
	f.m.Poll(context.Background())
	require.Equal(t, []int{503}, f.statuses())
}

func TestPollTruncatedFileIsIgnored(t *testing.T) {
	f := newFixture(t, stub("429"))
	require.NoError(t, os.WriteFile(f.path, nil, 0o600))
	f.m.Poll(context.Background())
	require.Equal(t, []int{429}, f.statuses())
	require.NoError(t, os.WriteFile(f.path, []byte(stub("429")), 0o600))
	f.m.Poll(context.Background())
	f.m.Poll(context.Background())
	require.Equal(t, []int{429}, f.statuses())
	require.Zero(t, f.wipes.Load())
}

func TestPollRenameSave(t *testing.T) {
	f := newFixture(t, stub("429"))
	tmp := f.path + ".tmp"
	require.NoError(t, os.WriteFile(tmp, []byte(stub("503")), 0o600))
	require.NoError(t, os.Rename(tmp, f.path))
	f.m.Poll(context.Background())
	f.m.Poll(context.Background())
	require.Equal(t, []int{503}, f.statuses())
}

func TestPollMissingWarnsOnce(t *testing.T) {
	f := newFixture(t, stub("429"))
	warns := func() int { return strings.Count(f.log.String(), `"level":"warn"`) }
	require.NoError(t, os.Remove(f.path))
	for range 3 {
		f.m.Poll(context.Background())
	}
	require.Equal(t, 1, warns())
	require.Equal(t, []int{429}, f.statuses())
	f.write(stub("429"))
	f.m.Poll(context.Background())
	require.Zero(t, f.wipes.Load())
	require.NoError(t, os.Remove(f.path))
	f.m.Poll(context.Background())
	require.Equal(t, 2, warns())
}

func TestResetAndPollDoNotInterleave(t *testing.T) {
	f := newFixture(t, stub("429"))
	ctx := context.Background()
	var wg sync.WaitGroup
	var mu sync.Mutex // Serialises the test's own atomic rewrites.
	for i := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				f.m.Reset(ctx, true)
				return
			}
			mu.Lock()
			if i%4 == 1 {
				f.write(stub("503"))
			} else {
				f.write(stub("429"))
			}
			mu.Unlock()
			f.m.Poll(ctx)
		}()
	}
	wg.Wait()
	require.Len(t, f.st.Matches.List(), 1)
}

func TestWatchReloads(t *testing.T) {
	f := newFixture(t, stub("429"))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		f.m.Watch(ctx, 10*time.Millisecond)
		close(done)
	}()
	f.write(stub("503"))
	require.Eventually(t, func() bool {
		s := f.statuses()
		return len(s) == 1 && s[0] == 503
	}, 2*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Watch did not return")
	}
}

func TestExportWaitsForReset(t *testing.T) {
	f := newFixture(t, stub("429"))
	inWipe, release := make(chan struct{}), make(chan struct{})
	f.m.Bind(func(context.Context) {
		f.st.reset()
		close(inWipe)
		<-release
	})
	resetDone := make(chan struct{})
	go func() {
		defer close(resetDone)
		f.m.Reset(context.Background(), true)
	}()
	<-inWipe

	exported := make(chan seed.File, 1)
	go func() { exported <- f.m.Export(false) }()
	select {
	case <-exported:
		t.Fatal("Export returned while a reset was between wipe and apply")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	<-resetDone
	got := <-exported
	require.Len(t, got.Expectations, 1)
	require.Equal(t, 429, got.Expectations[0].Response.Status)
}
