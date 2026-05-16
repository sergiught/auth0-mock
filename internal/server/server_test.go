package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrchestrator_StartAndShutdown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(204)
	})

	srv := NewHTTP(addr, handler, Timeouts{ReadHeader: time.Second, Write: 5 * time.Second, Idle: 30 * time.Second})
	orc := NewOrchestrator(srv)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- orc.Start(ctx) }()

	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + addr + "/")
		if err != nil {
			return false
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return resp.StatusCode == 204
	}, 2*time.Second, 50*time.Millisecond)

	cancel()
	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		require.Fail(t, "orchestrator did not stop within deadline")
	}
}
