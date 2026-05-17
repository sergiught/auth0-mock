package auth0mock_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClock_Get(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"mode":"frozen","now":"2030-01-01T00:00:00Z"}`))
	}

	got, err := c.Clock.Get(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "frozen", got.Mode)
	assert.True(t, got.Now.Equal(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)))
	assert.Equal(t, time.Duration(0), got.Offset)

	call := rec.last(t)
	assert.Equal(t, http.MethodGet, call.Method)
	assert.Equal(t, "/admin0/clock", call.Path)
}

func TestClock_Get_OffsetIsParsed(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"mode":"offset","now":"2030-01-01T00:00:00Z","offset":"25h"}`))
	}

	got, err := c.Clock.Get(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "offset", got.Mode)
	assert.Equal(t, 25*time.Hour, got.Offset)
}

func TestClock_Get_BadServerNow_ReturnsError(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)
	rec.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"mode":"real","now":"not-a-timestamp"}`))
	}

	_, err := c.Clock.Get(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse server now")
}

func TestClock_Freeze(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)

	require.NoError(t, c.Clock.Freeze(context.Background(),
		time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)))

	call := rec.last(t)
	assert.Equal(t, http.MethodPut, call.Method)
	assert.Equal(t, "/admin0/clock", call.Path)
	assert.Equal(t, "application/json", call.ContentType)
	assert.JSONEq(t, `{"now":"2030-01-01T00:00:00Z"}`, string(call.Body))
}

func TestClock_Offset(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)

	require.NoError(t, c.Clock.Offset(context.Background(), 25*time.Hour))

	call := rec.last(t)
	assert.Equal(t, http.MethodPut, call.Method)
	assert.JSONEq(t, `{"offset":"25h0m0s"}`, string(call.Body))
}

func TestClock_Advance(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)

	require.NoError(t, c.Clock.Advance(context.Background(), time.Hour))

	call := rec.last(t)
	assert.Equal(t, http.MethodPost, call.Method)
	assert.Equal(t, "/admin0/clock/advance", call.Path)
	assert.JSONEq(t, `{"by":"1h0m0s"}`, string(call.Body))
}

func TestClock_Reset(t *testing.T) {
	t.Parallel()
	rec, c := newStub(t)

	require.NoError(t, c.Clock.Reset(context.Background()))

	call := rec.last(t)
	assert.Equal(t, http.MethodDelete, call.Method)
	assert.Equal(t, "/admin0/clock", call.Path)
}
