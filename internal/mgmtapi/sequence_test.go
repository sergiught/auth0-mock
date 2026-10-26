package mgmtapi

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sergiught/auth0-mock/internal/matches"
)

func TestGenericHandler_ServesSequence(t *testing.T) {
	e := newJournalEnv(t, 10, nil)
	e.store.Put(matches.Expectation{
		Method: "GET", Path: "/api/v2/users/{id}", Kind: matches.KindTemplate,
		Response: matches.ResponseDef{
			Status:  429,
			Headers: map[string]string{"Retry-After": "0"},
			Body:    json.RawMessage(`{"statusCode":429,"error":"Too Many Requests","message":"slow down"}`),
		},
		Then: []matches.ResponseDef{{
			Status: 200,
			Body:   json.RawMessage(`{"user_id":"auth0|1","email":"a@x"}`),
		}},
	})

	first := e.do("GET", "/api/v2/users/auth0|1", "", true)
	second := e.do("GET", "/api/v2/users/auth0|1", "", true)
	third := e.do("GET", "/api/v2/users/auth0|1", "", true)

	assert.Equal(t, 429, first.Code, first.Body.String())
	assert.Equal(t, "0", first.Header().Get("Retry-After"))
	assert.Equal(t, 200, second.Code, second.Body.String())
	assert.Empty(t, second.Header().Get("Retry-After"))
	assert.JSONEq(t, `{"user_id":"auth0|1","email":"a@x"}`, second.Body.String())
	assert.Equal(t, 200, third.Code, third.Body.String())
}
