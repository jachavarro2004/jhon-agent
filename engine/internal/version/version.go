// Package version is the version of Jhon this binary was built as, set at
// build time with -ldflags -X (LDFlags) and read by `jhon version`, and by
// `jhon update` to compare against the latest release.
//
// A plain `go build` or `go test` leaves them unset: Version 0.0.0-dev,
// Commit and Date empty. `jhon sync` stamps 0.0.0-dev+<short commit>; the
// release CI stamps the tag (0.1.0). Both pass LDFlags's string.
package version

import (
	"strings"

	"golang.org/x/mod/semver"
)

// Set by LDFlags at build time.
var (
	Version = "0.0.0-dev"
	Commit  = ""
	Date    = ""
)

// Package is this package's import path, the -X flags' prefix.
const Package = "github.com/jachavarro2004/jhon-agent/engine/internal/version"

// LDFlags is the -ldflags value that stamps a build: version, the full
// commit, and the commit's ISO 8601 date (never the build time, so a build
// is reproducible). For 0.1.0 it is
//
//	-X github.com/jachavarro2004/jhon-agent/engine/internal/version.Version=0.1.0 -X github.com/jachavarro2004/jhon-agent/engine/internal/version.Commit=<commit> -X github.com/jachavarro2004/jhon-agent/engine/internal/version.Date=<date>
func LDFlags(version, commit, date string) string {
	return strings.Join([]string{
		"-X " + Package + ".Version=" + version,
		"-X " + Package + ".Commit=" + commit,
		"-X " + Package + ".Date=" + date,
	}, " ")
}

// String is `<version> (<commit[:12]>, <date>)`, each of commit and date
// left out when empty: `0.0.0-dev` unstamped.
func String() string {
	var meta []string
	if Commit != "" {
		meta = append(meta, Commit[:min(12, len(Commit))])
	}
	if Date != "" {
		meta = append(meta, Date)
	}
	if len(meta) == 0 {
		return Version
	}
	return Version + " (" + strings.Join(meta, ", ") + ")"
}

// IsDev is whether v is a development build's version: 0.0.0-dev unstamped,
// 0.0.0-dev+<short commit> from jhon sync. `jhon update` installs nothing
// over one, and a tool's minimum jhon (requires_jhon) is not checked
// against one (integrations.Judge).
func IsDev(v string) bool { return semver.Prerelease("v"+v) == "-dev" }
