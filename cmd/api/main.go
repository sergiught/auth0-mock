// Command auth0-mock is the entry point for the auth0-mock service.
package main

import (
	"context"
	stdLog "log"
	"os/signal"
	"syscall"

	"github.com/sergiught/auth0-mock/api"
	"github.com/sergiught/auth0-mock/internal/config"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/logger"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/router"
	"github.com/sergiught/auth0-mock/internal/server"
	"github.com/sergiught/auth0-mock/internal/spec"
	"github.com/sergiught/auth0-mock/internal/tlscert"
)

func main() {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		stdLog.Fatalf("config: %v", err)
	}
	log := logger.New(cfg.Environment)

	keys, err := jwks.NewKeySet(jwks.Config{
		Issuer:         cfg.IssuerURL,
		KeyFile:        cfg.SigningKeyFile,
		AccessTokenTTL: cfg.AccessTokenTTL,
		IDTokenTTL:     cfg.IDTokenTTL,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("jwks init")
	}

	openapiSpec, err := spec.Load(api.ManagementOpenAPIJSON)
	if err != nil {
		log.Fatal().Err(err).Msg("openapi load")
	}
	validator := spec.NewValidator(openapiSpec)

	store := matches.NewStore()
	handler, err := router.New(router.Deps{
		Log:                  log,
		Store:                store,
		Keys:                 keys,
		Spec:                 openapiSpec,
		Validator:            validator,
		Issuer:               cfg.IssuerURL,
		DefaultAudience:      cfg.DefaultAudience,
		SpecValidationStrict: cfg.SpecValidationStrict,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("router init")
	}

	servers := []server.Server{}
	if cfg.HTTPAddress != "" {
		servers = append(servers, server.NewHTTP(cfg.HTTPAddress, handler, cfg.ReadHeaderTimeout))
		log.Info().Str("addr", cfg.HTTPAddress).Msg("http listener")
	}
	if cfg.HTTPSAddress != "" {
		tlsCfg, err := tlscert.Load(tlscert.Config{
			CertFile:  cfg.TLSCertFile,
			KeyFile:   cfg.TLSKeyFile,
			CacheDir:  cfg.TLSCacheDir,
			Hostnames: cfg.TLSHostnames,
		})
		if err != nil {
			log.Fatal().Err(err).Msg("tls init")
		}
		servers = append(servers, server.NewHTTPS(cfg.HTTPSAddress, handler, tlsCfg, cfg.ReadHeaderTimeout))
		log.Info().Str("addr", cfg.HTTPSAddress).Msg("https listener")
	}

	orc := server.NewOrchestrator(servers...).WithShutdownTimeout(cfg.ShutdownTimeout)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := orc.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("server failure")
	}
	log.Info().Msg("shutdown complete")
}
