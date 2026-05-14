package matches

import (
	"encoding/json"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func resp(status int, body string) ResponseDef {
	return ResponseDef{Status: status, Body: json.RawMessage(body)}
}

func bodyMatcher(body string) *RequestMatcher {
	return &RequestMatcher{Body: json.RawMessage(body)}
}

func TestStore_PutAndFind_Exact(t *testing.T) {
	s := NewStore()
	s.Put(Expectation{Method: "GET", Path: "/api/v2/users/123", Kind: KindExact, Response: resp(200, `{"x":1}`)})

	m := s.Find("GET", "/api/v2/users/123", "/api/v2/users/{id}", MatchableRequest{})
	if assert.NotNil(t, m) {
		assert.Equal(t, 200, m.Response.Status)
		assert.JSONEq(t, `{"x":1}`, string(m.Response.Body))
	}
}

func TestStore_PutAndFind_TemplateFallback(t *testing.T) {
	s := NewStore()
	s.Put(Expectation{Method: "GET", Path: "/api/v2/users/{id}", Kind: KindTemplate, Response: resp(200, `{"any":true}`)})

	m := s.Find("GET", "/api/v2/users/anything", "/api/v2/users/{id}", MatchableRequest{})
	if assert.NotNil(t, m) {
		assert.JSONEq(t, `{"any":true}`, string(m.Response.Body))
	}
}

func TestStore_ExactWinsOverTemplate(t *testing.T) {
	s := NewStore()
	s.Put(Expectation{Method: "GET", Path: "/api/v2/users/{id}", Kind: KindTemplate, Response: resp(200, `{"who":"any"}`)})
	s.Put(Expectation{Method: "GET", Path: "/api/v2/users/123", Kind: KindExact, Response: resp(200, `{"who":"123"}`)})

	m := s.Find("GET", "/api/v2/users/123", "/api/v2/users/{id}", MatchableRequest{})
	if assert.NotNil(t, m) {
		assert.JSONEq(t, `{"who":"123"}`, string(m.Response.Body))
	}
}

func TestStore_FindMiss(t *testing.T) {
	s := NewStore()
	assert.Nil(t, s.Find("GET", "/api/v2/users/123", "/api/v2/users/{id}", MatchableRequest{}))
}

func TestStore_Put_ReplacesEqualMatcher(t *testing.T) {
	s := NewStore()
	// Catch-all replaced by catch-all.
	s.Put(Expectation{Method: "GET", Path: "/api/v2/users/123", Kind: KindExact, Response: resp(200, `{"v":1}`)})
	s.Put(Expectation{Method: "GET", Path: "/api/v2/users/123", Kind: KindExact, Response: resp(201, `{"v":2}`)})
	m := s.Find("GET", "/api/v2/users/123", "/api/v2/users/{id}", MatchableRequest{})
	if assert.NotNil(t, m) {
		assert.Equal(t, 201, m.Response.Status)
	}
	assert.Len(t, s.List(), 1)

	// Equal body matcher replaced (semantic JSON equality, not byte equality).
	s.Put(Expectation{Method: "POST", Path: "/api/v2/users", Kind: KindExact,
		Request: bodyMatcher(`{"email":"a@x"}`), Response: resp(201, `{"v":1}`)})
	s.Put(Expectation{Method: "POST", Path: "/api/v2/users", Kind: KindExact,
		Request: bodyMatcher(`{ "email": "a@x" }`), Response: resp(201, `{"v":2}`)})
	assert.Len(t, s.List(), 2) // One POST entry + one GET entry above.
}

func TestStore_RequestMatcherBeatsCatchAll(t *testing.T) {
	s := NewStore()
	s.Put(Expectation{Method: "POST", Path: "/api/v2/users", Kind: KindExact, Response: resp(201, `{"who":"catchall"}`)})
	s.Put(Expectation{Method: "POST", Path: "/api/v2/users", Kind: KindExact,
		Request: bodyMatcher(`{"email":"a@x"}`), Response: resp(201, `{"who":"specific"}`)})

	m := s.Find("POST", "/api/v2/users", "/api/v2/users", MatchableRequest{Body: []byte(`{"email":"a@x","extra":1}`)})
	if assert.NotNil(t, m) {
		assert.JSONEq(t, `{"who":"specific"}`, string(m.Response.Body))
	}

	// A request the specific matcher rejects falls back to the catch-all.
	m = s.Find("POST", "/api/v2/users", "/api/v2/users", MatchableRequest{Body: []byte(`{"email":"other@x"}`)})
	if assert.NotNil(t, m) {
		assert.JSONEq(t, `{"who":"catchall"}`, string(m.Response.Body))
	}
}

func TestStore_NewestWinsAmongEqualSpecificity(t *testing.T) {
	s := NewStore()
	s.Put(Expectation{Method: "POST", Path: "/api/v2/users", Kind: KindExact,
		Request: bodyMatcher(`{"email":"a@x"}`), Response: resp(201, `{"gen":1}`)})
	s.Put(Expectation{Method: "POST", Path: "/api/v2/users", Kind: KindExact,
		Request: bodyMatcher(`{"connection":"db"}`), Response: resp(201, `{"gen":2}`)})

	// Both matchers match; the newest-registered (gen 2) wins.
	m := s.Find("POST", "/api/v2/users", "/api/v2/users",
		MatchableRequest{Body: []byte(`{"email":"a@x","connection":"db"}`)})
	if assert.NotNil(t, m) {
		assert.JSONEq(t, `{"gen":2}`, string(m.Response.Body))
	}
}

func TestStore_ExactCatchAllBeatsTemplateMatcher(t *testing.T) {
	s := NewStore()
	s.Put(Expectation{Method: "GET", Path: "/api/v2/users/{id}", Kind: KindTemplate,
		Request: &RequestMatcher{Query: map[string]string{"fields": "email"}}, Response: resp(200, `{"tier":"tmpl-matcher"}`)})
	s.Put(Expectation{Method: "GET", Path: "/api/v2/users/123", Kind: KindExact, Response: resp(200, `{"tier":"exact-catchall"}`)})

	m := s.Find("GET", "/api/v2/users/123", "/api/v2/users/{id}",
		MatchableRequest{Query: url.Values{"fields": {"email"}}})
	if assert.NotNil(t, m) {
		assert.JSONEq(t, `{"tier":"exact-catchall"}`, string(m.Response.Body))
	}
}

func TestStore_QuerySubsetMatch(t *testing.T) {
	s := NewStore()
	s.Put(Expectation{Method: "GET", Path: "/api/v2/users", Kind: KindExact,
		Request: &RequestMatcher{Query: map[string]string{"q": "email:a@x"}}, Response: resp(200, `[]`)})

	// Extra query params are allowed.
	m := s.Find("GET", "/api/v2/users", "/api/v2/users",
		MatchableRequest{Query: url.Values{"q": {"email:a@x"}, "page": {"0"}}})
	assert.NotNil(t, m)

	// A different value does not match.
	m = s.Find("GET", "/api/v2/users", "/api/v2/users",
		MatchableRequest{Query: url.Values{"q": {"email:b@x"}}})
	assert.Nil(t, m)
}

func TestStore_List(t *testing.T) {
	s := NewStore()
	s.Put(Expectation{Method: "GET", Path: "/api/v2/users/{id}", Kind: KindTemplate, Response: resp(200, `{}`)})
	s.Put(Expectation{Method: "GET", Path: "/api/v2/users/123", Kind: KindExact, Response: resp(200, `{}`)})
	s.Put(Expectation{Method: "GET", Path: "/api/v2/users/123", Kind: KindExact,
		Request: bodyMatcher(`{"x":1}`), Response: resp(200, `{}`)})

	assert.Len(t, s.List(), 3)
}

func TestStore_ResetEndpoint_ClearsWholeList(t *testing.T) {
	s := NewStore()
	s.Put(Expectation{Method: "GET", Path: "/api/v2/users/123", Kind: KindExact, Response: resp(200, `{}`)})
	s.Put(Expectation{Method: "GET", Path: "/api/v2/users/123", Kind: KindExact,
		Request: bodyMatcher(`{"x":1}`), Response: resp(200, `{}`)})
	s.Put(Expectation{Method: "GET", Path: "/api/v2/users/{id}", Kind: KindTemplate, Response: resp(200, `{}`)})

	s.ResetEndpoint("GET", "/api/v2/users/123", KindExact)

	assert.Nil(t, s.Find("GET", "/api/v2/users/123", "/api/v2/users/{id}-other", MatchableRequest{}))
	assert.NotNil(t, s.Find("GET", "/api/v2/users/999", "/api/v2/users/{id}", MatchableRequest{}))
}

func TestStore_ResetAll(t *testing.T) {
	s := NewStore()
	s.Put(Expectation{Method: "GET", Path: "/api/v2/users/{id}", Kind: KindTemplate, Response: resp(200, `{}`)})
	s.Put(Expectation{Method: "POST", Path: "/api/v2/clients", Kind: KindExact, Response: resp(201, `{}`)})

	s.ResetAll()

	assert.Empty(t, s.List())
}

func TestStore_ConcurrentAccess(t *testing.T) {
	s := NewStore()
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(int) {
			defer wg.Done()
			s.Put(Expectation{Method: "GET", Path: "/api/v2/users/x", Kind: KindExact, Response: resp(200, `{}`)})
			_ = s.Find("GET", "/api/v2/users/x", "/api/v2/users/{id}", MatchableRequest{})
			_ = s.List()
			s.ResetAll()
		}(i)
	}
	wg.Wait()
}
