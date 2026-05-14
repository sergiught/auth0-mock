package main

import (
	"encoding/json"
	"strings"
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

	// The admin0 fragment is merged.
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

func TestBundleSynthesisesMatchAndResetSiblings(t *testing.T) {
	doc, err := bundle("http://localhost:8080")
	require.NoError(t, err)

	// Spot-check: a known Mgmt API path has both siblings.
	basePath := "/api/v2/users/{id}"
	parent := doc.Paths.Value(basePath)
	require.NotNil(t, parent,
		"sanity: base path missing — the upstream spec changed?")
	parentOp := parent.GetOperation("GET")
	require.NotNil(t, parentOp, "parent /api/v2/users/{id} GET missing")
	require.NotEmpty(t, parentOp.Tags,
		"sanity: parent must declare at least one tag")
	for _, suffix := range []string{"/match", "/reset"} {
		item := doc.Paths.Value(basePath + suffix)
		require.NotNilf(t, item, "missing sibling %s%s", basePath, suffix)
		op := item.GetOperation("POST")
		require.NotNilf(t, op, "missing POST sibling at %s%s", basePath, suffix)
		assert.Contains(t, op.Tags, parentOp.Tags[0],
			"sibling must inherit the parent's tag so it groups under the same section")
		assert.NotContains(t, op.Tags, "mock-control",
			"siblings must not be tagged mock-control — that creates a separate sidebar bucket")
	}

	// Sweep: every Mgmt API operation must have a POST /match sibling and a
	// POST /reset sibling unless a real operation already occupies that slot.
	// Snapshot first because we'll be reading paths the synthesiser added.
	mgmtPaths := []string{}
	for p := range doc.Paths.Map() {
		if len(p) >= len("/api/v2/") && p[:len("/api/v2/")] == "/api/v2/" {
			mgmtPaths = append(mgmtPaths, p)
		}
	}
	for _, p := range mgmtPaths {
		// Skip paths that are themselves /match or /reset.
		if strings.HasSuffix(p, "/match") || strings.HasSuffix(p, "/reset") {
			continue
		}
		for _, suffix := range []string{"/match", "/reset"} {
			sib := doc.Paths.Value(p + suffix)
			require.NotNilf(t, sib, "missing sibling %s%s", p, suffix)
		}
	}
}

func TestBundleSkipsSiblingsThatCollideWithRealOps(t *testing.T) {
	doc, err := bundle("http://localhost:8080")
	require.NoError(t, err)

	// The Auth0 spec has PATCH /branding/phone/templates/{id}/reset as a real
	// endpoint. The synthesiser must not add a mock-control POST on top of it.
	colliding := "/api/v2/branding/phone/templates/{id}/reset"
	item := doc.Paths.Value(colliding)
	require.NotNil(t, item)

	// The real PATCH must not be tagged mock-control.
	patchOp := item.GetOperation("PATCH")
	require.NotNil(t, patchOp, "expected real PATCH op to survive")
	assert.NotContains(t, patchOp.Tags, "mock-control",
		"real spec op was clobbered by synthesised /reset sibling")

	// No synthetic POST must have been inserted on top of the real path item.
	postOp := item.GetOperation("POST")
	assert.Nil(t, postOp,
		"synthesiser must not inject a POST at a path that already exists in the real spec")
}

func TestBundleRewritesInfoForAuth0Mock(t *testing.T) {
	doc, err := bundle("http://localhost:8080")
	require.NoError(t, err)
	require.NotNil(t, doc.Info)

	// Title and description must be auth0-mock's, not Auth0's.
	assert.Equal(t, "auth0-mock API", doc.Info.Title)
	assert.Contains(t, doc.Info.Description, "auth0-mock")
	assert.NotContains(t, doc.Info.Description, "Auth0 Management API v2.",
		"info.description must not be the upstream Mgmt API blurb")

	// Auth0 Support contact + Auth0 ToS must be gone.
	require.NotNil(t, doc.Info.Contact)
	assert.Equal(t, "auth0-mock", doc.Info.Contact.Name)
	assert.NotEqual(t, "Auth0 Support", doc.Info.Contact.Name)
	assert.Empty(t, doc.Info.TermsOfService,
		"termsOfService must not be carried over from the upstream Auth0 spec")

	// License must be set so Scalar can render the project licence.
	require.NotNil(t, doc.Info.License)
	assert.Equal(t, "MIT", doc.Info.License.Name)

	// ExternalDocs must point at the project, not Auth0's Mgmt API docs.
	require.NotNil(t, doc.ExternalDocs)
	assert.NotContains(t, doc.ExternalDocs.URL, "auth0.com",
		"externalDocs.url must not be Auth0's; it shipped from the upstream spec")
	assert.Contains(t, doc.ExternalDocs.URL, "github.com/sergiught/auth0-mock")
}

func TestBundleMergesFragmentTagsIntoBase(t *testing.T) {
	doc, err := bundle("http://localhost:8080")
	require.NoError(t, err)
	names := map[string]string{}
	for _, t := range doc.Tags {
		if t == nil {
			continue
		}
		names[t.Name] = t.Description
	}
	for _, expected := range []string{"auth-api", "admin0", "service"} {
		desc, ok := names[expected]
		require.Truef(t, ok, "merged base.Tags must include fragment tag %q", expected)
		assert.NotEmptyf(t, desc, "tag %q must carry a description from its fragment", expected)
	}
}

func TestBundleAppliesTagGroupsForSidebar(t *testing.T) {
	doc, err := bundle("http://localhost:8080")
	require.NoError(t, err)
	require.NotNil(t, doc.Extensions)
	raw, ok := doc.Extensions["x-tagGroups"]
	require.True(t, ok, "merged doc must carry x-tagGroups for Scalar sidebar grouping")

	// Marshal/unmarshal so we get back a plain Go shape we can inspect regardless
	// of the source slice's typed elements.
	body, err := json.Marshal(raw)
	require.NoError(t, err)
	var groups []struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}
	require.NoError(t, json.Unmarshal(body, &groups))

	byName := map[string][]string{}
	for _, g := range groups {
		byName[g.Name] = g.Tags
	}
	require.Contains(t, byName, "Authentication API")
	require.Contains(t, byName, "Management API")
	require.Contains(t, byName, "admin0")
	require.Contains(t, byName, "Service")
	assert.NotContains(t, byName, "Mock Control",
		"there must be no separate Mock Control bucket — siblings inherit the parent's group")

	assert.Equal(t, []string{"auth-api"}, byName["Authentication API"])
	assert.NotEmpty(t, byName["Management API"],
		"Management API group must contain the upstream Auth0 tags")
	assert.NotContains(t, byName["Management API"], "auth-api")
	assert.NotContains(t, byName["Management API"], "admin0")
	assert.NotContains(t, byName["Management API"], "service")
}
