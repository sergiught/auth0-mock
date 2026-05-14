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

	// Spot-check: a known multi-method Mgmt API path. Its /match and /reset
	// siblings must carry one operation per parent method — the running mock
	// registers siblings on the parent operation's own method, not always
	// POST (see internal/mgmtapi/mount.go).
	basePath := "/api/v2/users/{id}"
	parent := doc.Paths.Value(basePath)
	require.NotNil(t, parent,
		"sanity: base path missing — the upstream spec changed?")
	parentOps := parent.Operations()
	require.GreaterOrEqual(t, len(parentOps), 2,
		"sanity: expected a multi-method parent for this assertion")

	for _, suffix := range []string{"/match", "/reset"} {
		item := doc.Paths.Value(basePath + suffix)
		require.NotNilf(t, item, "missing sibling path %s%s", basePath, suffix)
		for method, parentOp := range parentOps {
			op := item.GetOperation(method)
			require.NotNilf(t, op, "sibling %s%s missing %s operation", basePath, suffix, method)
			if len(parentOp.Tags) > 0 {
				assert.Containsf(t, op.Tags, parentOp.Tags[0],
					"%s %s%s must inherit the parent's tag", method, basePath, suffix)
			}
			assert.NotContains(t, op.Tags, "mock-control",
				"siblings must not be tagged mock-control — that creates a separate sidebar bucket")
			assert.Containsf(t, op.OperationID, "mock-control.",
				"%s %s%s must have a synthesised operationId", method, basePath, suffix)
			assert.Containsf(t, op.Summary, method,
				"sibling summary must name the parent method")
			assert.Containsf(t, op.Summary, basePath,
				"sibling summary must name the parent path")
		}
	}

	// Sweep: for every Mgmt API parent operation, the /match and /reset
	// siblings must carry a same-method operation, and every synthesised
	// operationId + summary must be globally unique.
	parents := map[string]bool{}
	for p := range doc.Paths.Map() {
		if !strings.HasPrefix(p, "/api/v2/") ||
			strings.HasSuffix(p, "/match") || strings.HasSuffix(p, "/reset") {
			continue
		}
		parents[p] = true
	}
	seenIDs := map[string]string{}
	seenSummaries := map[string]string{}
	for p := range parents {
		parentItem := doc.Paths.Value(p)
		for _, suffix := range []string{"/match", "/reset"} {
			sib := doc.Paths.Value(p + suffix)
			require.NotNilf(t, sib, "missing sibling path %s%s", p, suffix)
			for method := range parentItem.Operations() {
				op := sib.GetOperation(method)
				require.NotNilf(t, op, "sibling %s%s missing %s operation", p, suffix, method)
				if !strings.HasPrefix(op.OperationID, "mock-control.") {
					// A real spec op occupies this method+path (collision
					// case) — covered by TestBundleSkipsSiblingsThatCollideWithRealOps.
					continue
				}
				where := method + " " + p + suffix
				if prev, dup := seenIDs[op.OperationID]; dup {
					t.Errorf("duplicate operationId %q on %s (also %s)", op.OperationID, where, prev)
				}
				seenIDs[op.OperationID] = where
				if prev, dup := seenSummaries[op.Summary]; dup {
					t.Errorf("duplicate summary %q on %s (also %s)", op.Summary, where, prev)
				}
				seenSummaries[op.Summary] = where
			}
		}
	}
}

func TestBundleSkipsSiblingsThatCollideWithRealOps(t *testing.T) {
	doc, err := bundle("http://localhost:8080")
	require.NoError(t, err)

	// The Auth0 spec has a real PATCH /branding/phone/templates/{id}/reset.
	// Its parent /branding/phone/templates/{id} has GET, PATCH and DELETE — so
	// the synthesiser must add GET and DELETE /reset siblings but must NOT
	// overwrite the real PATCH operation that already occupies that slot.
	colliding := "/api/v2/branding/phone/templates/{id}/reset"
	item := doc.Paths.Value(colliding)
	require.NotNil(t, item)

	// The PATCH op must still be the real one — not a synthesised sibling.
	patchOp := item.GetOperation("PATCH")
	require.NotNil(t, patchOp, "expected real PATCH op to survive")
	assert.NotContains(t, patchOp.OperationID, "mock-control",
		"real PATCH op was clobbered by a synthesised /reset sibling")
	assert.NotContains(t, patchOp.Tags, "mock-control")

	// The non-colliding parent methods still get synthesised reset siblings.
	for _, method := range []string{"GET", "DELETE"} {
		op := item.GetOperation(method)
		require.NotNilf(t, op, "expected a synthesised %s reset sibling", method)
		assert.Containsf(t, op.OperationID, "mock-control.reset.",
			"%s op on %s should be a synthesised reset sibling", method, colliding)
	}
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
