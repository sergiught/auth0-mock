package admin0

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"

	"github.com/sergiught/auth0-mock/internal/httperr"
	"github.com/sergiught/auth0-mock/internal/mfa"
)

// mfaUserBody is the wire shape of one per-user MFA config.
type mfaUserBody struct {
	User string `json:"user"`
	mfa.UserConfig
}

// mfaUserParam reads the {user} path parameter, decoded exactly once. Chi
// routes on RawPath only when it is set, so the param is still escaped then;
// otherwise it comes from the already-decoded Path. It writes a 400 and
// returns false when the escape is malformed.
func mfaUserParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	user := chi.URLParam(r, "user")
	if r.URL.RawPath == "" {
		return user, true
	}
	user, err := url.PathUnescape(user)
	if err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", "invalid user: "+err.Error(), "invalid_body")
		return "", false
	}
	return user, true
}

func writeMFAUserNotFound(w http.ResponseWriter, user string) {
	httperr.WriteMgmt(w, http.StatusNotFound, "Not Found", "no mfa config for user "+user, "unknown_user")
}

// PutMFAUserHandler replaces one user's MFA config.
type PutMFAUserHandler struct {
	Store *mfa.Store
}

func (h *PutMFAUserHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user, ok := mfaUserParam(w, r)
	if !ok {
		return
	}
	var cfg mfa.UserConfig
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", "decode body: "+err.Error(), "invalid_body")
		return
	}
	stored, err := h.Store.SetUser(user, cfg)
	if err != nil {
		httperr.WriteMgmt(w, http.StatusBadRequest, "Bad Request", err.Error(), "invalid_body")
		return
	}
	render.JSON(w, r, mfaUserBody{User: user, UserConfig: stored})
}

// GetMFAUserHandler returns one user's MFA config.
type GetMFAUserHandler struct {
	Store *mfa.Store
}

func (h *GetMFAUserHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user, ok := mfaUserParam(w, r)
	if !ok {
		return
	}
	cfg, found := h.Store.GetUser(user)
	if !found {
		writeMFAUserNotFound(w, user)
		return
	}
	render.JSON(w, r, mfaUserBody{User: user, UserConfig: cfg})
}

// DeleteMFAUserHandler removes one user's MFA config.
type DeleteMFAUserHandler struct {
	Store *mfa.Store
}

func (h *DeleteMFAUserHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user, ok := mfaUserParam(w, r)
	if !ok {
		return
	}
	if !h.Store.DeleteUser(user) {
		writeMFAUserNotFound(w, user)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListMFAUsersHandler returns every configured user, sorted by user.
type ListMFAUsersHandler struct {
	Store *mfa.Store
}

func (h *ListMFAUsersHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	users := h.Store.Users()
	out := make([]mfaUserBody, 0, len(users))
	for user, cfg := range users {
		out = append(out, mfaUserBody{User: user, UserConfig: cfg})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].User < out[j].User })
	render.JSON(w, r, out)
}
