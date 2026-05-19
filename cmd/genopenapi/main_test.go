package main

import (
	"encoding/json"
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

func TestBundleStripsUpstreamProse(t *testing.T) {
	doc, err := bundle("http://localhost:8080")
	require.NoError(t, err)

	// An upstream Auth0 operation: prose stripped, structure kept.
	usersGet := doc.Paths.Value("/api/v2/users/{id}").GetOperation("GET")
	require.NotNil(t, usersGet)
	assert.Empty(t, usersGet.Description, "upstream operation description must be blanked")
	assert.Empty(t, usersGet.Extensions, "upstream operation x- extensions must be dropped")
	assert.NotEmpty(t, usersGet.Summary, "summaries are kept — short factual labels")

	// Response.description is required by OpenAPI: blanked, not removed.
	resp200 := usersGet.Responses.Map()["200"]
	require.NotNil(t, resp200)
	require.NotNil(t, resp200.Value)
	require.NotNil(t, resp200.Value.Description,
		"Response.description is required — must stay non-nil after stripping")

	// Auth0-mock's own fragment prose, merged after the strip, survives.
	tokenPost := doc.Paths.Value("/oauth/token").GetOperation("POST")
	require.NotNil(t, tokenPost)
	assert.NotEmpty(t, tokenPost.Description,
		"auth0-mock's own fragment descriptions must be preserved")

	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	body := string(raw)
	// Auth0's documentation links and x-description prose are gone.
	assert.NotContains(t, body, "auth0.com/docs",
		"Auth0 documentation links must not survive in the merged spec")
	assert.NotContains(t, body, "x-description-",
		"Auth0's x-description-N prose extensions must be dropped")
	// OAuth grant-type URNs are protocol identifiers, not prose — kept.
	assert.Contains(t, body, "http://auth0.com/oauth/grant-type/",
		"grant-type URNs are protocol identifiers and must survive")
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
	// Every surface sub-tag must be merged in with a description from its
	// fragment so Scalar renders a real section header.
	surfaceTags := []string{
		"OAuth & OIDC", "Database Connections", "Passwordless",
		"Claims", "Permissions", "MFA", "Expectations",
		"Service",
	}
	for _, expected := range surfaceTags {
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

	// Surface groups carry their fragment sub-tags so the group→tag nesting is
	// meaningful, not a redundant single-tag wrapper.
	assert.ElementsMatch(t,
		[]string{"OAuth & OIDC", "Database Connections", "Passwordless"},
		byName["Authentication API"])
	assert.ElementsMatch(t,
		[]string{"Claims", "Permissions", "MFA", "Expectations", "Clock", "Event Producer"},
		byName["admin0"])
	assert.Equal(t, []string{"Service"}, byName["Service"])
	assert.NotEmpty(t, byName["Management API"],
		"Management API group must contain the upstream Auth0 tags")
	for _, surfaceTag := range []string{
		"OAuth & OIDC", "Database Connections", "Passwordless",
		"Claims", "Permissions", "MFA", "Expectations", "Clock", "Event Producer", "Service",
	} {
		assert.NotContainsf(t, byName["Management API"], surfaceTag,
			"surface tag %q leaked into the Management API group", surfaceTag)
	}
	// Upstream Auth0 tags are title-cased ("client-grants" -> "Client Grants")
	// so the sidebar reads consistently with the Title Case fragment tags.
	for _, tag := range byName["Management API"] {
		require.NotEmpty(t, tag)
		assert.NotContainsf(t, tag, "-",
			"Management API tag %q must be title-cased, not kebab-case", tag)
		first := tag[0]
		assert.Truef(t, first >= 'A' && first <= 'Z',
			"Management API tag %q must start with an uppercase letter", tag)
	}

	// Critical x-tagGroups invariant: every tag used by an operation must
	// belong to exactly one group, else Scalar drops it from the sidebar.
	used := map[string]struct{}{}
	for _, item := range doc.Paths.Map() {
		for _, op := range item.Operations() {
			for _, tag := range op.Tags {
				used[tag] = struct{}{}
			}
		}
	}
	grouped := map[string]int{}
	for _, g := range groups {
		for _, tag := range g.Tags {
			grouped[tag]++
		}
	}
	for tag := range used {
		assert.Equalf(t, 1, grouped[tag],
			"tag %q must be in exactly one x-tagGroup (found in %d)", tag, grouped[tag])
	}
	for tag := range grouped {
		_, isUsed := used[tag]
		assert.Truef(t, isUsed, "x-tagGroups references tag %q that no operation uses", tag)
	}
}
