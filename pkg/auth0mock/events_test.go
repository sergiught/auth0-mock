package auth0mock_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/pkg/auth0mock"
)

func TestEventsClient_Push_SendsPayloadVerbatim(t *testing.T) {
	var (
		gotMethod, gotPath, gotCT string
		gotBody                   []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotCT = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"evt_aaaaaaaaaaaaaaaa"}`))
	}))
	t.Cleanup(srv.Close)

	c, err := auth0mock.NewClient(srv.URL)
	require.NoError(t, err)

	payload := json.RawMessage(`{
	  "type":"user.created","offset":"0",
	  "event":{"specversion":"1.0","type":"user.created","source":"x","id":"evt_aaaaaaaaaaaaaaaa","time":"2026-05-19T00:00:00Z","a0tenant":"t","a0stream":"est_aaaaaaaaaaaaaaaa","data":{"object":{"user_id":"u","created_at":"2026-05-19T00:00:00Z","updated_at":"2026-05-19T00:00:00Z","identities":[]}}}
	}`)
	err = c.Events.Push(context.Background(), auth0mock.Event{
		Type:    "user.created",
		ID:      "evt_aaaaaaaaaaaaaaaa",
		Payload: payload,
	})
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/admin0/events", gotPath)
	assert.Equal(t, "application/json", gotCT)
	assert.JSONEq(t, string(payload), string(gotBody))
}

func TestEventsClient_Push_PropagatesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"statusCode":400,"error":"Bad Request","errorCode":"invalid_event","message":"nope"}`))
	}))
	t.Cleanup(srv.Close)

	c, _ := auth0mock.NewClient(srv.URL)
	err := c.Events.Push(context.Background(), auth0mock.Event{
		Type:    "user.created",
		Payload: json.RawMessage(`{}`),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_event")
}

func TestEventsClient_Push_RejectsEmptyPayload(t *testing.T) {
	c, err := auth0mock.NewClient("http://localhost:1")
	require.NoError(t, err)
	err = c.Events.Push(context.Background(), auth0mock.Event{Type: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Payload is required")
}
