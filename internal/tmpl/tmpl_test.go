package tmpl

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testUUID = "11111111-1111-4111-8111-111111111111"

func testVars() Vars {
	return Vars{UUID: testUUID, Now: time.Date(2026, 11, 7, 12, 34, 56, 789e6, time.UTC)}
}

func renderBody(t *testing.T, tpl string, pathNames []string, v Vars) string {
	t.Helper()
	p, err := Parse(json.RawMessage(tpl), nil, pathNames)
	require.NoError(t, err)
	out, _, err := p.Render(v)
	require.NoError(t, err)
	return string(out)
}

func TestParseErrors(t *testing.T) {
	for _, ph := range []string{"{{nope}}", "{{uuid", "{{path.missing}}", "{{body.}}", "{{}}"} {
		_, err := Parse(json.RawMessage(`{"a":"`+ph+`"}`), nil, []string{"id"})
		require.Error(t, err, ph)
		assert.Contains(t, err.Error(), ph)
	}
	_, err := Parse(nil, map[string]string{"X": "{{nope}}"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "{{nope}}")
}

func TestRenderSharedValues(t *testing.T) {
	p, err := Parse(json.RawMessage(`{"a":"{{uuid}}","b":"x-{{ uuid }}","t":"{{now}}"}`),
		map[string]string{"Location": "/u/{{uuid}}"}, nil)
	require.NoError(t, err)
	body, h, err := p.Render(testVars())
	require.NoError(t, err)
	var got map[string]string
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, testUUID, got["a"])
	assert.Equal(t, "x-"+testUUID, got["b"])
	assert.Equal(t, "2026-11-07T12:34:56.789Z", got["t"])
	assert.Equal(t, "/u/"+testUUID, h["Location"])
}

func TestRenderBodyWholeStringTyped(t *testing.T) {
	v := testVars()
	v.Body = []byte(`{"s":"str","b":true,"n":42,"o":{"k":1},"a":[1,2]}`)
	cases := map[string]string{"s": `"str"`, "b": `true`, "n": `42`, "o": `{"k":1}`, "a": `[1,2]`}
	for k, want := range cases {
		assert.JSONEq(t, `{"x":`+want+`}`, renderBody(t, `{"x":"{{body.`+k+`}}"}`, nil, v), k)
	}
	assert.Equal(t, `{"x":{"k":1}}`, renderBody(t, `{"x":"{{ body.o }}"}`, nil, v))
}

func TestRenderBodyEmbedded(t *testing.T) {
	v := testVars()
	v.Body = []byte(`{"s":"str","n":42,"o":{"k":1}}`)
	assert.Equal(t, `{"x":"v=42 str {\"k\":1}"}`,
		renderBody(t, `{"x":"v={{body.n}} {{body.s}} {{body.o}}"}`, nil, v))
}

func TestRenderBodyMissing(t *testing.T) {
	for _, b := range [][]byte{[]byte(`{"s":1}`), nil, []byte("not json")} {
		v := testVars()
		v.Body = b
		assert.Equal(t, `{"b":"x"}`,
			renderBody(t, `{"a":"{{body.nope}}","b":"x{{body.nope}}"}`, nil, v))
	}
}

func TestRenderBodyMissingDropsKeysAndElements(t *testing.T) {
	v := testVars()
	v.Body = []byte(`{"a":1,"c":3}`)
	assert.Equal(t, `{"k":[1,3],"n":{"keep":"x","o":{}}}`,
		renderBody(t, `{"k":["{{body.a}}","{{body.b}}","{{body.c}}"],"n":{"keep":"x","gone":"{{body.b}}","o":{"deep":"{{body.z.y}}"}}}`, nil, v))
}

func TestRenderBodyNonJSONDropsWholeKeepsEmbedded(t *testing.T) {
	v := testVars()
	v.Body = []byte("not json")
	assert.Equal(t, `{"emb":"u="}`,
		renderBody(t, `{"whole":"{{body.a}}","emb":"u={{body.a}}"}`, nil, v))
}

func TestRenderBodyWholeBodyMissingIsEmptyString(t *testing.T) {
	assert.Equal(t, `""`, renderBody(t, `"{{body.a}}"`, nil, testVars()))
}

func TestRenderBodyPresentFalsyValuesKept(t *testing.T) {
	v := testVars()
	v.Body = []byte(`{"f":false,"z":0,"n":null,"e":""}`)
	assert.Equal(t, `{"e":"","f":false,"n":null,"z":0}`,
		renderBody(t, `{"f":"{{body.f}}","z":"{{body.z}}","n":"{{body.n}}","e":"{{body.e}}"}`, nil, v))
}

func TestRenderSampleKeepsSlot(t *testing.T) {
	v := testVars()
	v.Sample = true
	assert.Equal(t, `{"a":"","l":[""]}`,
		renderBody(t, `{"a":"{{body.a}}","l":["{{body.b}}"]}`, nil, v))
}

func TestRenderPath(t *testing.T) {
	v := testVars()
	v.Path = map[string]string{"id": "auth0|1"}
	assert.Equal(t, `{"a":"auth0|1"}`, renderBody(t, `{"a":"{{path.id}}"}`, []string{"id"}, v))
}

func TestRenderEscape(t *testing.T) {
	assert.Equal(t, `{"a":"{{uuid}}"}`, renderBody(t, `{"a":"\\{{uuid}}"}`, nil, testVars()))
}

