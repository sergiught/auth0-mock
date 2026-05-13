package authapi

import "net/http"

func logout(_ Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ret := r.URL.Query().Get("returnTo")
		if ret == "" {
			ret = "/"
		}
		w.Header().Set("Location", ret)
		w.WriteHeader(http.StatusFound)
	}
}
