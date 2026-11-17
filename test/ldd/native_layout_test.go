package ldd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// The native layout (T57c): ldd/ in place of openspec/, read the same way,
// held to the same delivery refusal; openspec/ comes first when both are.
func TestNativeLayout(t *testing.T) {
	root := support.GitRepo(t, temp(t))
	env := support.Env("XDG_STATE_HOME=" + temp(t))
	support.Write(t, root, map[string]string{
		"ldd/config.yaml":         "delivery: slice\n",
		"ldd/changes/c1/tasks.md": "## Intent\nDelivers: X is 2\n",
		"src/app.py":              "X = 1\n",
	})
	commit(t, root, "base")
	support.Write(t, root, map[string]string{"src/app.py": "X = 2\n"})

	r := support.Jhon(t, root, env, "status", "c1", "--json")
	var s struct {
		Directory string `json:"directory"`
		Ldd       struct {
			Active bool `json:"active"`
		} `json:"ldd"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &s); err != nil {
		t.Fatalf("status: %v\n%s%s", err, r.Stdout, r.Stderr)
	}
	if s.Directory != "ldd/changes/c1" || !s.Ldd.Active {
		t.Fatalf("status: %+v", s)
	}
	pr := map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": map[string]any{"command": "gh pr create --title x --body y"}, "cwd": root, "session_id": "s"}
	h := support.Hook(t, env, "route-guard", pr)
	if h.Code != 2 || !strings.Contains(h.Stderr, "route-guard: ") {
		t.Fatalf("a native LDD repository's PR without RED→GREEN: exit %d %s", h.Code, h.Stderr)
	}
	// The refusal names the native layout's paths, never openspec's (L479).
	if strings.Contains(h.Stderr, "openspec") {
		t.Errorf("a native repository's refusal names openspec: %s", h.Stderr)
	}
	// A record missing: its refusal names ldd/changes/<change>/.
	other := support.GitRepo(t, temp(t))
	support.Write(t, other, map[string]string{"ldd/config.yaml": "delivery: slice\n", "src/app.py": "X = 1\n"})
	commit(t, other, "base")
	support.Write(t, other, map[string]string{"src/app.py": "X = 2\n"})
	pr["cwd"] = other
	m := support.Hook(t, env, "route-guard", pr)
	if m.Code != 2 || !strings.Contains(m.Stderr, "under ldd/changes/<change>/") || strings.Contains(m.Stderr, "openspec") {
		t.Errorf("a native repository's missing-record refusal: exit %d %s", m.Code, m.Stderr)
	}
	pr["cwd"] = root
	// The brief names the layout's own config (L477, A5).
	b := support.Hook(t, env, "session-brief", map[string]any{"cwd": root}, "jhon-orchestrator")
	if !strings.Contains(b.Stdout, "from ldd/config.yaml") || strings.Contains(b.Stdout, "openspec") {
		t.Errorf("a native project's brief: %s", b.Stdout)
	}
}

// A native ldd/ above the repository is never the repository's (L477, B1):
// a folder holding ldd/config.yaml, or only ldd/changes/, around a plain
// git repository lends it no change and no LDD.
func TestNativeLayoutNeverAboveTheRepository(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"config and changes": {"ldd/config.yaml": "delivery: slice\n", "ldd/changes/c1/tasks.md": "## Intent\nDelivers: X\n"},
		"changes only":       {"ldd/changes/c1/tasks.md": "## Intent\nDelivers: X\n"},
	} {
		t.Run(name, func(t *testing.T) {
			parent := temp(t)
			support.Write(t, parent, files)
			inner := filepath.Join(parent, "inner")
			if err := os.MkdirAll(inner, 0o755); err != nil {
				t.Fatal(err)
			}
			support.GitRepo(t, inner)
			env := support.Env("XDG_STATE_HOME=" + temp(t))
			r := support.Jhon(t, inner, env, "status", "--json")
			var s struct {
				Change *string `json:"change"`
				Ldd    struct {
					Active bool `json:"active"`
				} `json:"ldd"`
				Vcs struct {
					State string `json:"state"`
				} `json:"vcs"`
			}
			if err := json.Unmarshal([]byte(r.Stdout), &s); err != nil {
				t.Fatalf("status: %v\n%s%s", err, r.Stdout, r.Stderr)
			}
			if s.Change != nil || s.Ldd.Active || s.Vcs.State != "ready" {
				t.Errorf("status in the inner repository: change %v, active %v, vcs %q", s.Change, s.Ldd.Active, s.Vcs.State)
			}
			if r := support.Jhon(t, inner, env, "executions", "run", "c1", "--dry-run"); r.Code != 2 {
				t.Errorf("executions run c1 --dry-run: exit %d, want 2\n%s%s", r.Code, r.Stdout, r.Stderr)
			}
		})
	}
}

// openspec/ above the repository is never the repository's either (L477,
// owner L786/L853): discovery stops at the git toplevel, so a plain inner
// repository sees no change and no LDD; a folder in no repository still
// finds the openspec/ above it, as before.
func TestOpenSpecAboveTheRepositoryIgnored(t *testing.T) {
	parent := temp(t)
	support.Write(t, parent, map[string]string{"openspec/config.yaml": "delivery: slice\n", "openspec/changes/c1/tasks.md": "## Intent\nDelivers: X\n", "loose/notes.txt": "x\n"})
	inner := filepath.Join(parent, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	support.GitRepo(t, inner)
	env := support.Env("XDG_STATE_HOME=" + temp(t))
	r := support.Jhon(t, inner, env, "status", "--json")
	var s struct {
		Change *string `json:"change"`
		Ldd    struct {
			Active bool `json:"active"`
		} `json:"ldd"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &s); err != nil {
		t.Fatalf("status: %v\n%s%s", err, r.Stdout, r.Stderr)
	}
	if s.Change != nil || s.Ldd.Active {
		t.Errorf("status in the inner repository under openspec/: change %+v, active %v\n%s", s.Change, s.Ldd.Active, r.Stdout)
	}
	if r := support.Jhon(t, inner, env, "executions", "run", "c1", "--dry-run"); r.Code != 2 {
		t.Errorf("executions run c1 --dry-run in the inner repository: exit %d, want 2\n%s%s", r.Code, r.Stdout, r.Stderr)
	}
	// A folder in no repository climbs to openspec/ as it always did.
	if l := support.Jhon(t, filepath.Join(parent, "loose"), env, "status", "--json"); !strings.Contains(l.Stdout, `"change": "c1"`) {
		t.Errorf("a folder in no repository under openspec/ no longer finds it: %s%s", l.Stdout, l.Stderr)
	}
}

