package mgmtapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRouterPath_Translates(t *testing.T) {
	assert.Equal(t, "/api/v2/users/:id", RouterPath("/api/v2/users/{id}"))
	assert.Equal(t, "/api/v2/orgs/:orgId/members/:memberId",
		RouterPath("/api/v2/orgs/{orgId}/members/{memberId}"))
	assert.Equal(t, "/api/v2/users", RouterPath("/api/v2/users"))
}

func TestKindOfPath_Detection(t *testing.T) {
	assert.Equal(t, KindTemplate, KindOfPath("/api/v2/users/{id}"))
	assert.Equal(t, KindExact, KindOfPath("/api/v2/users/auth0|123"))
}
