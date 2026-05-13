package scenario

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/cucumber/godog"
	"github.com/tidwall/gjson"
)

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

	// Registration that's expected to succeed (4xx fails the step).
	sc.Step(`^I register "([^"]+)" with body:$`, func(target string, body *godog.DocString) error {
		method, path, err := splitTarget(target)
		if err != nil {
			return err
		}
		c.Do(method, path, body.Content, false)
		if c.LastResp.StatusCode >= 400 {
			return fmt.Errorf("registration failed: %d %s", c.LastResp.StatusCode, string(c.LastBody))
		}
		return nil
	})

	// Registration that's expected to fail (we want to assert the 4xx).
	sc.Step(`^I attempt to register "([^"]+)" with body:$`, func(target string, body *godog.DocString) error {
		method, path, err := splitTarget(target)
		if err != nil {
			return err
		}
		c.Do(method, path, body.Content, false)
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

	sc.Step(`^I reset "([^"]+)"$`, func(target string) error {
		method, path, err := splitTarget(target)
		if err != nil {
			return err
		}
		c.Do(method, path, "", false)
		if c.LastResp.StatusCode != 204 {
			return fmt.Errorf("reset failed: %d %s", c.LastResp.StatusCode, string(c.LastBody))
		}
		return nil
	})

	sc.Step(`^I reset all matches$`, func() error {
		c.Do("POST", "/admin0/reset", "", false)
		if c.LastResp.StatusCode != 204 {
			return fmt.Errorf("/admin0/reset failed: %d %s", c.LastResp.StatusCode, string(c.LastBody))
		}
		return nil
	})

	sc.Step(`^I list registered matches$`, func() error {
		c.Do("GET", "/admin0/matches", "", false)
		return nil
	})

	sc.Step(`^the matches list has (\d+) entries$`, func(want int) error {
		got := int(gjson.GetBytes(c.LastBody, "matches.#").Int())
		if got != want {
			return fmt.Errorf("got %d entries, want %d (body=%s)", got, want, string(c.LastBody))
		}
		return nil
	})

	sc.Step(`^the matches list contains "([^"]+)"$`, func(target string) error {
		method, path, err := splitTarget(target)
		if err != nil {
			return err
		}
		found := false
		gjson.GetBytes(c.LastBody, "matches").ForEach(func(_, v gjson.Result) bool {
			if v.Get("method").String() == method && v.Get("path").String() == path {
				found = true
				return false
			}
			return true
		})
		if !found {
			return fmt.Errorf("matches list does not contain %s %s (body=%s)", method, path, string(c.LastBody))
		}
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
			return fmt.Errorf("Location %q does not contain %q", loc, needle)
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
}

// splitTarget parses a "METHOD /path" string into its parts.
func splitTarget(target string) (method, path string, err error) {
	parts := strings.SplitN(target, " ", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("expected 'METHOD /path', got %q", target)
	}
	return parts[0], parts[1], nil
}
