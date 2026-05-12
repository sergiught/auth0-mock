package httperr

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteMgmt(t *testing.T) {
	w := httptest.NewRecorder()
	WriteMgmt(w, 404, "Not Found", "no match", "no_match")

	assert.Equal(t, 404, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, float64(404), body["statusCode"])
	assert.Equal(t, "Not Found", body["error"])
	assert.Equal(t, "no match", body["message"])
	assert.Equal(t, "no_match", body["errorCode"])
}

func TestWriteAuth(t *testing.T) {
	w := httptest.NewRecorder()
	WriteAuth(w, 400, "invalid_request", "missing grant_type")

	assert.Equal(t, 400, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "invalid_request", body["error"])
	assert.Equal(t, "missing grant_type", body["error_description"])
}
