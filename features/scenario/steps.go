package scenario

import (
	"fmt"
	"strings"

	"github.com/cucumber/godog"
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

	sc.Step(`^I register "([^"]+)" with body:$`, func(target string, body *godog.DocString) error {
		parts := strings.SplitN(target, " ", 2)
		if len(parts) != 2 {
			return fmt.Errorf("expected 'METHOD /path', got %q", target)
		}
		c.Do(parts[0], parts[1], body.Content, false)
		if c.LastResp.StatusCode >= 400 {
			return fmt.Errorf("registration failed: %d %s", c.LastResp.StatusCode, string(c.LastBody))
		}
		return nil
	})

	sc.Step(`^I send "([^"]+)" with a valid bearer$`, func(target string) error {
		parts := strings.SplitN(target, " ", 2)
		if len(parts) != 2 {
			return fmt.Errorf("expected 'METHOD /path', got %q", target)
		}
		c.Do(parts[0], parts[1], "", true)
		return nil
	})

	sc.Step(`^I send "([^"]+)" without a bearer$`, func(target string) error {
		parts := strings.SplitN(target, " ", 2)
		if len(parts) != 2 {
			return fmt.Errorf("expected 'METHOD /path', got %q", target)
		}
		c.Do(parts[0], parts[1], "", false)
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

	sc.Step(`^I post to "([^"]+)" with body:$`, func(path string, body *godog.DocString) error {
		c.Do("POST", path, body.Content, false)
		return nil
	})
}
