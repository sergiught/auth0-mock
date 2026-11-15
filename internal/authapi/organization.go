package authapi

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// organization is the /authorize organization parameter, resolved. No
// organizations are stored: a value starting with org_ is an id used as is,
// anything else is a name that maps to a stable derived id.
type organization struct{ ID, Name string }

func resolveOrganization(v string) organization {
	v = strings.TrimSpace(v)
	if v == "" {
		return organization{}
	}
	if strings.HasPrefix(v, "org_") {
		return organization{ID: v}
	}
	name := strings.ToLower(v)
	sum := sha256.Sum256([]byte(name))
	return organization{ID: "org_" + hex.EncodeToString(sum[:])[:16], Name: name}
}

// addClaims sets org_id and org_name on m; the zero value is a no-op.
func (o organization) addClaims(m map[string]any) {
	if o.ID != "" {
		m["org_id"] = o.ID
	}
	if o.Name != "" {
		m["org_name"] = o.Name
	}
}
