package authapi

import (
	"encoding/json"
	"maps"
	"net/http"
	"strings"

	"github.com/sergiught/auth0-mock/internal/httperr"
)

func userinfo(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			httperr.WriteAuth(w, http.StatusUnauthorized, "invalid_token", "missing bearer token")
			return
		}
		claims, err := d.Keys.Verify(strings.TrimPrefix(h, "Bearer "))
		if err != nil {
			httperr.WriteAuth(w, http.StatusUnauthorized, "invalid_token", err.Error())
			return
		}
		out := map[string]any{"sub": claims.Subject}
		maps.Copy(out, claims.Extra)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}
