// Command auth0-mock is the entry point for the auth0-mock service.
package main

import (
	"context"
	"flag"
	"fmt"
	stdLog "log"
	"os"
	"os/signal"
	"syscall"

	"github.com/sergiught/auth0-mock/api"
	"github.com/sergiught/auth0-mock/internal/claims"
	"github.com/sergiught/auth0-mock/internal/clock"
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
	"github.com/sergiught/auth0-mock/internal/version"
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
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version.String())
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	log := logger.New(cfg.LogLevel)
	log.Info().
		Str("version", version.Version).
		Str("commit", version.Commit).
		Str("date", version.Date).
		Msg("auth0-mock starting")

	// Controllable clock — drives JWT iat/exp, PKCE code TTLs, MFA token
	// TTLs, and bearer exp/nbf validation. Mounted at /admin0/clock so
	// tests can freeze and advance time at runtime.
	clk := clock.NewControlled()

	keys, err := jwks.NewKeySet(jwks.Config{
		Issuer:         cfg.IssuerURL,
		KeyFile:        cfg.SigningKeyFile,
		AccessTokenTTL: cfg.AccessTokenTTL,
		IDTokenTTL:     cfg.IDTokenTTL,
		Now:            clk.Now,
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
	pkceStore := pkce.NewStore(pkce.WithNow(clk.Now))
	mfaStore := mfa.NewStore(mfa.WithNow(clk.Now))
	handler, err := router.New(router.Deps{
		Log:                          log,
		Store:                        store,
		Claims:                       claimsStore,
		Permissions:                  permsStore,
		PKCE:                         pkceStore,
		MFA:                          mfaStore,
		Keys:                         keys,
		Spec:                         openapiSpec,
		Validator:                    validator,
		Clock:                        clk,
		Issuer:                       cfg.IssuerURL,
		DefaultAudience:              cfg.DefaultAudience,
		SpecValidationStrict:         cfg.SpecValidationStrict,
		MaxRequestBodyBytes:          cfg.MaxRequestBodyBytes,
		LogoutAllowedURLs:            cfg.LogoutAllowedURLs,
		AuthorizeAllowedRedirectURIs: cfg.AuthorizeAllowedCallbacks,
		BearerRequireAudience:        cfg.BearerRequireAudience,
		Debug:                        cfg.Debug,
	})
	if err != nil {
		return fmt.Errorf("router init: %w", err)
	}

	timeouts := server.Timeouts{
		ReadHeader: cfg.ReadHeaderTimeout,
		Write:      cfg.WriteTimeout,
		Idle:       cfg.IdleTimeout,
	}
	// "off" is the documented disable sentinel for either listener. The
	// empty string can't be used as a disable signal because caarlos0/env
	// treats an unset-or-empty env var as "use envDefault", which still
	// yields a bind address — so the user-facing knob has to be a real
	// non-empty value.
	servers := []server.Server{}
	if cfg.HTTPAddress != "" && cfg.HTTPAddress != "off" {
		servers = append(servers, server.NewHTTP(cfg.HTTPAddress, handler, timeouts))
		log.Info().Str("addr", cfg.HTTPAddress).Msg("http listener")
	}
	if cfg.HTTPSAddress != "" && cfg.HTTPSAddress != "off" {
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
