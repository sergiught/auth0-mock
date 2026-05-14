package scenario

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/cucumber/godog"
	"github.com/kinbiko/jsonassert"
	"github.com/tidwall/gjson"
)

// jsonAssertPrinter satisfies jsonassert.Printer by buffering Errorf calls so
// the godog step can convert them into a returned error rather than failing
// the surrounding *testing.T directly.
type jsonAssertPrinter struct{ msgs []string }

func (p *jsonAssertPrinter) Errorf(format string, args ...any) {
	p.msgs = append(p.msgs, fmt.Sprintf(format, args...))
}

// RegisterSteps wires the .feature step phrases to Go functions.
func RegisterSteps(sc *godog.ScenarioContext, c *Context) {
	sc.Step(`^the mock is running$`, func() error { return nil })

	sc.Step(`^I have a valid bearer token$`, func() error {
		c.MintBearer()
		if c.BearerTok == "" {
			return fmt.Errorf("failed to mint bearer")
		}
		return nil
	})

	// Registration expected to succeed (4xx fails the step).
	sc.Step(`^I register an expectation for "([^"]+)" with response:$`, func(target string, body *godog.DocString) error {
		payload, err := expectationBody(target, body.Content)
		if err != nil {
			return err
		}
		c.Do("POST", "/admin0/expectations", payload, false)
		if c.LastResp.StatusCode >= 400 {
			return fmt.Errorf("registration failed: %d %s", c.LastResp.StatusCode, string(c.LastBody))
		}
		return nil
	})

	// Registration expected to fail (assert the 4xx afterwards).
	sc.Step(`^I attempt to register an expectation for "([^"]+)" with response:$`, func(target string, body *godog.DocString) error {
		payload, err := expectationBody(target, body.Content)
		if err != nil {
			return err
		}
		c.Do("POST", "/admin0/expectations", payload, false)
		return nil
	})

	sc.Step(`^I clear the expectation for "([^"]+)"$`, func(target string) error {
		method, path, err := splitTarget(target)
		if err != nil {
			return err
		}
		payload := fmt.Sprintf(`{"method":%q,"path":%q}`, method, path)
		c.Do("DELETE", "/admin0/expectations", payload, false)
		if c.LastResp.StatusCode != 204 {
			return fmt.Errorf("clear failed: %d %s", c.LastResp.StatusCode, string(c.LastBody))
		}
		return nil
	})

	sc.Step(`^I clear all expectations$`, func() error {
		c.Do("DELETE", "/admin0/expectations", "", false)
		if c.LastResp.StatusCode != 204 {
			return fmt.Errorf("clear all failed: %d %s", c.LastResp.StatusCode, string(c.LastBody))
		}
		return nil
	})

	sc.Step(`^I reset all mock state$`, func() error {
		c.Do("POST", "/admin0/reset", "", false)
		if c.LastResp.StatusCode != 204 {
			return fmt.Errorf("/admin0/reset failed: %d %s", c.LastResp.StatusCode, string(c.LastBody))
		}
		return nil
	})

	sc.Step(`^I list registered expectations$`, func() error {
		c.Do("GET", "/admin0/expectations", "", false)
		return nil
	})

	sc.Step(`^the expectations list has (\d+) entries$`, func(want int) error {
		got := int(gjson.GetBytes(c.LastBody, "expectations.#").Int())
		if got != want {
			return fmt.Errorf("got %d entries, want %d (body=%s)", got, want, string(c.LastBody))
		}
		return nil
	})

	sc.Step(`^the expectations list contains "([^"]+)"$`, func(target string) error {
		method, path, err := splitTarget(target)
		if err != nil {
			return err
		}
		found := false
		gjson.GetBytes(c.LastBody, "expectations").ForEach(func(_, v gjson.Result) bool {
			if v.Get("method").String() == method && v.Get("path").String() == path {
				found = true
				return false
			}
			return true
		})
		if !found {
			return fmt.Errorf("expectations list does not contain %s %s (body=%s)", method, path, string(c.LastBody))
		}
		return nil
	})

	sc.Step(`^I send "([^"]+)" with a valid bearer$`, func(target string) error {
		method, path, err := splitTarget(target)
		if err != nil {
			return err
		}
		c.Do(method, path, "", true)
		return nil
	})

	sc.Step(`^I send "([^"]+)" without a bearer$`, func(target string) error {
		method, path, err := splitTarget(target)
		if err != nil {
			return err
		}
		c.Do(method, path, "", false)
		return nil
	})

	sc.Step(`^I send "([^"]+)" with body and a valid bearer:$`, func(target string, body *godog.DocString) error {
		method, path, err := splitTarget(target)
		if err != nil {
			return err
		}
		c.Do(method, path, body.Content, true)
		return nil
	})

	// JSON-body POST (existing).
	sc.Step(`^I post to "([^"]+)" with body:$`, func(path string, body *godog.DocString) error {
		c.Do("POST", path, body.Content, false)
		return nil
	})

	// Form-encoded POST. The DocString is a sequence of "key=value" lines.
	sc.Step(`^I post to "([^"]+)" with form body:$`, func(path string, body *godog.DocString) error {
		form := url.Values{}
		for line := range strings.SplitSeq(strings.TrimSpace(body.Content), "\n") {
			kv := strings.SplitN(strings.TrimSpace(line), "=", 2)
			if len(kv) != 2 {
				continue
			}
			form.Set(strings.TrimSpace(kv[0]), strings.TrimSpace(kv[1]))
		}
		c.DoForm("POST", path, form, false)
		return nil
	})

	sc.Step(`^I receive a (\d+) response$`, func(want int) error {
		if c.LastResp.StatusCode != want {
			return fmt.Errorf("got %d, want %d (body=%s)", c.LastResp.StatusCode, want, string(c.LastBody))
		}
		return nil
	})

	sc.Step(`^the response body contains "([^"]+)"$`, func(needle string) error {
		if !strings.Contains(string(c.LastBody), needle) {
			return fmt.Errorf("body %s does not contain %q", string(c.LastBody), needle)
		}
		return nil
	})

	sc.Step(`^the response JSON path "([^"]+)" equals "([^"]*)"$`, func(path, want string) error {
		got := gjson.GetBytes(c.LastBody, path).String()
		if got != want {
			return fmt.Errorf("JSON path %s: got %q, want %q (body=%s)", path, got, want, string(c.LastBody))
		}
		return nil
	})

	sc.Step(`^the response JSON path "([^"]+)" exists$`, func(path string) error {
		if !gjson.GetBytes(c.LastBody, path).Exists() {
			return fmt.Errorf("JSON path %s does not exist (body=%s)", path, string(c.LastBody))
		}
		return nil
	})

	// Match the entire response body against a JSON shape. Use "<<PRESENCE>>"
	// for fields whose values are truly runtime-random (signed JWTs, UUIDs).
	// Use "${BASE_URL}" inside string values to refer to the live server's
	// base URL (e.g. "http://127.0.0.1:RANDOM_PORT") so URL-shaped fields can
	// be asserted concretely instead of with PRESENCE.
	// See https://github.com/kinbiko/jsonassert for the full DSL.
	sc.Step(`^the response body should match the JSON pattern:$`, func(pattern *godog.DocString) error {
		expanded := strings.ReplaceAll(pattern.Content, "${BASE_URL}", c.BaseURL)
		p := &jsonAssertPrinter{}
		jsonassert.New(p).Assertf(string(c.LastBody), "%s", expanded)
		if len(p.msgs) > 0 {
			return fmt.Errorf("JSON pattern mismatch:\n%s\n\nactual body:\n%s",
				strings.Join(p.msgs, "\n"), string(c.LastBody))
		}
		return nil
	})

	sc.Step(`^the response header "([^"]+)" equals "([^"]+)"$`, func(name, want string) error {
		got := c.LastResp.Header.Get(name)
		if got != want {
			return fmt.Errorf("header %s: got %q, want %q", name, got, want)
		}
		return nil
	})

	sc.Step(`^the response Location header contains "([^"]+)"$`, func(needle string) error {
		loc := c.LastResp.Header.Get("Location")
		if !strings.Contains(loc, needle) {
			return fmt.Errorf("location %q does not contain %q", loc, needle)
		}
		return nil
	})

	sc.Step(`^the access_token verifies against the published JWKS$`, func() error {
		tok := gjson.GetBytes(c.LastBody, "access_token").String()
		if tok == "" {
			return fmt.Errorf("no access_token in last response (body=%s)", string(c.LastBody))
		}
		return c.VerifyAccessTokenAgainstJWKS(tok)
	})

	sc.Step(`^I save the access_token as my bearer$`, func() error {
		tok := gjson.GetBytes(c.LastBody, "access_token").String()
		if tok == "" {
			return fmt.Errorf("no access_token in last response")
		}
		c.BearerTok = tok
		return nil
	})

	// JSON-body request with an explicit verb (covers PUT/DELETE for admin0).
	sc.Step(`^I (PUT|POST|DELETE|PATCH) "([^"]+)" with body:$`, func(method, path string, body *godog.DocString) error {
		c.Do(method, path, body.Content, false)
		return nil
	})

	// Bodyless request with an explicit verb (covers DELETE).
	sc.Step(`^I (PUT|DELETE) "([^"]+)"$`, func(method, path string) error {
		c.Do(method, path, "", false)
		return nil
	})

	// Decode the access_token from the last response and assert on its claims.
	sc.Step(`^the access_token claim "([^"]+)" equals "([^"]*)"$`, func(claim, want string) error {
		got, err := claimValueFromAccessToken(c.LastBody, claim)
		if err != nil {
			return err
		}
		if got.String() != want {
			return fmt.Errorf("claim %q: got %q, want %q", claim, got.String(), want)
		}
		return nil
	})

	// PKCE: capture the `code` from the last /authorize redirect and POST it
	// through /oauth/token with the supplied verifier. The challenge is the
	// S256 hash of the verifier.
	sc.Step(`^I start /authorize with code_verifier "([^"]+)"$`, func(verifier string) error {
		challenge := pkceS256Challenge(verifier)
		q := url.Values{}
		q.Set("client_id", "demo")
		q.Set("redirect_uri", "https://app/cb")
		q.Set("state", "s1")
		q.Set("response_type", "code")
		q.Set("code_challenge", challenge)
		q.Set("code_challenge_method", "S256")
		c.Do("GET", "/authorize?"+q.Encode(), "", false)
		return nil
	})

	sc.Step(`^I exchange the code with verifier "([^"]+)"$`, func(verifier string) error {
		code, err := codeFromLastLocation(c)
		if err != nil {
			return err
		}
		form := url.Values{}
		form.Set("grant_type", "authorization_code")
		form.Set("client_id", "demo")
		form.Set("code", code)
		form.Set("redirect_uri", "https://app/cb")
		form.Set("code_verifier", verifier)
		c.DoForm("POST", "/oauth/token", form, false)
		return nil
	})

	// MFA: capture the mfa_token from a 403 mfa_required response so a follow-up
	// step can present it on /oauth/token with one of the mfa-* grants.
	sc.Step(`^I save the mfa_token from the response$`, func() error {
		tok := gjson.GetBytes(c.LastBody, "mfa_token").String()
		if tok == "" {
			return fmt.Errorf("no mfa_token in response (body=%s)", string(c.LastBody))
		}
		c.MFAToken = tok
		return nil
	})

	sc.Step(`^I exchange the mfa_token with grant "([^"]+)" and form body:$`, func(grant string, body *godog.DocString) error {
		form := url.Values{}
		form.Set("grant_type", grant)
		form.Set("mfa_token", c.MFAToken)
		for line := range strings.SplitSeq(strings.TrimSpace(body.Content), "\n") {
			kv := strings.SplitN(strings.TrimSpace(line), "=", 2)
			if len(kv) != 2 {
				continue
			}
			form.Set(strings.TrimSpace(kv[0]), strings.TrimSpace(kv[1]))
		}
		c.DoForm("POST", "/oauth/token", form, false)
		return nil
	})

	sc.Step(`^I exchange the code without a verifier$`, func() error {
		code, err := codeFromLastLocation(c)
		if err != nil {
			return err
		}
		form := url.Values{}
		form.Set("grant_type", "authorization_code")
		form.Set("client_id", "demo")
		form.Set("code", code)
		form.Set("redirect_uri", "https://app/cb")
		c.DoForm("POST", "/oauth/token", form, false)
		return nil
	})

	sc.Step(`^the access_token claim "([^"]+)" array contains "([^"]+)"$`, func(claim, item string) error {
		got, err := claimValueFromAccessToken(c.LastBody, claim)
		if err != nil {
			return err
		}
		if !got.IsArray() {
			return fmt.Errorf("claim %q is not an array (got %s)", claim, got.Raw)
		}
		for _, v := range got.Array() {
			if v.String() == item {
				return nil
			}
		}
		return fmt.Errorf("claim %q array does not contain %q (got %s)", claim, item, got.Raw)
	})
}

