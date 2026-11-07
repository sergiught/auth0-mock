package seed_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/api"
	"github.com/sergiught/auth0-mock/internal/claims"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/mfa"
	"github.com/sergiught/auth0-mock/internal/permissions"
	"github.com/sergiught/auth0-mock/internal/seed"
	"github.com/sergiught/auth0-mock/internal/spec"
)

var (
	validatorOnce sync.Once
	validator     *spec.Validator
	validatorErr  error
)

// newValidator builds the (slow) spec validator once per test binary.
func newValidator(t *testing.T) *spec.Validator {
	t.Helper()
	validatorOnce.Do(func() {
		var s *spec.Spec
		s, validatorErr = spec.Load(api.ManagementOpenAPIJSON)
		if validatorErr == nil {
			validator, validatorErr = spec.NewValidator(s)
		}
	})
	require.NoError(t, validatorErr)
	return validator
}

const full = `
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
      body: {user_id: "auth0|1", email: a@b.co}
  - method: POST
    path: /api/v2/users
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

func parse(t *testing.T, src string) *seed.Seed {
	t.Helper()
	s, err := seed.Parse([]byte(src), newValidator(t))
	require.NoError(t, err)
	return s
}

func parseErr(t *testing.T, src string) string {
	t.Helper()
	_, err := seed.Parse([]byte(src), newValidator(t))
	require.Error(t, err)
	return err.Error()
}

func TestParseAllSections(t *testing.T) {
	s := parse(t, full)
	require.Len(t, s.Expectations, 4)
	require.Equal(t, "gold", s.Claims["tier"])
	require.Equal(t, "org_id", s.ClaimMappings["org"])
	require.Equal(t, []string{"read:x"}, s.Permissions["https://api/"])
	require.True(t, s.MFARequired)
}

func TestParseJSON(t *testing.T) {
	y := parse(t, full)
	var anyv any
	require.NoError(t, json.Unmarshal(mustJSON(t), &anyv))
	j, err := json.Marshal(anyv)
	require.NoError(t, err)
	js, err := seed.Parse(j, newValidator(t))
	require.NoError(t, err)
	require.True(t, reflect.DeepEqual(y, js))
}

func mustJSON(t *testing.T) []byte {
	t.Helper()
	// Round trip through the parser's own conversion is private; build JSON by hand.
	return []byte(`{"expectations":[
{"method":"GET","path":"/api/v2/users","response":{"status":429}},
{"method":"GET","path":"/api/v2/users","request":{"query":{"q":"a"}},"response":{"status":429}},
{"method":"GET","path":"/api/v2/users/{id}","response":{"status":200,"body":{"user_id":"auth0|1","email":"a@b.co"}}},
{"method":"POST","path":"/api/v2/users","response":{"status":429}}],
"claims":{"tier":"gold","plan":"pro"},"claim_mappings":{"org":"org_id"},
"permissions":{"https://api/":["read:x"]},"mfa_required":true}`)
}

func TestParseEmpty(t *testing.T) {
	for _, src := range []string{"", "# only a comment\n"} {
		s := parse(t, src)
		require.Equal(t, &seed.Seed{}, s)
	}
}

func TestParseDocumentMarkers(t *testing.T) {
	require.Equal(t, &seed.Seed{}, parse(t, "---\n"))
	parse(t, "---\nmfa_required: true\n")
	require.Equal(t, "seed file must contain a single YAML document",
		parseErr(t, "mfa_required: true\n---\nmfa_required: false\n"))
}

func TestParseUnknownTopKey(t *testing.T) {
	require.Equal(t, `unknown key "expectation"`, parseErr(t, "expectation: []\n"))
}

func TestParseUnknownItemKey(t *testing.T) {
	src := `
expectations:
  - {method: GET, path: /api/v2/users, response: {status: 429}}
  - {methd: GET, path: /api/v2/users, response: {status: 429}}
