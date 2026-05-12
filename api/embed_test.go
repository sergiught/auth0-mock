package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEmbeddedSpecHasContent(t *testing.T) {
	assert.Greater(t, len(ManagementOpenAPIJSON), 100_000)
}
