// Package version exposes build-time metadata baked into the binary at link
// time via `-ldflags="-X ..."`. The release pipeline (.goreleaser.yaml) and
// `make build` both populate these from goreleaser's template context and
// `git describe`/`git rev-parse` respectively; a bare `go build ./...` keeps
// the defaults below so the binary always reports something.
package version

import "fmt"

var (
	// Version is the human-readable release version (e.g. "v1.2.3" from a
	// goreleaser build, "v1.2.3-4-gabc1234-dirty" from `make build`,
	// "dev" when neither is in effect).
	Version = "dev"

	// Commit is the short git SHA the binary was built from.
	Commit = "none"

	// Date is the RFC3339 UTC timestamp of the build.
	Date = "unknown"
)

// String renders the metadata in the conventional
// "<name> <version> (<commit>, <date>)" shape used by `auth0-mock -version`
// and the startup log line.
func String() string {
	return fmt.Sprintf("auth0-mock %s (%s, %s)", Version, Commit, Date)
}