// codeFromLastLocation extracts the `code` query parameter from the Location
// header of the last response. Used to wire /authorize → /oauth/token in
// godog scenarios.
func codeFromLastLocation(c *Context) (string, error) {
	loc := c.LastResp.Header.Get("Location")
	if loc == "" {
		return "", fmt.Errorf("no Location header on last response")
	}
	u, err := url.Parse(loc)
	if err != nil {
		return "", fmt.Errorf("parse Location: %w", err)
	}
	code := u.Query().Get("code")
	if code == "" {
		return "", fmt.Errorf("location %q has no code param", loc)
	}
	return code, nil
}

// pkceS256Challenge returns the base64url(sha256(verifier)) S256 challenge for
// the given verifier — useful for scripting PKCE scenarios.
func pkceS256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// claimValueFromAccessToken decodes the JWT payload from the access_token in
// the given JSON response body and returns the gjson Result for the named
// claim path.
func claimValueFromAccessToken(body []byte, claim string) (gjson.Result, error) {
	tok := gjson.GetBytes(body, "access_token").String()
	if tok == "" {
		return gjson.Result{}, fmt.Errorf("no access_token in response")
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return gjson.Result{}, fmt.Errorf("access_token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return gjson.Result{}, fmt.Errorf("decode payload: %w", err)
	}
	return gjson.GetBytes(payload, claim), nil
}

// splitTarget parses a "METHOD /path" string into its parts.
func splitTarget(target string) (method, path string, err error) {
	parts := strings.SplitN(target, " ", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("expected 'METHOD /path', got %q", target)
	}
	return parts[0], parts[1], nil
}

// expectationBody merges a "METHOD /path" target into a response docstring
// ({status, headers?, body?}) to form a POST /admin0/expectations payload.
func expectationBody(target, responseJSON string) (string, error) {
	method, path, err := splitTarget(target)
	if err != nil {
		return "", err
	}
	var resp map[string]any
	if strings.TrimSpace(responseJSON) != "" {
		if err := json.Unmarshal([]byte(responseJSON), &resp); err != nil {
			return "", fmt.Errorf("response json: %w", err)
		}
	} else {
		resp = map[string]any{}
	}
	resp["method"] = method
	resp["path"] = path
	out, err := json.Marshal(resp)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
