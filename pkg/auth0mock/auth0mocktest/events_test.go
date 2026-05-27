package auth0mocktest_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/pkg/auth0mock"
	"github.com/sergiught/auth0-mock/pkg/auth0mock/auth0mocktest"
)

// fakeSSEServer simulates the mock's /api/v2/events endpoint without
// dragging the full router in. Lets us exercise the helpers in
// isolation — the cmd/api e2e test covers the integration angle.
//
// The handler writes body then blocks until the test ends, so
// NextEvent doesn't see an early EOF in the happy-path tests.
func fakeSSEServer(t *testing.T, body string, statusCode int, contentType string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.WriteHeader(statusCode)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		_, _ = w.Write([]byte(body))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Hold the connection open until the test ends.
		<-t.Context().Done()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSubscribeEvents_ParsesIDEventData(t *testing.T) {
	body := "id: evt-1\nevent: user.created\ndata: {\"hello\":\"world\"}\n\n"
	srv := fakeSSEServer(t, body, http.StatusOK, "text/event-stream")
	c, err := auth0mock.NewClient(srv.URL)
	require.NoError(t, err)
	stream := auth0mocktest.SubscribeEvents(t, c, "bearer", "")
	evt := stream.NextEvent(t, time.Second)
	assert.Equal(t, "evt-1", evt.ID)
	assert.Equal(t, "user.created", evt.Type)
	assert.Equal(t, `{"hello":"world"}`, string(evt.Data))
}

func TestSubscribeEvents_StripsKeepAliveComments(t *testing.T) {
	body := ":keep-alive\n\n:another-comment\n\nid: evt-1\nevent: x\ndata: y\n\n"
	srv := fakeSSEServer(t, body, http.StatusOK, "text/event-stream")
	c, _ := auth0mock.NewClient(srv.URL)
	stream := auth0mocktest.SubscribeEvents(t, c, "bearer", "")
	evt := stream.NextEvent(t, time.Second)
	assert.Equal(t, "evt-1", evt.ID, "comment frames must be filtered out")
}

func TestSubscribeEvents_JoinsMultilineData(t *testing.T) {
	body := "id: evt-1\nevent: x\ndata: line1\ndata: line2\n\n"
	srv := fakeSSEServer(t, body, http.StatusOK, "text/event-stream")
	c, _ := auth0mock.NewClient(srv.URL)
	stream := auth0mocktest.SubscribeEvents(t, c, "bearer", "")
	evt := stream.NextEvent(t, time.Second)
	assert.Equal(t, "line1\nline2", string(evt.Data),
		"multi-line data fields should be joined with newline per SSE spec")
}

func TestNextEvent_TimeoutFatals(t *testing.T) {
	// Subscribe to a live but silent endpoint, then call NextEvent
	// with a tight timeout. The fakeT (from auth0mocktest_test.go)
	// records Fatalf without failing this test.
	srv := fakeSSEServer(t, "", http.StatusOK, "text/event-stream")
	c, _ := auth0mock.NewClient(srv.URL)
	stream := auth0mocktest.SubscribeEvents(t, c, "bearer", "")

	ft := &fakeT{}
	stream.NextEvent(ft, 50*time.Millisecond)
	assert.True(t, ft.fatalCalled.Load(), "NextEvent must fatal on timeout")
}

func TestSubscribeEvents_NonOKStatus_Fatals(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	c, _ := auth0mock.NewClient(srv.URL)

	ft := &fakeT{}
	auth0mocktest.SubscribeEvents(ft, c, "bearer", "")
	assert.True(t, ft.fatalCalled.Load(), "non-200 subscribe must fatal")
}

func TestSubscribeEvents_WrongContentType_Fatals(t *testing.T) {
	srv := fakeSSEServer(t, "{}", http.StatusOK, "application/json")
	c, _ := auth0mock.NewClient(srv.URL)
	ft := &fakeT{}
	auth0mocktest.SubscribeEvents(ft, c, "bearer", "")
	assert.True(t, ft.fatalCalled.Load(), "non-text/event-stream Content-Type must fatal")
}

func TestSubscribeEvents_QueryStringForwarded(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("id: e\nevent: t\ndata: d\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-t.Context().Done()
	}))
	t.Cleanup(srv.Close)
	c, _ := auth0mock.NewClient(srv.URL)
	stream := auth0mocktest.SubscribeEvents(t, c, "bearer", "event_type=user.created&from=evt_xxx")
	_ = stream.NextEvent(t, time.Second)
	assert.Equal(t, "event_type=user.created&from=evt_xxx", gotQuery)
}

func TestMustPush_FatalsOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"statusCode":400,"error":"Bad Request","errorCode":"invalid_event","message":"x"}`))
	}))
	t.Cleanup(srv.Close)
	c, _ := auth0mock.NewClient(srv.URL)
	ft := &fakeT{}
	auth0mocktest.MustPush(ft, c, `{}`)
	assert.True(t, ft.fatalCalled.Load(), "non-2xx push must fatal")
}

func TestMustPush_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)
	c, _ := auth0mock.NewClient(srv.URL)
	// Doesn't fatal — uses the real *testing.T.
	auth0mocktest.MustPush(t, c, `{}`)
}

func TestWaitForActiveSubscribers_ReturnsWhenCountSettles(t *testing.T) {
	// Active starts at 1 and drains to 0 on the second poll — the
	// helper must keep polling, not read once and fatal.
	var polls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		active := 0
		if polls.Add(1) == 1 {
			active = 1
		}
		_, _ = fmt.Fprintf(w, `{"active":%d,"total":1}`, active)
	}))
	t.Cleanup(srv.Close)
	c, _ := auth0mock.NewClient(srv.URL)

	// Uses the real *testing.T — must not fatal.
	auth0mocktest.WaitForActiveSubscribers(t, c, 0, time.Second)
	assert.Greater(t, polls.Load(), int64(1), "helper should poll until the count settles")
}

func TestWaitForActiveSubscribers_TimeoutFatals(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"active":1,"total":1}`))
	}))
	t.Cleanup(srv.Close)
	c, _ := auth0mock.NewClient(srv.URL)

	ft := &fakeT{}
	auth0mocktest.WaitForActiveSubscribers(ft, c, 0, 50*time.Millisecond)
	assert.True(t, ft.fatalCalled.Load(), "active never reaching want must fatal")
}

func TestSSEStream_CloseIsIdempotent(t *testing.T) {
	srv := fakeSSEServer(t, "", http.StatusOK, "text/event-stream")
	c, _ := auth0mock.NewClient(srv.URL)
	stream := auth0mocktest.SubscribeEvents(t, c, "bearer", "")
	stream.Close()
	stream.Close() // No panic.
}
