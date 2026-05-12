// Command auth0-mock is the entry point for the auth0-mock service.
package main

import (
	"context"
	stdLog "log"
	"os/signal"
	"syscall"

	"github.com/sergiught/auth0-mock/internal/config"
	"github.com/sergiught/auth0-mock/internal/logger"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/router"
	"github.com/sergiught/auth0-mock/internal/server"
)

func main() {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		stdLog.Fatalf("config: %v", err)
	}

	log := logger.New(cfg.Environment)
	log.Info().Str("http_addr", cfg.HTTPAddress).Msg("starting auth0-mock")

	store := matches.NewStore()
	handler := router.New(log, store)

	httpSrv := server.NewHTTP(cfg.HTTPAddress, handler, cfg.ReadHeaderTimeout)
	orc := server.NewOrchestrator(httpSrv).WithShutdownTimeout(cfg.ShutdownTimeout)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := orc.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("server failure")
	}
	log.Info().Msg("shutdown complete")
}
