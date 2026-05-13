package matches

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStore_PutAndFind_Exact(t *testing.T) {
	s := NewStore()
	body := json.RawMessage(`{"x":1}`)
	s.Put(Match{Method: "GET", Path: "/api/v2/users/123", Kind: KindExact, Status: 200, Body: body})

	m := s.Find("GET", "/api/v2/users/123", "/api/v2/users/{id}")
	if assert.NotNil(t, m) {
		assert.Equal(t, 200, m.Status)
		assert.JSONEq(t, `{"x":1}`, string(m.Body))
	}
}

func TestStore_PutAndFind_TemplateFallback(t *testing.T) {
	s := NewStore()
	body := json.RawMessage(`{"any":true}`)
	s.Put(Match{Method: "GET", Path: "/api/v2/users/{id}", Kind: KindTemplate, Status: 200, Body: body})

	m := s.Find("GET", "/api/v2/users/anything", "/api/v2/users/{id}")
	if assert.NotNil(t, m) {
		assert.JSONEq(t, `{"any":true}`, string(m.Body))
	}
}

func TestStore_ExactWinsOverTemplate(t *testing.T) {
	s := NewStore()
	s.Put(Match{Method: "GET", Path: "/api/v2/users/{id}", Kind: KindTemplate, Status: 200, Body: json.RawMessage(`{"who":"any"}`)})
	s.Put(Match{Method: "GET", Path: "/api/v2/users/123", Kind: KindExact, Status: 200, Body: json.RawMessage(`{"who":"123"}`)})

	m := s.Find("GET", "/api/v2/users/123", "/api/v2/users/{id}")
	if assert.NotNil(t, m) {
		assert.JSONEq(t, `{"who":"123"}`, string(m.Body))
	}
}

func TestStore_FindMiss(t *testing.T) {
	s := NewStore()
	assert.Nil(t, s.Find("GET", "/api/v2/users/123", "/api/v2/users/{id}"))
}

func TestStore_OverwriteSameKey(t *testing.T) {
	s := NewStore()
	s.Put(Match{Method: "GET", Path: "/api/v2/users/123", Kind: KindExact, Status: 200, Body: json.RawMessage(`{"v":1}`)})
	s.Put(Match{Method: "GET", Path: "/api/v2/users/123", Kind: KindExact, Status: 201, Body: json.RawMessage(`{"v":2}`)})

	m := s.Find("GET", "/api/v2/users/123", "/api/v2/users/{id}")
	if assert.NotNil(t, m) {
		assert.Equal(t, 201, m.Status)
		assert.JSONEq(t, `{"v":2}`, string(m.Body))
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	s := NewStore()
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := "/api/v2/users/x"
			s.Put(Match{Method: "GET", Path: path, Kind: KindExact, Status: 200})
			_ = s.Find("GET", path, "/api/v2/users/{id}")
		}(i)
	}
	wg.Wait()
}

func TestStore_List(t *testing.T) {
	s := NewStore()
	s.Put(Match{Method: "GET", Path: "/api/v2/users/{id}", Kind: KindTemplate, Status: 200})
	s.Put(Match{Method: "GET", Path: "/api/v2/users/123", Kind: KindExact, Status: 200})

	list := s.List()
	assert.Len(t, list, 2)
}

func TestStore_ResetEndpoint_Exact(t *testing.T) {
	s := NewStore()
	s.Put(Match{Method: "GET", Path: "/api/v2/users/{id}", Kind: KindTemplate, Status: 200})
	s.Put(Match{Method: "GET", Path: "/api/v2/users/123", Kind: KindExact, Status: 200})

	s.ResetEndpoint("GET", "/api/v2/users/123", KindExact)

	assert.Nil(t, s.Find("GET", "/api/v2/users/123", "/api/v2/users/{id}-other-template"))
	// Template still serves any concrete id.
	assert.NotNil(t, s.Find("GET", "/api/v2/users/999", "/api/v2/users/{id}"))
}

func TestStore_ResetEndpoint_Template(t *testing.T) {
	s := NewStore()
	s.Put(Match{Method: "GET", Path: "/api/v2/users/{id}", Kind: KindTemplate, Status: 200})
	s.Put(Match{Method: "GET", Path: "/api/v2/users/123", Kind: KindExact, Status: 200})

	s.ResetEndpoint("GET", "/api/v2/users/{id}", KindTemplate)

	// Concrete remains.
	assert.NotNil(t, s.Find("GET", "/api/v2/users/123", "/api/v2/users/{id}"))
	// Template gone.
	assert.Nil(t, s.Find("GET", "/api/v2/users/999", "/api/v2/users/{id}"))
}

func TestStore_ResetAll(t *testing.T) {
	s := NewStore()
	s.Put(Match{Method: "GET", Path: "/api/v2/users/{id}", Kind: KindTemplate, Status: 200})
	s.Put(Match{Method: "POST", Path: "/api/v2/clients", Kind: KindExact, Status: 201})

	s.ResetAll()

	assert.Empty(t, s.List())
}
