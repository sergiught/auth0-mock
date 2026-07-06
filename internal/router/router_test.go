package router_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/api"
	"github.com/sergiught/auth0-mock/internal/claims"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/permissions"
	"github.com/sergiught/auth0-mock/internal/router"
	"github.com/sergiught/auth0-mock/internal/spec"
)

// TestNew_DefaultsClaimMappings pins the off-by-default safety net: a
// Deps that never wires a ClaimMappings store must still serve the
// /admin0/claims/mappings routes (empty map = feature off) instead of
// mounting them over a nil store that panics on first request.
func TestNew_DefaultsClaimMappings(t *testing.T) {
	t.Parallel()
	openapiSpec, err := spec.Load(api.ManagementOpenAPIJSON)
	require.NoError(t, err)
	validator, err := spec.NewValidator(openapiSpec)
	require.NoError(t, err)
	ks, err := jwks.NewKeySet(jwks.Config{
		Issuer: "https://mock/", AccessTokenTTL: time.Hour, IDTokenTTL: time.Hour,
	})
	require.NoError(t, err)

	h, err := router.New(router.Deps{
		Log:             zerolog.Nop(),
		Store:           matches.NewStore(),
		Claims:          claims.NewStore(),
		Permissions:     permissions.NewStore(),
		Keys:            ks,
		Spec:            openapiSpec,
		Validator:       validator,
		Issuer:          "https://mock/",
		DefaultAudience: "https://mock/api/v2/",
		// ClaimMappings deliberately omitted.
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin0/claims/mappings", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{}`, w.Body.String())
}
