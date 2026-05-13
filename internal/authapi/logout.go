package authapi

import "net/http"

// LogoutHandler redirects the user to returnTo (or "/").
type LogoutHandler struct{}

func (h *LogoutHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ret := r.URL.Query().Get("returnTo")
	if ret == "" {
		ret = "/"
	}
	w.Header().Set("Location", ret)
	w.WriteHeader(http.StatusFound)
}
