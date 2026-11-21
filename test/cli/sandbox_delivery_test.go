// T181 (L1500-L1503): git over SSH and gh run outside the sandbox on a line
// of their own, which may open with `cd <folder> &&` into the project and
// name gh's {owner} and {repo}; the delivery profile, which had no caller,
// is gone. pi asks `jhon sandbox profile` for each line, so these are its
// answers; neither question starts bubblewrap.
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func TestProfileTakesALeadingCdIntoTheProjectAndGhsPlaceholders(t *testing.T) {
	t.Parallel()
	m := newMachine(t)
	for line, want := range map[string]string{
		"cd " + m.proj + " && git push -u origin x":                        "unsandboxed",
		"cd " + filepath.Join(m.proj, "src") + " && git fetch origin main": "unsandboxed",
		"cd src && git fetch origin main":                                  "unsandboxed",
		"gh api repos/{owner}/{repo}/commits/main --jq .sha":               "unsandboxed",
		"cd " + m.plain + " && git push -u origin x":                       "work",
		"cd /tmp && git fetch":                                             "work",
		"cd ~ && git fetch":                                                "work",
		"cd ../plain && git fetch":                                         "work",
		"cd " + m.proj + " && git fetch && rm -rf x":                       "work",
		"cd " + m.proj + " && git fetch -q origin main && echo behind":     "work",
		"gh api {a,b}": "work",
	} {
		r := support.Jhon(t, m.proj, m.env, "sandbox", "profile", "--project", m.proj, "--", line)
		if r.Code != 0 || r.Stdout != want+"\n" || r.Stderr != "" {
			t.Errorf("profile %q: exit %d, stdout %q, stderr %q; want 0 and %s", line, r.Code, r.Stdout, r.Stderr, want)
		}
	}
}

// `jhon sandbox run --profile delivery` is an unknown profile: a usage
// error, and nothing runs.
func TestRunRefusesTheDeliveryProfile(t *testing.T) {
	t.Parallel()
	m := newMachine(t)
	marker := filepath.Join(m.proj, "ran.txt")
	for _, args := range [][]string{{"--profile", "delivery"}, {"--profile=delivery"}} {
		r := support.Jhon(t, m.proj, m.env, append(append([]string{"sandbox", "run"}, args...), "--", "touch", marker)...)
		_, err := os.Lstat(marker)
		ran := err == nil
		want := "jhon sandbox: unknown profile: delivery (accepted: work)\n\nusage: jhon sandbox run [--profile work] [--project <folder>] -- <program> [args...]\n"
		if r.Code != 2 || r.Stdout != "" || !strings.HasPrefix(r.Stderr, want) || ran {
			t.Errorf("%q: exit %d, stdout %q, stderr %q, ran %v; want 2 and %q, nothing run", args, r.Code, r.Stdout, r.Stderr, ran, want)
		}
	}
}
