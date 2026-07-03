package claims

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMappingStore_SetGetClear(t *testing.T) {
	t.Parallel()
	s := NewMappingStore()
	assert.Empty(t, s.Get())

	s.Set(map[string]string{"resource": "https://example.com/resource"})
	got := s.Get()
	assert.Equal(t, "https://example.com/resource", got["resource"])

	// Snapshot is a copy — mutation doesn't leak back.
	got["resource"] = "mutated"
	assert.Equal(t, "https://example.com/resource", s.Get()["resource"])

	s.Clear()
	assert.Empty(t, s.Get())
}

func TestMappingStore_Project(t *testing.T) {
	t.Parallel()
	s := NewMappingStore()
	s.Set(map[string]string{
		"resource": "https://example.com/resource",
		"tenant":   "https://example.com/tenant",
	})

	dst := map[string]any{
		"gty":                          "client-credentials",
		"https://example.com/resource": "stale-global-value",
	}
	s.Project(map[string]any{
		"resource": "urn:api:orders",
		"ignored":  "not-in-the-allowlist",
		// "tenant" absent from the request → claim untouched.
	}, dst)

	assert.Equal(t, "urn:api:orders", dst["https://example.com/resource"],
		"per-request value overwrites an existing (global) claim")
	assert.Equal(t, "client-credentials", dst["gty"], "unmapped keys are preserved")
	assert.NotContains(t, dst, "https://example.com/tenant", "absent parameters project nothing")
	assert.NotContains(t, dst, "ignored", "params outside the allowlist are ignored")
	assert.NotContains(t, dst, "not-in-the-allowlist")
}

// TestMappingStore_Set_ReplacesNotMerges pins PUT semantics: like the
// claims store, Set swaps the whole map — earlier mappings don't linger.
func TestMappingStore_Set_ReplacesNotMerges(t *testing.T) {
	t.Parallel()
	s := NewMappingStore()
	s.Set(map[string]string{"resource": "https://example.com/resource"})
	s.Set(map[string]string{"tenant": "https://example.com/tenant"})

	got := s.Get()
	assert.NotContains(t, got, "resource", "replaced mapping must not survive a second Set")
	assert.Equal(t, map[string]string{"tenant": "https://example.com/tenant"}, got)
}

// TestMappingStore_Project_SameClaimCollision_Deterministic nails down the
// two-parameters-one-claim case. Both `a_param` and `b_param` target the
// same claim; when both appear in a request, the lexicographically-last
// parameter must win — every time. Looped because the failure mode being
// guarded against is random map iteration order: a single pass would
// succeed by coin flip.
func TestMappingStore_Project_SameClaimCollision_Deterministic(t *testing.T) {
	t.Parallel()
	s := NewMappingStore()
	s.Set(map[string]string{
		"a_param": "https://example.com/claim",
		"b_param": "https://example.com/claim",
	})

	for range 100 {
		dst := map[string]any{}
		s.Project(map[string]any{"a_param": "from-a", "b_param": "from-b"}, dst)
		assert.Equal(t, "from-b", dst["https://example.com/claim"],
			"lexicographically-last parameter must win, deterministically")
	}

	// Only one of the colliding parameters present → no ambiguity, it wins.
	dst := map[string]any{}
	s.Project(map[string]any{"a_param": "from-a"}, dst)
	assert.Equal(t, "from-a", dst["https://example.com/claim"])
}

// TestMappingStore_Project_NullValue pins the JSON-null edge: a request
// body of {"resource": null} decodes to a present key with a nil value,
// and projects as an explicit null claim — present ≠ omitted.
func TestMappingStore_Project_NullValue(t *testing.T) {
	t.Parallel()
	s := NewMappingStore()
	s.Set(map[string]string{"resource": "https://example.com/resource"})

	dst := map[string]any{}
	s.Project(map[string]any{"resource": nil}, dst)
	v, present := dst["https://example.com/resource"]
	assert.True(t, present, "explicit null projects the claim")
	assert.Nil(t, v)
}

func TestMappingStore_Project_EmptyStoreIsNoop(t *testing.T) {
	t.Parallel()
	s := NewMappingStore()
	dst := map[string]any{"gty": "client-credentials"}
	s.Project(map[string]any{"resource": "urn:api:orders"}, dst)
	assert.Equal(t, map[string]any{"gty": "client-credentials"}, dst)
}

func TestMappingStore_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	s := NewMappingStore()
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(2)
		go func(i int) { defer wg.Done(); s.Set(map[string]string{"i": string(rune('a' + i%26))}) }(i)
		go func() { defer wg.Done(); s.Project(map[string]any{"i": "v"}, map[string]any{}) }()
	}
	wg.Wait()
}
