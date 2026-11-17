// The VCS state of a project root (contracts/vocabulary.yaml, `vcs_state`), as
// `jhon init --json` reports it and `jhon doctor` warns about. The detection
// itself is unit-tested in internal/vcs. Every tree is built in a temporary
// directory outside any repository.
package ldd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func TestVCSPrecondition(t *testing.T) {
	t.Parallel()
	r := support.Run(t, support.Cmd{Args: []string{"git", "rev-parse", "--is-inside-work-tree"}, Dir: temp(t), Env: support.Env()})
	if strings.TrimSpace(r.Stdout) == "true" {
		t.Fatal("tmpdir is inside a git work tree; these cases cannot run here")
	}
}

// ── jhon init --json ────────────────────────────────────────────────────────

func TestVCSInitJSON(t *testing.T) {
	t.Parallel()
	states := vocab(t, "vcs_state")
	pkg := map[string]string{"package.json": "{}"}
	for _, c := range []struct{ root, want string }{
		{tree(t, pkg), states[0]},
		{support.GitInit(t, tree(t, pkg)), states[1]},
		{support.GitRepo(t, tree(t, pkg)), states[2]},
	} {
		before := snapshot(t, c.root, false)
		r := jhonGit(t, c.root, "init", c.root, "--json")
		equal(t, r.Code, 0, r.Stderr)
		raw := field(t, []byte(r.Stdout), "vcs")
		equal(t, keys(t, raw), []string{"state", "reason"})
		var vcs struct{ State, Reason string }
		if err := json.Unmarshal(raw, &vcs); err != nil {
			t.Fatal(err)
		}
		equal(t, vcs.State, c.want, vcs.Reason)
		equal(t, snapshot(t, c.root, false), before, c.root+" changed")
	}
}

func TestVCSInitHumanReport(t *testing.T) {
	t.Parallel()
	root := support.GitInit(t, tree(t, map[string]string{"package.json": "{}"}))
	r := jhonGit(t, root, "init", root)
	match(t, r.Stdout, `(?m)^vcs\s+`+vocab(t, "vcs_state")[1]+`\b`)
}

// ── the doctor report ───────────────────────────────────────────────────────
// `jhon doctor` checks the framework install, so a project's VCS state is a
// warning there, never a failure; outside a project it says nothing.

// doctor reads the lines of `jhon doctor`'s project section, under a scratch
// HOME whose `claude` fails.
func doctor(t *testing.T, dir string) [][2]string {
	t.Helper()
	home := temp(t)
	support.Write(t, home, map[string]string{"bin/claude": "#!/bin/sh\nexit 1\n"})
	if err := os.Chmod(filepath.Join(home, "bin", "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := support.Jhon(t, dir, support.Env("HOME="+home, "PATH="+filepath.Join(home, "bin")+":"+os.Getenv("PATH")), "doctor")
	match(t, r.Stdout, `\n(healthy|\d+ problem\(s\))\n$`, r.Stderr)
	section := ""
	for _, s := range strings.Split(r.Stdout, "\n\n") {
		if strings.HasPrefix(s, "project\n") {
			section = s
			break
		}
	}
	var out [][2]string
	lines := strings.Split(section, "\n")
	if len(lines) > 0 {
		lines = lines[1:]
	}
	line := regexp.MustCompile(`^ {2}(ok|warn) +(.*)$`)
	for _, l := range lines {
		if l == "" {
			continue
		}
		m := line.FindStringSubmatch(l)
		if m == nil {
			t.Fatalf("not a check line: %q", l)
		}
		out = append(out, [2]string{m[1], m[2]})
	}
	return out
}

func TestVCSDoctor(t *testing.T) {
	t.Parallel()
	states := vocab(t, "vcs_state")
	ok := doctor(t, support.GitRepo(t, tree(t, map[string]string{"package.json": "{}"})))
	if len(ok) != 1 {
		t.Fatalf("want one project line, got %q", ok)
	}
	equal(t, ok[0][0], "ok")
	match(t, ok[0][1], states[2])
	for _, c := range []struct{ root, want string }{
		{support.GitInit(t, tree(t, nil)), states[1]},
		{tree(t, map[string]string{"pyproject.toml": ""}), states[0]},
		{tree(t, map[string]string{"openspec/config.yaml": ""}), states[0]},
	} {
		lines := doctor(t, c.root)
		if len(lines) != 1 {
			t.Fatalf("want one project line, got %q", lines)
		}
		equal(t, lines[0][0], "warn")
		match(t, lines[0][1], `\b`+c.want+`\b`)
		match(t, lines[0][1], `consent`)
	}
}

func TestVCSDoctorOutsideProject(t *testing.T) {
	t.Parallel()
	equal(t, len(doctor(t, tree(t, map[string]string{"notes.txt": "x"}))), 0)
}
