// Package version carries the server binary build metadata. The build scripts
// and the Dockerfile inject all three via -ldflags -X, so a running server can
// report the exact version, git commit, and build time it was produced from
// (`metaxisdata --version` and `GET /api/version`). A plain `go build` leaves
// the defaults below.
package version

var (
	// Version is the release version, "dev" for a local build.
	Version = "dev"
	// GitCommit is the commit the binary was built from, "unknown" outside a
	// build that passes it.
	GitCommit = "unknown"
	// BuildTime is the UTC build timestamp in RFC 3339 form.
	BuildTime = "unknown"
)
