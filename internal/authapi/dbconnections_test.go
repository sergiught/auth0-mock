package authapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDBConnections_Signup_201(t *testing.T) {
	r, _ := newAuthRouter(t)
	body := `{"client_id":"abc","email":"alice@example.com","password":"pw","connection":"Username-Password-Authentication"}`

	req := httptest.NewRequest("POST", "/dbconnections/signup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "alice@example.com", resp["email"])
	assert.NotEmpty(t, resp["_id"])
}

func TestDBConnections_Signup_MissingEmail_400(t *testing.T) {
	r, _ := newAuthRouter(t)
	body := `{"client_id":"abc","password":"pw","connection":"x"}`
	req := httptest.NewRequest("POST", "/dbconnections/signup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDBConnections_ChangePassword_200(t *testing.T) {
	r, _ := newAuthRouter(t)
	body := `{"client_id":"abc","email":"alice@example.com","connection":"x"}`
	req := httptest.NewRequest("POST", "/dbconnections/change_password", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "We've just sent you an email")
}
