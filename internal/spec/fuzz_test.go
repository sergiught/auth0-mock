package spec

import (
	"testing"

	"github.com/sergiught/auth0-mock/api"
)

// FuzzResolve feeds arbitrary method/path pairs through the OpenAPI router.
// Paths come straight off untrusted HTTP requests, so Resolve must return a
// clean (Operation, error) for any input and never panic inside the matcher.
// The single contract checked here: a nil error implies a resolved template.
func FuzzResolve(f *testing.F) {
	s, err := Load(api.ManagementOpenAPIJSON)
	if err != nil {
		f.Fatalf("load spec: %v", err)
	}
	v, err := NewValidator(s)
	if err != nil {
		f.Fatalf("new validator: %v", err)
	}

	f.Add("GET", "/api/v2/users")
	f.Add("POST", "/api/v2/clients")
	f.Add("", "")
	f.Add("GET", "/api/v2/users/%ff/../../etc")
	f.Add("\U0001f525", "/{}/\x00")

	f.Fuzz(func(t *testing.T, method, path string) {
		op, err := v.Resolve(method, path)
		if err == nil && op.Template == "" {
			t.Fatalf("nil error but empty operation for %q %q", method, path)
		}
	})
}
