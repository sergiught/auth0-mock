package mgmtapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sergiught/auth0-mock/api"
	"github.com/sergiught/auth0-mock/internal/jwks"
	"github.com/sergiught/auth0-mock/internal/matches"
	"github.com/sergiught/auth0-mock/internal/spec"
)

var fixedNow = time.Date(2026, 11, 7, 12, 34, 56, 789_000_000, time.UTC)

type tmplEnv struct {
	r     chi.Router
	v     *spec.Validator
	store *matches.Store
	token string
}

func newTmplEnv(t *testing.T, strict bool) *tmplEnv {
	t.Helper()
	s, err := spec.Load(api.ManagementOpenAPIJSON)
	require.NoError(t, err)
	v, err := spec.NewValidator(s)
	require.NoError(t, err)
	ks, err := jwks.NewKeySet(jwks.Config{Issuer: "https://mock/", AccessTokenTTL: time.Hour})
	require.NoError(t, err)
	store := matches.NewStore()
	r := chi.NewRouter()
	require.NoError(t, Mount(MountOpts{Router: r, Spec: s, Validator: v, Store: store, Keys: ks,
		Log: zerolog.Nop(), Strict: strict, Now: func() time.Time { return fixedNow }}))
	return &tmplEnv{r: r, v: v, store: store, token: mintBearer(t, ks)}
}

func (e *tmplEnv) register(t *testing.T, reg matches.Registration) {
	t.Helper()
	exp, err := matches.Prepare(e.v, reg)
	require.NoError(t, err)
	e.store.Put(exp)
}

func (e *tmplEnv) do(method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.token)
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	return w
}

const userTmpl = `{"user_id":"auth0|{{uuid}}","email":"{{body.email}}","email_verified":false,"created_at":"{{now}}","identities":[{"provider":"auth0","user_id":"{{uuid}}","connection":"{{body.connection}}","isSocial":false}]}`

func TestRespondTemplate(t *testing.T) {
	e := newTmplEnv(t, true)
	e.register(t, matches.Registration{Method: "POST", Path: "/api/v2/users", Response: matches.ResponseDef{
		Status: 201, Template: true, Body: json.RawMessage(userTmpl),
		Headers: map[string]string{"Location": "/api/v2/users/auth0|{{uuid}}"},
	}})
	req := `{"email":"a\"b@x.io","connection":"Username-Password-Authentication","password":"x"}`
	ids := map[string]bool{}
	for range 2 {
		w := e.do("POST", "/api/v2/users", req)
		require.Equal(t, 201, w.Code, w.Body.String())
		var got struct {
			UserID     string `json:"user_id"`
			Email      string `json:"email"`
			CreatedAt  string `json:"created_at"`
			Identities []struct {
				UserID string `json:"user_id"`
			} `json:"identities"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		assert.Equal(t, "auth0|"+got.Identities[0].UserID, got.UserID)
		assert.Equal(t, `a"b@x.io`, got.Email)
		assert.Equal(t, "2026-11-07T12:34:56.789Z", got.CreatedAt)
		// Headers and body share one uuid.
		assert.True(t, strings.HasSuffix(w.Header().Get("Location"), got.Identities[0].UserID))
		ids[got.UserID] = true
	}
	assert.Len(t, ids, 2)
}

func TestRespondTemplatePath(t *testing.T) {
	e := newTmplEnv(t, true)
	e.register(t, matches.Registration{Method: "GET", Path: "/api/v2/users/{id}", Response: matches.ResponseDef{
		Status: 200, Template: true, Body: json.RawMessage(`{"user_id":"{{path.id}}"}`),
	}})
	w := e.do("GET", "/api/v2/users/auth0%7C9", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	assert.JSONEq(t, `{"user_id":"auth0|9"}`, w.Body.String())
}

func TestRespondNoFlagByteForByte(t *testing.T) {
	e := newTmplEnv(t, true)
	body := `{"user_id":"{{path.id}}",  "email":"{{body.email}}"}`
	e.register(t, matches.Registration{Method: "GET", Path: "/api/v2/users/{id}", Response: matches.ResponseDef{
		Status: 200, Body: json.RawMessage(body),
	}})
	w := e.do("GET", "/api/v2/users/auth0%7C9", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	assert.Equal(t, body, w.Body.String())
}

func TestRespondTemplateHeaderCRLF(t *testing.T) {
	for _, strict := range []bool{true, false} {
		e := newTmplEnv(t, strict)
		e.register(t, matches.Registration{Method: "POST", Path: "/api/v2/users", Response: matches.ResponseDef{
			Status: 201, Template: true, Body: json.RawMessage(userTmpl),
			Headers: map[string]string{"X-Echo": "{{body.email}}"},
		}})
		w := e.do("POST", "/api/v2/users", `{"email":"a\nb@x.io","connection":"c","password":"x"}`)
		require.Equal(t, 500, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "invalid_template")
	}
}

func TestRespondTemplateWrongTypeStrict(t *testing.T) {
	// A whole-string placeholder keeps the JSON type of the request value. The
	// request schema accepts any app_metadata content, so the string "yes"
	// reaches email_verified (a boolean in the response schema) unchanged.
	body := `{"user_id":"auth0|1","email":"a@x.io","email_verified":"{{body.app_metadata.v}}"}`
	req := `{"email":"a@x.io","connection":"c","password":"x","app_metadata":{"v":"yes"}}`
	for _, strict := range []bool{true, false} {
		e := newTmplEnv(t, strict)
		e.register(t, matches.Registration{Method: "POST", Path: "/api/v2/users", Response: matches.ResponseDef{
			Status: 201, Template: true, Body: json.RawMessage(body),
		}})
		w := e.do("POST", "/api/v2/users", req)
		if strict {
			require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), "invalid_match")
		} else {
			assert.Equal(t, 201, w.Code, w.Body.String())
		}
	}
}

func TestRespondTemplateErrorEnvelope(t *testing.T) {
	// Registration cannot know what {{body.app_metadata.code}} renders to, so the
	// envelope is checked when the response is served.
	body := `{"statusCode":"{{body.app_metadata.code}}","error":"Conflict","message":"m"}`
	for _, strict := range []bool{true, false} {
		e := newTmplEnv(t, strict)
		e.register(t, matches.Registration{Method: "POST", Path: "/api/v2/users", Response: matches.ResponseDef{
			Status: 409, Template: true, Body: json.RawMessage(body),
		}})
		w := e.do("POST", "/api/v2/users", `{"email":"a@x.io","connection":"c","password":"x","app_metadata":{"code":"oops"}}`)
		if strict {
			require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), "invalid_match")
		} else {
			require.Equal(t, 409, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), `"statusCode":"oops"`)
		}
	}
}

func TestRespondTemplateUnionResponse(t *testing.T) {
	// GET /api/v2/users answers with a oneOf (array or paginated object);
	// the placeholder in the array branch must still register.
	e := newTmplEnv(t, true)
	e.register(t, matches.Registration{Method: "GET", Path: "/api/v2/users", Response: matches.ResponseDef{
		Status: 200, Template: true, Body: json.RawMessage(`[{"email_verified":"{{body.verified}}"}]`),
	}})
}
