package ldd

import (
	"path/filepath"
	"strings"
	"testing"
)

// The record the route text names (S28): below high risk a one-line
// tasks.md, and a spec delta when a scenario changes; at high risk today's
// wording, a spec delta and tasks per slice.
func TestRouteTextNamesTheRecordByRisk(t *testing.T) {
	standard := Route{Level: RouteStandard, Tier: Medium, Behaviour: true, Reasons: []string{"risk medium is under the verified tier (high)"}}
	if got, want := standard.String(), "standard: risk medium is under the verified tier (high); its affected tests deliver it; it alters behaviour, so its record goes with it: its RED and GREEN runs in `jhon executions`, tasks.md only when the work cannot be resumed from the diff (over the advisory lines of code, risk high, or more than one component; below high risk a one-line `## Intent` and `Delivers:` is enough), and a spec delta only when a main spec changes or is added"; got != want {
		t.Errorf("standard route:\n got %q\nwant %q", got, want)
	}
	verified := Route{Level: RouteVerified, Tier: High, Behaviour: true, Reasons: []string{"risk high reaches the verified tier (high)"}}
	if got, want := verified.String(), "verified: risk high reaches the verified tier (high); an independent verification before delivery, no proposal or design; it alters behaviour, so its record goes with it: its RED and GREEN runs in `jhon executions`, tasks per slice (risk high), and a spec delta only when a main spec changes or is added"; got != want {
		t.Errorf("verified route:\n got %q\nwant %q", got, want)
	}
}

// configuredRepo is a repository whose main branch holds
// openspec/config.yaml as config, with a feature branch whose change adds
// files, ten lines each: the config is the project's, not the change's.
func configuredRepo(t *testing.T, config string, files ...string) string {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	gitT(t, root, "init", "-q")
	write(t, filepath.Join(root, "src/base.py"), "X = 1\n")
	write(t, filepath.Join(root, "openspec/config.yaml"), config)
	gitT(t, root, "add", "-A")
	gitT(t, root, "commit", "-q", "-m", "base")
	gitT(t, root, "checkout", "-q", "-b", "feature")
	for _, f := range files {
		write(t, filepath.Join(root, f), strings.Repeat("x = 1\n", 10))
	}
	return root
}

func TestProjectBriefNamesTheRecordByRisk(t *testing.T) {
	jhonRoot(t)
	method := func(brief string) string {
		_, after, _ := strings.Cut(brief, " The method is LDD (contracts/ldd.md): ")
		before, _, _ := strings.Cut(after, "; the route sets only the rigor")
		return before
	}
	medium := configuredRepo(t, "delivery: change\n", "src/app.py", "tests/test_app.py")
	if got, want := method(ProjectBrief(medium)), "every change that alters behaviour carries its record (its RED and GREEN runs in `jhon executions`; a small change (`jhon status` says so) keeps no tasks.md, archive, light verify or todo list; else tasks.md only when the work cannot be resumed from the diff, below high risk a one-line `## Intent` and `Delivers:`; a spec delta only when a main spec changes or is added), and a PR without it is refused"; got != want {
		t.Errorf("medium:\n got %q\nwant %q", got, want)
	}
	high := configuredRepo(t, "delivery: change\n", "src/auth/login.py", "tests/test_login.py")
	if got, want := method(ProjectBrief(high)), "every change that alters behaviour carries its record, its RED and GREEN runs in `jhon executions`, tasks per slice, and a spec delta only when a main spec changes or is added, and a PR without it is refused"; got != want {
		t.Errorf("high:\n got %q\nwant %q", got, want)
	}
}
