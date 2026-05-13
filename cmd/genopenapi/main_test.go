package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBundleMergesEveryFragment(t *testing.T) {
	doc, err := bundle("http://localhost:8080")
	require.NoError(t, err)

	// Mgmt API base path survives and paths are prefixed with /api/v2 so that
	// the merged doc matches the routes the mock actually serves.
	assert.NotNil(t, doc.Paths.Value("/api/v2/users/{id}"),
		"base /api/v2/users/{id} should be present after prefixing")

	// Auth API fragment is merged.
	require.NotNil(t, doc.Paths.Value("/oauth/token"))
	assert.NotNil(t, doc.Paths.Value("/oauth/token").GetOperation("POST"))

	// admin0 fragment is merged.
	require.NotNil(t, doc.Paths.Value("/admin0/reset"))
	assert.NotNil(t, doc.Paths.Value("/admin0/reset").GetOperation("POST"))

	// Service fragment is merged.
	require.NotNil(t, doc.Paths.Value("/healthz"))
	require.NotNil(t, doc.Paths.Value("/openapi.json"))
}

func TestBundleDetectsConflicts(t *testing.T) {
	// Pass a fragment that re-declares POST /oauth/token to force a conflict.
	conflicting := []byte(`openapi: 3.1.0
info: { title: bad, version: "1.0" }
paths:
  /oauth/token:
    post:
      summary: duplicate
      responses: { "200": { description: ok } }
`)
	_, err := bundleWithExtras("http://localhost:8080", [][]byte{conflicting})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflict")
	assert.Contains(t, err.Error(), "/oauth/token")
}
