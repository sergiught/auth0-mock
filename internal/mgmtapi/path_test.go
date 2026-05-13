package mgmtapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKindOfPath_Detection(t *testing.T) {
	assert.Equal(t, KindTemplate, KindOfPath("/api/v2/users/{id}"))
	assert.Equal(t, KindExact, KindOfPath("/api/v2/users/auth0|123"))
}
