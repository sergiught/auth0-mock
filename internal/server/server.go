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

// stdServer wraps an *http.Server with optional TLS.
type stdServer struct {
	srv *http.Server
	tls bool
}

// NewHTTP returns a plain HTTP Server bound to addr.
func NewHTTP(addr string, handler http.Handler, readHeaderTimeout time.Duration) Server {
	return &stdServer{srv: &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
	}}
}

// NewHTTPS returns an HTTPS Server bound to addr using tlsCfg.
func NewHTTPS(addr string, handler http.Handler, tlsCfg *tls.Config, readHeaderTimeout time.Duration) Server {
	return &stdServer{
		srv: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: readHeaderTimeout,
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
