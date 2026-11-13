package seed_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const queryListSeed = `
expectations:
  - method: GET
    path: /api/v2/users
    request:
      query: {fields: [name, email], q: a}
    response: {status: 429}
`

func TestParseQueryList(t *testing.T) {
	s := parse(t, queryListSeed)
	require.Len(t, s.Expectations, 1)
	require.Equal(t, []string{"name", "email"}, s.Expectations[0].Request.Query["fields"])
	require.Equal(t, []string{"a"}, s.Expectations[0].Request.Query["q"])
}

func TestParseQueryListRejectsEmptyList(t *testing.T) {
	msg := parseErr(t, `
expectations:
  - method: GET
    path: /api/v2/users
    request:
      query: {fields: []}
    response: {status: 429}
`)
	require.Contains(t, msg, "query")
}

func TestParseUnknownRequestKeyStillRejected(t *testing.T) {
	require.Equal(t, `expectations[0]: unknown key "bogus" (keys are case-sensitive)`, parseErr(t, `
expectations:
  - method: GET
    path: /api/v2/users
    request:
      bogus: 1
    response: {status: 429}
`))
}

func TestParseRejectsUnexportedMatcherFieldNames(t *testing.T) {
	for _, key := range []string{"decodeErr", "arraysErr"} {
		require.Equal(t, `expectations[0]: unknown key "`+key+`" (keys are case-sensitive)`, parseErr(t, `
expectations:
  - method: GET
    path: /api/v2/users
    request:
      `+key+`: 1
    response: {status: 429}
`))
	}
}
