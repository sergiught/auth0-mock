package server

import (
	"context"
	"errors"
	"time"

	"golang.org/x/sync/errgroup"
)

// Orchestrator runs multiple Server instances concurrently and shuts them
// down together on context cancellation or first non-expected error.
type Orchestrator struct {
	servers         []Server
	shutdownTimeout time.Duration
}

// NewOrchestrator constructs an Orchestrator over non-nil servers.
func NewOrchestrator(servers ...Server) *Orchestrator {
	filtered := make([]Server, 0, len(servers))
	for _, s := range servers {
		if s != nil {
			filtered = append(filtered, s)
		}
	}
	return &Orchestrator{servers: filtered, shutdownTimeout: 5 * time.Second}
}

// WithShutdownTimeout overrides the default shutdown grace period.
func (o *Orchestrator) WithShutdownTimeout(d time.Duration) *Orchestrator {
	o.shutdownTimeout = d
	return o
}

// Start blocks until ctx is cancelled or any server fails.
func (o *Orchestrator) Start(ctx context.Context) (err error) {
	defer func() { err = errors.Join(err, o.stop()) }()

	g, ctx := errgroup.WithContext(ctx)
	for _, srv := range o.servers {
		srv := srv
		g.Go(func() error {
			errCh := make(chan error, 1)
			go func() {
				if e := srv.ListenAndServe(); e != nil && !srv.IsExpectedStopErr(e) {
					errCh <- e
				}
				close(errCh)
			}()
			select {
			case <-ctx.Done():
				return nil
			case e := <-errCh:
				return e
			}
		})
	}
	return g.Wait()
}

func (o *Orchestrator) stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), o.shutdownTimeout)
	defer cancel()
	var err error
	for _, srv := range o.servers {
		err = errors.Join(err, srv.Shutdown(ctx))
	}
	return err
}