func TestRenderEscapesJSON(t *testing.T) {
	v := testVars()
	v.Body = []byte(`{"e":"a\"b\\c\nd"}`)
	out := renderBody(t, `{"x":"{{body.e}}","y":"<{{body.e}}>"}`, nil, v)
	var got map[string]string
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	want := "a\"b\\c\nd"
	assert.Equal(t, want, got["x"])
	assert.Equal(t, "<"+want+">", got["y"])
	// Request-derived text is HTML-escaped on the wire; the JSON value is unchanged.
	assert.Contains(t, out, `\u003c`)
}

func TestRenderPreservesNumbers(t *testing.T) {
	out := renderBody(t, `{"n":1e3,"big":12345678901234567890,"f":0.10}`, nil, testVars())
	dec := json.NewDecoder(bytes.NewReader([]byte(out)))
	dec.UseNumber()
	var got map[string]json.Number
	require.NoError(t, dec.Decode(&got))
	assert.Equal(t, json.Number("1e3"), got["n"])
	assert.Equal(t, json.Number("12345678901234567890"), got["big"])
	assert.Equal(t, json.Number("0.10"), got["f"])
}

func TestRenderHeaderCRLF(t *testing.T) {
	p, err := Parse(nil, map[string]string{"X": "{{body.s}}"}, nil)
	require.NoError(t, err)
	v := testVars()
	v.Body = []byte(`{"s":"a\r\nb"}`)
	_, _, err = p.Render(v)
	require.Error(t, err)
}

func TestRenderEmptyBody(t *testing.T) {
	for _, b := range []json.RawMessage{nil, json.RawMessage("")} {
		p, err := Parse(b, map[string]string{"X": "{{uuid}}"}, nil)
		require.NoError(t, err)
		body, h, err := p.Render(testVars())
		require.NoError(t, err)
		assert.Nil(t, body)
		assert.Equal(t, testUUID, h["X"])
	}
}

func TestBodyPointers(t *testing.T) {
	p, err := Parse(json.RawMessage(`{"a":"{{body.x}}","l":[{"b":"p{{body.y}}"}],"c":"{{uuid}}","a/b":"{{body.z}}","m~n":"{{body.z}}"}`), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"/a", "/a~1b", "/m~0n"}, p.BodyPointers())
	assert.Equal(t, []string{"/l/0/b"}, p.TextPointers(nil))
}

func TestTextPointers(t *testing.T) {
	p, err := Parse(json.RawMessage(`{"a":"{{body.x}}","b":"p{{body.y}}","c":"{{path.id}}","d":"{{path.org}}/{{uuid}}","e":"{{uuid}}"}`), nil, []string{"id", "org"})
	require.NoError(t, err)
	assert.Equal(t, []string{"/a"}, p.BodyPointers())
	assert.Equal(t, []string{"/b"}, p.TextPointers(nil))
	assert.Equal(t, []string{"/b", "/d"}, p.TextPointers([]string{"org"}))
	assert.Equal(t, []string{"/b", "/c", "/d"}, p.TextPointers([]string{"id", "org"}))
}

func TestPathNames(t *testing.T) {
	assert.Equal(t, []string{"id"}, PathNames("/api/v2/users/{id}/roles"))
}

func TestPathValues(t *testing.T) {
	assert.Equal(t, map[string]string{"id": "auth0|123"}, PathValues("/api/v2/users/{id}", "/api/v2/users/auth0%7C123"))
	assert.Equal(t, map[string]string{"id": "a/b"}, PathValues("/api/v2/users/{id}", "/api/v2/users/a%2Fb"))
	assert.Nil(t, PathValues("/api/v2/users/{id}", "/api/v2/users"))
}

func TestBodyMultipath(t *testing.T) {
	v := testVars()
	v.Body = []byte(`{"email":"a@x.io","user_id":"u1","other":1}`)
	// Whole string: the projected object keeps its JSON type.
	assert.JSONEq(t, `{"x":{"email":"a@x.io","user_id":"u1"}}`, renderBody(t, `{"x":"{{body.{email,user_id}}}"}`, nil, v))
	assert.JSONEq(t, `{"x":{"email":"a@x.io","user_id":"u1"}}`, renderBody(t, `{"x":"{{ body.{email,user_id} }}"}`, nil, v))
	// Embedded: the projection is spliced in as text.
	assert.JSONEq(t, `{"x":"got {\"email\":\"a@x.io\",\"user_id\":\"u1\"}!"}`, renderBody(t, `{"x":"got {{body.{email,user_id}}}!"}`, nil, v))
	// Braces inside a quoted alias, or escaped, do not count.
	assert.JSONEq(t, `{"x":{"a}":"a@x.io"}}`, renderBody(t, `{"x":"{{body.{\"a}\":email}}}"}`, nil, v))
	v.Body = []byte(`{"a}b":1}`)
	assert.JSONEq(t, `{"x":1}`, renderBody(t, `{"x":"{{body.a\\}b}}"}`, nil, v))

	_, err := Parse(json.RawMessage(`{"x":"{{body.{email}"}`), nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unclosed placeholder")

	p, err := Parse(json.RawMessage(`{"x":"{{body.{email,user_id}}}"}`), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"/x"}, p.BodyPointers())
}