// A native project's config is read for its projects as an OpenSpec one's
// is (T60): test_command_override in ldd/config.yaml makes the project
// testable with that command, in status and in an executions run.
func TestNativeLayoutReadsItsConfigOverrides(t *testing.T) {
	for _, dir := range []string{"openspec", "ldd"} {
		t.Run(dir, func(t *testing.T) {
			root := support.GitRepo(t, temp(t))
			support.Write(t, root, map[string]string{
				dir + "/config.yaml":         "strict_tdd_policy: require\ntest_command_override:\n  \".\": \"python3 -m unittest\"\n",
				dir + "/changes/c1/tasks.md": "## Intent\nDelivers: X\n",
				"pyproject.toml":             "[project]\nname = \"x\"\n",
				"app.py":                     "X = 1\n",
			})
			commit(t, root, "base")
			env := support.Env("XDG_STATE_HOME=" + temp(t))
			r := support.Jhon(t, root, env, "status", "c1", "--json")
			var s struct {
				Projects []struct {
					Path          string  `json:"path"`
					Testability   string  `json:"testability"`
					Command       *string `json:"command"`
					CommandSource *string `json:"command_source"`
				} `json:"projects"`
			}
			if err := json.Unmarshal([]byte(r.Stdout), &s); err != nil {
				t.Fatalf("status: %v\n%s%s", err, r.Stdout, r.Stderr)
			}
			if len(s.Projects) == 0 || s.Projects[0].Testability != "testable" || s.Projects[0].Command == nil || *s.Projects[0].Command != "python3 -m unittest" ||
				s.Projects[0].CommandSource == nil || *s.Projects[0].CommandSource != "override" {
				t.Errorf("projects: %s", r.Stdout)
			}
			d := support.Jhon(t, root, env, "executions", "run", "c1", "--dry-run")
			if d.Code != 0 || !strings.Contains(d.Stdout, "python3 -m unittest") {
				t.Errorf("executions run --dry-run: exit %d\n%s%s", d.Code, d.Stdout, d.Stderr)
			}
		})
	}
}