`
	require.Equal(t, `expectations[1]: unknown key "methd"`, parseErr(t, src))
}

func TestParseIgnoresIDAndHits(t *testing.T) {
	src := `
expectations:
  - {id: abc, hits: 3, method: GET, path: /api/v2/users, response: {status: 429}}
`
	require.Len(t, parse(t, src).Expectations, 1)
}

func TestParseBadExpectationNamesIndex(t *testing.T) {
	src := `
expectations:
  - {method: GET, path: /api/v2/users, response: {status: 429}}
  - {method: POST, path: /api/v2/users, response: {status: 429}}
  - method: GET
    path: /api/v2/users/{id}
    response: {status: 200, body: {user_id: 5}}
`
	require.True(t, strings.HasPrefix(parseErr(t, src),
		"expectations[2]: registered response violates schema for status 200: "))
}

func TestParseDuplicate(t *testing.T) {
	src := `
expectations:
  - {method: GET, path: /api/v2/users, response: {status: 429}}
  - {method: POST, path: /api/v2/users, response: {status: 429}}
  - {method: get, path: /api/v2/users, response: {status: 429}}
`
	require.Equal(t, "expectations[0] and expectations[2] have the same method, path and request matcher", parseErr(t, src))
}

func TestParseBadPermissionsType(t *testing.T) {
	require.Contains(t, parseErr(t, `permissions: {aud: "read:x"}`), "permissions")
}

func TestParseNotAMapping(t *testing.T) {
	require.Equal(t, "the seed file must be a mapping", parseErr(t, "- a\n- b\n"))
}

func firstBody(t *testing.T, src string) string {
	t.Helper()
	return string(parse(t, src).Expectations[0].Response.Body)
}

func TestParseYAMLTimestampStaysString(t *testing.T) {
	for _, ts := range []string{"2024-01-01", "2024-01-01 10:00:00", "2024-01-01T10:00:00.500+02:00"} {
		t.Run(ts, func(t *testing.T) {
			src := `
expectations:
  - method: GET
    path: /api/v2/users/{id}
    response: {status: 200, body: {user_id: "auth0|1", user_metadata: {created_at: ` + ts + `}}}
`
			require.Contains(t, firstBody(t, src), `"created_at":"`+ts+`"`)
		})
	}
}

func TestParseLargeIntExact(t *testing.T) {
	src := `
expectations:
  - method: GET
    path: /api/v2/users/{id}
    response: {status: 200, body: {user_id: "auth0|1", user_metadata: {n: 9007199254740993}}}
`
	require.Contains(t, firstBody(t, src), "9007199254740993")
}

func TestParseNumericMapKeys(t *testing.T) {
	src := `
expectations:
  - method: GET
    path: /api/v2/users/{id}
    response: {status: 200, body: {user_id: "auth0|1", user_metadata: {200: ok, true: x}}}
`
	require.Contains(t, firstBody(t, src), `"user_metadata":{"200":"ok","true":"x"}`)
}

type allStores struct {
	seed.Stores
}

func newStores() allStores {
	return allStores{seed.Stores{
		Matches:       matches.NewStore(),
		Claims:        claims.NewStore(),
		ClaimMappings: claims.NewMappingStore(),
		Permissions:   permissions.NewStore(),
		MFA:           mfa.NewStore(),
	}}
}

func (a allStores) reset() {
	a.Matches.ResetAll()
	a.Claims.Clear()
	a.ClaimMappings.Clear()
	a.Permissions.Clear()
	a.MFA.Reset()
}

func TestApply(t *testing.T) {
	s := parse(t, full)
	st := newStores()
	s.Apply(st.Stores)

	list := st.Matches.List()
	require.Len(t, list, 4)
	var users []matches.Expectation
	for _, e := range list {
		if e.Method == "GET" && e.Path == "/api/v2/users" {
			users = append(users, e)
		}
	}
	require.Len(t, users, 2)
	require.Nil(t, users[0].Request)
	require.NotNil(t, users[1].Request)
	require.Equal(t, "gold", st.Claims.Get()["tier"])
	require.Equal(t, "org_id", st.ClaimMappings.Get()["org"])
	require.Equal(t, []string{"read:x"}, st.Permissions.Get("https://api/"))
	require.True(t, st.MFA.IsRequired())

	oldIDs := map[string]bool{}
	for _, e := range list {
		oldIDs[e.ID] = true
	}
	st.reset()
	s.Apply(st.Stores)
	for _, e := range st.Matches.List() {
		require.False(t, oldIDs[e.ID], "ids must be fresh")
	}
}

func TestSummary(t *testing.T) {
	require.Equal(t, "4 expectations, 2 claims, 1 claim mapping, 1 permission audience, mfa required", parse(t, full).Summary())
	require.Equal(t, "empty", (&seed.Seed{}).Summary())
}

func TestParseDuplicateEmptyQueryIsOmittedQuery(t *testing.T) {
	src := `
