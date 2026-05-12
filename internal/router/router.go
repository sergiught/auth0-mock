// Package router builds the http.Handler that fronts the mock service.
// Each milestone extends Mount with another subsystem.
package router

import (
	"net/http"

	"github.com/julienschmidt/httprouter"
	"github.com/rs/zerolog"

	"github.com/sergiught/auth0-mock/internal/admin0"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/middleware"
)

// New constructs the http.Handler with admin0 endpoints wired in. Later
// milestones add Auth API and Mgmt API mounts.
func New(log zerolog.Logger, store *matches.Store) http.Handler {
	r := httprouter.New()
	admin0.Mount(r, store)

	chain := middleware.RequestID
	withRecovery := middleware.Recovery(log)
	withLogging := middleware.Logging(log)
	return chain(withRecovery(withLogging(r)))
}
