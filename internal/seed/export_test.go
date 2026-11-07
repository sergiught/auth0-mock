package seed_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/internal/matches"

	"github.com/sergiught/auth0-mock/internal/seed"
)

const exportExtra = `
expectations:
  - method: GET
    path: /api/v2/users
    response: {status: 429}
  - method: GET
    path: /api/v2/users
    request:
      query: {q: a}
    response: {status: 429}
  - method: GET
    path: /api/v2/users/{id}
    response:
      status: 200
      body: {user_id: "auth0|1", email: a@b.co, user_metadata: {created: 2024-01-01, note: "<b>&</b>"}}
    then:
      - status: 404
      - status: 200
        body: {user_id: "auth0|2"}
  - method: POST
    path: /api/v2/users
    request:
      headers: {X-Test: "1"}
      body: {email: a@b.co, connection: db}
    response: {status: 429}
claims:
  tier: gold
  plan: pro
claim_mappings:
  org: org_id
permissions:
  "https://api/": [read:x]
mfa_required: true
`

func applied(t *testing.T, src string) allStores {
	t.Helper()
	st := newStores()
	parse(t, src).Apply(st.Stores)
	return st
}

func mustBytes(t *testing.T) func([]byte, error) []byte {
	return func(b []byte, err error) []byte {
		t.Helper()
		require.NoError(t, err)
		return b
	}
}

func TestExportSections(t *testing.T) {
	st := applied(t, full)
	js := mustBytes(t)(seed.Export(st.Stores, false).JSON())
	var m map[string]any
	require.NoError(t, json.Unmarshal(js, &m))
	for _, k := range []string{"expectations", "claims", "claim_mappings", "permissions", "mfa_required"} {
		require.Contains(t, m, k)
	}
	require.Len(t, m["expectations"], 4)
	require.NotContains(t, string(js), `"id"`)
	require.NotContains(t, string(js), `"hits"`)
}

func TestExportOmitsEmpty(t *testing.T) {
	js := mustBytes(t)(seed.Export(newStores().Stores, false).JSON())
	require.Equal(t, "{}\n", string(js))
}

func TestExportDetail(t *testing.T) {
	st := applied(t, full)
	st.Matches.Find("GET", "/api/v2/users/auth0|1", "/api/v2/users/{id}", matches.MatchableRequest{})
	js := mustBytes(t)(seed.Export(st.Stores, true).JSON())
	var f struct {
		Expectations []struct {
			ID   string `json:"id"`
			Hits *int64 `json:"hits"`
			Path string `json:"path"`
		} `json:"expectations"`
	}
	require.NoError(t, json.Unmarshal(js, &f))
	var hit int
	for _, e := range f.Expectations {
		require.NotEmpty(t, e.ID)
		require.NotNil(t, e.Hits)
		if e.Path == "/api/v2/users/{id}" {
			require.EqualValues(t, 1, *e.Hits)
			hit++
		}
	}
	require.Equal(t, 1, hit)
	_, err := seed.Parse(js, newValidator(t))
	require.NoError(t, err)
}

func roundTrip(t *testing.T, enc func(seed.File) ([]byte, error)) []byte {
	t.Helper()
	first := mustBytes(t)(enc(seed.Export(applied(t, exportExtra).Stores, false)))
	st := newStores()
	parse(t, string(first)).Apply(st.Stores)
	second := mustBytes(t)(enc(seed.Export(st.Stores, false)))
	require.Equal(t, string(first), string(second))
	return first
}

func TestExportRoundTripJSON(t *testing.T) {
	out := roundTrip(t, seed.File.JSON)
	require.Contains(t, string(out), `"created": "2024-01-01"`)
	require.Contains(t, string(out), `<b>&</b>`)
}

func TestExportRoundTripYAML(t *testing.T) {
	out := roundTrip(t, seed.File.YAML)
	require.Contains(t, string(out), "expectations:")
	s := parse(t, string(out))
	var found bool
	for _, e := range s.Expectations {
		if strings.Contains(string(e.Response.Body), "created") {
			require.Contains(t, string(e.Response.Body), `"created":"2024-01-01"`)
			found = true
		}
	}
	require.True(t, found)
}

func TestExportLargeIntExact(t *testing.T) {
	st := applied(t, `
expectations:
  - method: GET
    path: /api/v2/users/{id}
    response: {status: 200, body: {n: 9007199254740993}}
`)
	f := seed.Export(st.Stores, false)
	js := mustBytes(t)(f.JSON())
	require.Contains(t, string(js), "9007199254740993")
	ym := mustBytes(t)(f.YAML())
	require.Contains(t, string(ym), "9007199254740993")
	require.Contains(t, string(ym), "n: 9007199254740993\n")
	require.Contains(t, string(ym), "status: 200\n")
}

func TestExportYAMLQuotesTimestampLikeStrings(t *testing.T) {
	out := roundTrip(t, seed.File.YAML)
	require.Contains(t, string(out), `created: "2024-01-01"`)
}

func TestExportYAMLRoundTripStringLookalikes(t *testing.T) {
	cases := []struct {
		name string
		val  any
	}{
		{"int", "200"},
		{"bool", "true"},
		{"null", "null"},
		{"hex", "0x1F"},
		{"exp", "1e3"},
		{"tilde", "~"},
		{"yes", "yes"},
		{"empty", ""},
		{"multiline", "a\nb\n"},
		{"leading space", " x"},
		{"hash", "#x"},
	}
	body := map[string]any{
		"nil":    nil,
		"object": map[string]any{},
		"array":  []any{},
		"nested": []any{map[string]any{"k": []any{map[string]any{"deep": "1"}}}},
	}
	for _, c := range cases {
		body[c.name] = c.val
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)

	st := newStores()
	exp, err := matches.Prepare(newValidator(t), matches.Registration{
		Method: "GET", Path: "/api/v2/users/{id}",
		Response: matches.ResponseDef{Status: 200, Body: raw},
	})
	require.NoError(t, err)
	st.Matches.Put(exp)

	first := mustBytes(t)(seed.Export(st.Stores, false).YAML())
	st2 := newStores()
	parse(t, string(first)).Apply(st2.Stores)
	second := mustBytes(t)(seed.Export(st2.Stores, false).YAML())
	require.Equal(t, string(first), string(second))

	list := st2.Matches.List()
	require.Len(t, list, 1)
	var got map[string]any
	require.NoError(t, json.Unmarshal(list[0].Response.Body, &got))
	for _, c := range cases {
		require.Equal(t, c.val, got[c.name], c.name)
	}
	require.Contains(t, got, "nil")
	require.Nil(t, got["nil"])
	require.Equal(t, map[string]any{}, got["object"])
	require.Equal(t, []any{}, got["array"])
	require.Equal(t, body["nested"], got["nested"])
}
