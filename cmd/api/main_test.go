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
	suite := godog.TestSuite{
		Name: "auth0-mock",
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			c := scenario.New(t, sc)
			scenario.RegisterSteps(sc, c)
		},
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"../../features"},
			Output:   colors.Colored(os.Stdout),
			TestingT: t,
		},
	}
	if status := suite.Run(); status != 0 {
		t.Fatalf("godog returned status %d", status)
	}
}