expectations:
  - {method: GET, path: /api/v2/users, request: {query: {}, headers: {X-A: "1"}}, response: {status: 429}}
  - {method: GET, path: /api/v2/users, request: {headers: {X-A: "1"}}, response: {status: 429}}
`
	require.Equal(t, "expectations[0] and expectations[1] have the same method, path and request matcher", parseErr(t, src))
}

func TestParseRejectsKeysThatCollideAsText(t *testing.T) {
	require.Contains(t, parseErr(t, "claims: {0xC8: first, \"200\": second}\n"),
		`duplicate key "200" after conversion to text`)
}

func TestParseRejectsKeysThatDifferOnlyInCase(t *testing.T) {
	require.Equal(t, `unknown key "Expectations" (keys are case-sensitive)`, parseErr(t, `
expectations:
  - {method: GET, path: /api/v2/users, response: {status: 429}}
Expectations:
  - {method: GET, path: /api/v2/roles, response: {status: 429}}
`))
	require.Equal(t, `expectations[0]: unknown key "Method" (keys are case-sensitive)`, parseErr(t, `
expectations:
  - {Method: GET, path: /api/v2/users, response: {status: 429}}
`))
	require.Equal(t, `expectations[0]: unknown key "Status" (keys are case-sensitive)`, parseErr(t, `
expectations:
  - {method: GET, path: /api/v2/users, response: {Status: 429}}
`))
}

func TestParseKeepsCaseOfFreeFormKeys(t *testing.T) {
	s := parse(t, `
claims: {Tier: gold}
expectations:
  - method: GET
    path: /api/v2/users
    request: {headers: {X-Trace: "1"}}
    response: {status: 429}
`)
	require.Equal(t, "gold", s.Claims["Tier"])
	require.Len(t, s.Expectations, 1)
}

func TestParseAnchoredNumberReusedAsKey(t *testing.T) {
	s := parse(t, "claims:\n  level: &n 42\n  *n : answer\n")
	require.Equal(t, map[string]any{"level": float64(42), "42": "answer"}, s.Claims)
	require.Equal(t, `duplicate key "42" after conversion to text`,
		parseErr(t, "claims:\n  level: &n 42\n  \"42\": a\n  *n : b\n"))
}

func TestParseKeysKeepTheirWrittenText(t *testing.T) {
	s := parse(t, "claims:\n  1.10: a\n  123456789012345678901234567890: b\n  ~: c\n  &n 0.10000000000000000555: d\n  v: *n\n")
	require.Equal(t, map[string]any{
		"1.10": "a", "123456789012345678901234567890": "b", "null": "c",
		"0.10000000000000000555": "d", "v": float64(0.1),
	}, s.Claims)
	src := `
expectations:
  - method: GET
    path: /api/v2/users/{id}
    response: {status: 200, body: {user_id: "auth0|1", user_metadata: {&n 0.10000000000000000555: k, v: *n}}}
`
	require.Contains(t, firstBody(t, src), `"user_metadata":{"0.10000000000000000555":"k","v":0.10000000000000000555}`)
}
