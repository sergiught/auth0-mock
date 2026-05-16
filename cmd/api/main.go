// Command auth0-mock is the entry point for the auth0-mock service.
package main

import (
	"context"
	"fmt"
	stdLog "log"
	"os"
	"os/signal"
	"syscall"

	"github.com/sergiught/auth0-mock/api"
	"github.com/sergiught/auth0-mock/internal/claims"
	"github.com/sergiught/auth0-mock/internal/config"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/logger"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/mfa"
	"github.com/sergiught/auth0-mock/internal/permissions"
	"github.com/sergiught/auth0-mock/internal/pkce"
	"github.com/sergiught/auth0-mock/internal/router"
	"github.com/sergiught/auth0-mock/internal/server"
	"github.com/sergiught/auth0-mock/internal/spec"
	"github.com/sergiught/auth0-mock/internal/tlscert"
)

func main() {
	if err := run(); err != nil {
		stdLog.Printf("fatal: %v", err)
		os.Exit(1)
	}
}

// run wires every dependency, starts the listeners, and blocks until the
// orchestrator returns. Returning an error here lets main() defer cleanups
// before exiting non-zero — using log.Fatal / os.Exit directly would skip
// any pending defers (signal.NotifyContext's stop, in particular).
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	log := logger.New(cfg.Environment)

	keys, err := jwks.NewKeySet(jwks.Config{
		Issuer:         cfg.IssuerURL,
		KeyFile:        cfg.SigningKeyFile,
		AccessTokenTTL: cfg.AccessTokenTTL,
		IDTokenTTL:     cfg.IDTokenTTL,
	})
	if err != nil {
		return fmt.Errorf("jwks init: %w", err)
	}

	openapiSpec, err := spec.Load(api.ManagementOpenAPIJSON)
	if err != nil {
		return fmt.Errorf("openapi load: %w", err)
	}
	validator, err := spec.NewValidator(openapiSpec)
	if err != nil {
		return fmt.Errorf("spec validator: %w", err)
	}

	store := matches.NewStore()
	claimsStore := claims.NewStore()
	permsStore := permissions.NewStore()
	pkceStore := pkce.NewStore()
	mfaStore := mfa.NewStore()
	handler, err := router.New(router.Deps{
		Log:                  log,
		Store:                store,
		Claims:               claimsStore,
		Permissions:          permsStore,
		PKCE:                 pkceStore,
		MFA:                  mfaStore,
		Keys:                 keys,
		Spec:                 openapiSpec,
		Validator:            validator,
		Issuer:               cfg.IssuerURL,
		DefaultAudience:      cfg.DefaultAudience,
		SpecValidationStrict: cfg.SpecValidationStrict,
		MaxRequestBodyBytes:  cfg.MaxRequestBodyBytes,
	})
	if err != nil {
		return fmt.Errorf("router init: %w", err)
	}

	timeouts := server.Timeouts{
		ReadHeader: cfg.ReadHeaderTimeout,
		Write:      cfg.WriteTimeout,
		Idle:       cfg.IdleTimeout,
	}
	servers := []server.Server{}
	if cfg.HTTPAddress != "" {
		servers = append(servers, server.NewHTTP(cfg.HTTPAddress, handler, timeouts))
		log.Info().Str("addr", cfg.HTTPAddress).Msg("http listener")
	}
	if cfg.HTTPSAddress != "" {
		tlsCfg, err := tlscert.Load(cfg.TLS)
		if err != nil {
			return fmt.Errorf("tls init: %w", err)
		}
		servers = append(servers, server.NewHTTPS(cfg.HTTPSAddress, handler, tlsCfg, timeouts))
		log.Info().Str("addr", cfg.HTTPSAddress).Msg("https listener")
	}

	orc := server.NewOrchestrator(servers...).WithShutdownTimeout(cfg.ShutdownTimeout)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := orc.Start(ctx); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	log.Info().Msg("shutdown complete")
	return nil
}
