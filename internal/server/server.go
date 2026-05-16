// Package server hosts the HTTP and (later) HTTPS listeners. Multiple servers
// share one http.Handler and run concurrently under an Orchestrator.
package server

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"time"
)

// Server is the minimum surface the Orchestrator needs.
type Server interface {
	ListenAndServe() error
	Shutdown(ctx context.Context) error
	IsExpectedStopErr(err error) bool
}

// Timeouts is the parameter object for the four http.Server timeouts the mock
// exposes. Zero means "use the http stdlib default" (which for WriteTimeout
// and IdleTimeout is effectively unbounded — set them explicitly).
type Timeouts struct {
	ReadHeader time.Duration
	Write      time.Duration
	Idle       time.Duration
}

// stdServer wraps an *http.Server with optional TLS.
type stdServer struct {
	srv *http.Server
	tls bool
}

// NewHTTP returns a plain HTTP Server bound to addr.
func NewHTTP(addr string, handler http.Handler, t Timeouts) Server {
	return &stdServer{srv: &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: t.ReadHeader,
		WriteTimeout:      t.Write,
		IdleTimeout:       t.Idle,
	}}
}

// NewHTTPS returns an HTTPS Server bound to addr using tlsCfg.
func NewHTTPS(addr string, handler http.Handler, tlsCfg *tls.Config, t Timeouts) Server {
	return &stdServer{
		srv: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: t.ReadHeader,
			WriteTimeout:      t.Write,
			IdleTimeout:       t.Idle,
			TLSConfig:         tlsCfg,
		},
		tls: true,
	}
}

func (s *stdServer) ListenAndServe() error {
	if s.tls {
		return s.srv.ListenAndServeTLS("", "")
	}
	return s.srv.ListenAndServe()
}

func (s *stdServer) Shutdown(ctx context.Context) error { return s.srv.Shutdown(ctx) }

func (s *stdServer) IsExpectedStopErr(err error) bool { return errors.Is(err, http.ErrServerClosed) }
