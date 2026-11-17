package mgmtstore

import (
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/api"
	"github.com/sergiught/auth0-mock/internal/spec"
)

var update = flag.Bool("update", false, "rewrite testdata/classification.golden")

func loadEngine(t *testing.T) (*Engine, *spec.Spec) {
	t.Helper()
	s, err := spec.Load(api.ManagementOpenAPIJSON)
	require.NoError(t, err)
	v, err := spec.NewValidator(s)
	require.NoError(t, err)
	return NewEngine(s, v), s
}

func TestClassification(t *testing.T) {
	e, _ := loadEngine(t)
	lines := e.Describe()
	got := strings.Join(lines, "\n") + "\n"

	const golden = "testdata/classification.golden"
	if *update {
		require.NoError(t, os.WriteFile(golden, []byte(got), 0o644))
	}
	want, err := os.ReadFile(golden)
	require.NoError(t, err)
	assert.Equal(t, string(want), got)
	assert.Len(t, lines, 485)

	has := func(line string) {
		t.Helper()
		assert.Contains(t, lines, line)
	}
	has("POST\t/api/v2/users\tcreate\tid=user_id")
	has("GET\t/api/v2/organizations/{id}/invitations/{invitation_id}\tread\tid=id\tparent=/organizations/{id}")
	has("PATCH\t/api/v2/tenants/settings\tsettings-merge")
	has("GET\t/api/v2/users/{id}/roles\tstub")
	has("POST\t/api/v2/event-streams/{id}/redeliver\tstub")
	has("PATCH\t/api/v2/actions/triggers/{triggerId}/bindings\tstub")
}
