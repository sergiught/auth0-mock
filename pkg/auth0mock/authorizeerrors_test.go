package auth0mock_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/pkg/auth0mock"
)

func TestAuthorizeErrors_Add_WireShape(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"r1","error":"access_denied"}`))
	}

	got, err := c.AuthorizeErrors.Add(t.Context(), auth0mock.AuthorizeError{
		Match:            map[string]string{"client_id": "a"},
		Error:            "access_denied",
		ErrorDescription: "User cancelled",
	})
	require.NoError(t, err)
	assert.Equal(t, "r1", got.ID)

	call := rec.last(t)
	assert.Equal(t, http.MethodPost, call.Method)
	assert.Equal(t, "/admin0/authorize-errors", call.Path)
	assert.JSONEq(t, `{"match":{"client_id":"a"},"error":"access_denied","error_description":"User cancelled"}`, string(call.Body))
}

func TestAuthorizeErrors_Add_OmitsEmptyMatch(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"r1","error":"login_required"}`))
	}

	_, err := c.AuthorizeErrors.Add(t.Context(), auth0mock.AuthorizeError{Error: "login_required"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"error":"login_required"}`, string(rec.last(t).Body))
}

func TestAuthorizeErrors_List(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rules":[{"id":"r1","error":"x"}]}`))
	}

	got, err := c.AuthorizeErrors.List(t.Context())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "r1", got[0].ID)
	assert.Equal(t, "x", got[0].Error)

	call := rec.last(t)
	assert.Equal(t, http.MethodGet, call.Method)
	assert.Equal(t, "/admin0/authorize-errors", call.Path)
}

func TestAuthorizeErrors_DeleteAndClear(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)

	require.NoError(t, c.AuthorizeErrors.Delete(t.Context(), "a/b"))
	call := rec.last(t)
	assert.Equal(t, http.MethodDelete, call.Method)
	assert.Equal(t, "/admin0/authorize-errors/a%2Fb", call.RawPath)

	require.NoError(t, c.AuthorizeErrors.Clear(t.Context()))
	call = rec.last(t)
	assert.Equal(t, http.MethodDelete, call.Method)
	assert.Equal(t, "/admin0/authorize-errors", call.Path)
}

func TestAuthorizeErrors_Add_ServerError(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"statusCode":400,"error":"Bad Request","message":"bad","errorCode":"invalid_body"}`))
	}

	_, err := c.AuthorizeErrors.Add(t.Context(), auth0mock.AuthorizeError{})
	var apiErr *auth0mock.APIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.Equal(t, "invalid_body", apiErr.ErrorCode)
}

func TestAuthorizeErrors_List_ServerError(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"statusCode":500,"error":"Internal Server Error","message":"boom"}`))
	}

	rules, err := c.AuthorizeErrors.List(t.Context())
	var apiErr *auth0mock.APIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
	assert.Nil(t, rules)
}
