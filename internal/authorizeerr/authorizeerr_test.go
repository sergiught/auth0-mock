package authorizeerr

import (
	"fmt"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStore_FindNewestWins(t *testing.T) {
	s := NewStore()
	s.Add(Rule{Match: map[string]string{"client_id": "a"}, Error: "access_denied"})
	newer := s.Add(Rule{Error: "login_required"})

	q := url.Values{"client_id": {"a"}}
	require.NotNil(t, s.Find(q))
	assert.Equal(t, "login_required", s.Find(q).Error)

	require.True(t, s.Delete(newer.ID))
	require.NotNil(t, s.Find(q))
	assert.Equal(t, "access_denied", s.Find(q).Error)
}

func TestStore_FindSubsetMatch(t *testing.T) {
	s := NewStore()
	s.Add(Rule{Match: map[string]string{"client_id": "a", "prompt": "none"}, Error: "login_required"})

	assert.NotNil(t, s.Find(url.Values{"client_id": {"a"}, "prompt": {"none"}, "scope": {"openid"}}))
	assert.Nil(t, s.Find(url.Values{"client_id": {"a"}}))
	assert.Nil(t, s.Find(url.Values{"client_id": {"b"}, "prompt": {"none"}}))
}

func TestStore_EmptyMatchHitsEverything(t *testing.T) {
	s := NewStore()
	s.Add(Rule{Error: "access_denied"})
	assert.NotNil(t, s.Find(url.Values{}))
}

func TestStore_AddAssignsID(t *testing.T) {
	s := NewStore()
	a := s.Add(Rule{ID: "forged", Error: "x"})
	b := s.Add(Rule{Error: "x"})
	assert.NotEmpty(t, a.ID)
	assert.NotEqual(t, "forged", a.ID)
	assert.NotEqual(t, a.ID, b.ID)
}

func TestStore_ListOrderAndClear(t *testing.T) {
	s := NewStore()
	a := s.Add(Rule{Error: "one"})
	b := s.Add(Rule{Error: "two"})
	assert.Equal(t, []Rule{a, b}, s.List())

	assert.False(t, s.Delete("unknown"))

	s.Clear()
	assert.Empty(t, s.List())
	assert.Nil(t, s.Find(url.Values{}))
}

func TestStore_ListReturnsCopies(t *testing.T) {
	s := NewStore()
	s.Add(Rule{Match: map[string]string{"client_id": "a"}, Error: "x"})

	s.List()[0].Match["client_id"] = "mutated"
	s.Find(url.Values{"client_id": {"a"}}).Match["client_id"] = "mutated"

	assert.Equal(t, "a", s.List()[0].Match["client_id"])
}

func TestStore_ConcurrentAddFindDelete(t *testing.T) {
	s := NewStore()
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := s.Add(Rule{Match: map[string]string{"n": fmt.Sprint(i)}, Error: "x"})
			s.Find(url.Values{"n": {fmt.Sprint(i)}})
			s.List()
			s.Delete(r.ID)
		}()
	}
	wg.Wait()
	assert.Empty(t, s.List())
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		rule Rule
		want string
	}{
		{"empty error", Rule{}, "error is required"},
		{"ok", Rule{Error: "access_denied"}, ""},
		{"quote in error", Rule{Error: `bad"quote`}, "error contains characters OAuth does not allow"},
		{"backslash in error", Rule{Error: `back\slash`}, "error contains characters OAuth does not allow"},
		{"tab in error", Rule{Error: "tab\t"}, "error contains characters OAuth does not allow"},
		{"unicode in error", Rule{Error: "ünicode"}, "error contains characters OAuth does not allow"},
		{"quote in description", Rule{Error: "x", ErrorDescription: `bad"quote`}, "error_description contains characters OAuth does not allow"},
		{"backslash in description", Rule{Error: "x", ErrorDescription: `back\slash`}, "error_description contains characters OAuth does not allow"},
		{"tab in description", Rule{Error: "x", ErrorDescription: "tab\t"}, "error_description contains characters OAuth does not allow"},
		{"unicode in description", Rule{Error: "x", ErrorDescription: "ünicode"}, "error_description contains characters OAuth does not allow"},
		{"spaces and symbols in description", Rule{Error: "x", ErrorDescription: "User said no &#=<"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.rule)
			if tt.want == "" {
				assert.NoError(t, err)
				return
			}
			assert.EqualError(t, err, tt.want)
		})
	}
}
