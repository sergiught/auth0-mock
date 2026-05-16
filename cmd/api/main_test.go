//go:build features

package main

import (
	"os"
	"testing"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"

	"github.com/sergiught/auth0-mock/features/scenario"
)

func TestFeatures(t *testing.T) {
	// Honour GODOG_FORMAT when set so CI can emit a junit report alongside
	// the human-readable "pretty" output without changing local behaviour.
	// Format syntax: "pretty,junit:path.xml" (comma-separated; the junit
	// emitter writes to the named file at suite-end).
	format := "pretty"
	if v := os.Getenv("GODOG_FORMAT"); v != "" {
		format = v
	}
	suite := godog.TestSuite{
		Name: "auth0-mock",
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			c := scenario.New(t, sc)
			scenario.RegisterSteps(sc, c)
		},
		Options: &godog.Options{
			Format:   format,
			Paths:    []string{"../../features"},
			Output:   colors.Colored(os.Stdout),
			TestingT: t,
		},
	}
	if status := suite.Run(); status != 0 {
		t.Fatalf("godog returned status %d", status)
	}
}
