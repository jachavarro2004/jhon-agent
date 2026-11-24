package integrations

import (
	"regexp"
	"strings"
)

// A cli integration's program releases on its own schedule (T242,
// odd/tasks/studio-integration-v1.md S5). Schema v1 has no field for where
// its releases are and who signs them, and adding one needs v2
// (integrations/README.md, "Schema versions"); so jhon reads them from the
// fields v1 already has, by one convention the engine holds
// (internal/update, tool.go): a cli integration whose doctor.source is
// github.com/<owner>/<repo> publishes its releases there, as
// <doctor.binary>-<version>.tar.gz with its .sha256 and .sigstore.json,
// signed by that repository's .github/workflows/release.yml at the
// release's tag. jhon-studio's release workflow is that.

// releaseName is a GitHub owner, repository, or a program's name: a leading letter
// or digit, then letters, digits, '.', '_' or '-' (never "." or "..").
var releaseName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// ReleaseRepo is the GitHub repository m's program is released from: a cli
// integration naming a doctor.binary whose doctor.source is
// github.com/<owner>/<repo>; false for any other (an MCP server, a module
// path such as github.com/<owner>/<repo>/v3, an npm package).
func (m Manifest) ReleaseRepo() (owner, repo string, ok bool) {
	if m.Kind != "cli" || !releaseName.MatchString(m.Doctor.Binary) {
		return "", "", false
	}
	rest, found := strings.CutPrefix(m.Doctor.Source, "github.com/")
	owner, repo, two := strings.Cut(rest, "/")
	if !found || !two || !releaseName.MatchString(owner) || !releaseName.MatchString(repo) {
		return "", "", false
	}
	return owner, repo, true
}
