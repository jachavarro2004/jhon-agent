// Jhon always works under LDD (T72, L554, L555, L557, T89, L646): in a
// repository with no layout a commit or a PR, and the first code edit, are
// refused by route-guard until LDD is set up, naming the init steps by their
// absolute path (L534); writes that set LDD up and read-only work are not.
// Once a layout's config exists, delivery follows LDD's rules and edits pass.

package guards

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// noLayoutRepo is a git repository with code and no openspec/ or ldd/ config.
func noLayoutRepo(t *testing.T) (repo string, env []string) {
	t.Helper()
	scratch := realTemp(t)
	repo = filepath.Join(scratch, "proj")
	support.Write(t, repo, map[string]string{"src/app.py": "X = 1\n"})
	support.GitRepo(t, repo)
	support.Git(t, repo, "add", "-A")
	support.Git(t, repo, "commit", "-q", "-m", "base")
	support.Write(t, repo, map[string]string{"src/app.py": "X = 2\n"})
	env = support.Env("HOME=/home/jhon-route-test", "XDG_STATE_HOME="+filepath.Join(scratch, "state"), "JHON_ROOT="+support.Root())
	return repo, env
}

func routeCall(t *testing.T, env []string, repo, tool string, input map[string]any) support.Result {
	t.Helper()
	payload := map[string]any{"hook_event_name": "PreToolUse", "tool_name": tool, "tool_input": input, "cwd": repo, "session_id": "s", "permission_mode": "default"}
	return hook(t, env, "route-guard", nil, payload)
}

func TestNoLayoutRefusesDeliveryUntilLddIsSetUp(t *testing.T) {
	t.Parallel()
	repo, env := noLayoutRepo(t)
	steps := filepath.Join(support.Root(), "prose", "skills", "ldd", "references", "init.md")
	for _, c := range []string{"git commit -q -m x", "git -C " + repo + " commit -am x", "gh pr create --title x --body y"} {
		r := routeCall(t, env, repo, "Bash", cmd(c))
		code(t, r, 2, c)
		if !strings.Contains(r.Stderr, "route-guard: LDD is not set up in this project") || !strings.Contains(r.Stderr, "`"+steps+"`") {
			t.Errorf("%s: %q", c, r.Stderr)
		}
	}
	// Reads and other commands are never refused.
	for _, c := range []struct {
		tool  string
		input map[string]any
	}{
		{"Read", map[string]any{"file_path": filepath.Join(repo, "src", "app.py")}},
		{"Bash", cmd("git status")},
		{"Bash", cmd("git diff")},
		{"Bash", cmd("python3 -m pytest")},
	} {
		r := routeCall(t, env, repo, c.tool, c.input)
		code(t, r, 0, c.tool)
		if r.Stderr != "" {
			t.Errorf("%s: %q", c.tool, r.Stderr)
		}
	}
	// Set up (ldd/config.yaml written): a commit follows LDD's rules, which let it pass.
	support.Write(t, repo, map[string]string{"ldd/config.yaml": "", "ldd/specs/.keep": "", "ldd/changes/archive/.keep": ""})
	code(t, routeCall(t, env, repo, "Bash", cmd("git commit -q -m x")), 0, "a commit once LDD is set up")
	// A folder in no repository is no project: nothing to deliver, nothing refused.
	folder := filepath.Join(realTemp(t), "folder")
	support.Write(t, folder, map[string]string{"a.py": "x\n"})
	code(t, routeCall(t, env, folder, "Bash", cmd("git commit -m x")), 0, "a folder")
}

// switchRecord writes the record an old `jhon ldd off` left for root.
func switchRecord(t *testing.T, state, root string) {
	t.Helper()
	sum := sha256.Sum256([]byte(root))
	b, _ := json.Marshal(map[string]string{"ldd": "off", "root": root, "since": "2026-10-01T00:00:00Z"})
	support.Write(t, filepath.Join(state, "jhon", "ldd-switch"), map[string]string{hex.EncodeToString(sum[:]) + ".json": string(b)})
}

// An old switch-off record has no effect: status says on, the LDD rules
// and route-guard bind; `jhon ldd` is no command any more.
func TestLddAlwaysOnIgnoresAnOldSwitchOff(t *testing.T) {
	t.Parallel()
	scratch := realTemp(t)
	state := filepath.Join(scratch, "state")
	env := support.Env("HOME="+sgHome, "XDG_STATE_HOME="+state, "TMPDIR="+mkdir(t, filepath.Join(scratch, "tmp")), "JHON_ROOT="+support.Root())
	repo := mkdir(t, filepath.Join(scratch, "repo"))
	support.Write(t, repo, map[string]string{"openspec/config.yaml": "delivery: change\n", "src/app.py": "X = 1\n"})
	support.GitRepo(t, repo)
	support.Git(t, repo, "add", "-A")
	support.Git(t, repo, "commit", "-q", "-m", "base")
	real, _ := filepath.EvalSymlinks(repo)
	switchRecord(t, state, real)

	r := support.Jhon(t, repo, env, "status", "--json")
	var s map[string]any
	if err := json.Unmarshal([]byte(r.Stdout), &s); err != nil {
		t.Fatalf("status --json: %v\n%s", err, r.Stdout)
	}
	if got, _ := s["ldd"].(map[string]any); got["state"] != "on" || got["source"] != "default" || got["active"] != true || len(got) != 3 {
		t.Errorf("status ldd: %v", got)
	}
	matches(t, support.Jhon(t, repo, env, "status").Stdout, `(?m)^ldd         on  \(default; active\)$`, "status")
	b := support.Hook(t, env, "session-brief", map[string]any{"cwd": repo}, "jhon-orchestrator")
	const notice = "LDD is always on with Jhon now; the old switch-off no longer applies."
	if !strings.Contains(b.Stdout, "This project's rules") || strings.Count(b.Stdout, notice) != 1 {
		t.Errorf("session-brief: %s", b.Stdout)
	}
	// Told once: the next session is not told again.
	if b := support.Hook(t, env, "session-brief", map[string]any{"cwd": repo}, "jhon-orchestrator"); strings.Contains(b.Stdout, notice) || !strings.Contains(b.Stdout, "This project's rules") {
		t.Errorf("session-brief, again: %s", b.Stdout)
	}
	support.Write(t, repo, map[string]string{"src/app.py": "X = 2\n", "tests/test_app.py": "def test_x(): pass\n", "openspec/changes/c1/tasks.md": "## Intent\nDelivers: X is 2\n"})
	code(t, routeCall(t, env, repo, "Bash", cmd("gh pr create --title x --body y")), 2, "a PR without RED before GREEN")

	r = support.Jhon(t, repo, env, "ldd", "off")
	if r.Code == 0 || !strings.Contains(r.Stderr, "unknown command: ldd") {
		t.Errorf("jhon ldd off: exit %d, %q", r.Code, r.Stderr)
	}
	if _, err := os.Stat(filepath.Join(state, "jhon", "ldd-switch", "history.jsonl")); err == nil {
		t.Errorf("jhon ldd off wrote a history")
	}

	// No agent writes jhon's state, by a tool or by the shell, and an
	// agent's `jhon ldd on|off` is still refused like `jhon mode`.
	file := filepath.Join(state, "jhon", "ldd-switch", "k.json")
	for _, c := range []map[string]any{
		{"tool_name": "Write", "tool_input": map[string]any{"file_path": file, "content": "{}"}},
		{"tool_name": "Bash", "tool_input": cmd("echo '{}' > " + file)},
		{"tool_name": "Bash", "tool_input": cmd("bash -c 'jhon ldd off'")},
	} {
		c["hook_event_name"], c["cwd"], c["session_id"], c["permission_mode"] = "PreToolUse", repo, "s", "default"
		code(t, hook(t, env, "pre-tool", []string{"--agent", "*", "safety-guard", "vcs-guard", ".git"}, c), 2, "an agent's write into jhon's state")
	}
}

// The independent verification's probes (L561, L562): every spelling of a
// delivery the LDD branch refuses is refused before set-up too, and a write
// or a command that only names a commit is not.
func TestNoLayoutDeliverySpellings(t *testing.T) {
	t.Parallel()
	repo, env := noLayoutRepo(t)
	for _, c := range []string{"bash -c 'git commit -m x'", "(cd src && git commit -m x)", "env A=1 git commit -m x", "command git commit -m x", "GIT_AUTHOR_NAME=a git commit -m x", "/usr/bin/git commit -m x", "gh  pr   create"} {
		code(t, routeCall(t, env, repo, "Bash", cmd(c)), 2, c)
	}
	for _, c := range []struct {
		tool  string
		input map[string]any
	}{
		{"Bash", cmd("git stash")},
		{"Bash", cmd("git log -1")},
		{"Write", map[string]any{"file_path": filepath.Join(repo, "ldd", "notes.md"), "content": "run git commit, then gh pr create\n"}},
	} {
		code(t, routeCall(t, env, repo, c.tool, c.input), 0, c.tool)
	}
}

// The first code edit in a repository with no layout is refused, naming the
// init steps (T89, L646), by every tool that writes a file and by a Bash
// command the safety guard reads as writing one; pi's edit and write reach
// the hook as Edit and Write (shell/lib/guards.ts claudeCall). Writes that
// set LDD up, read-only work, and every edit once the config exists pass.
func TestNoLayoutRefusesTheFirstCodeEdit(t *testing.T) {
	t.Parallel()
	repo, env := noLayoutRepo(t)
	steps := filepath.Join(support.Root(), "prose", "skills", "ldd", "references", "init.md")
	want := "route-guard: refused: LDD is not set up in this project yet; set it up first by following `" + steps +
		"` (ask how to deliver and test with your ask tool, or use the recommended values), then make this edit and, before you finish, start its change (tasks.md) and record a run (`jhon executions run`)\n"
	src := filepath.Join(repo, "src", "x.py")
	type call struct {
		label, tool string
		input       map[string]any
		pi          bool
	}
	edits := []call{
		{"Write src/x.py", "Write", map[string]any{"file_path": src, "content": "X = 3\n"}, false},
		{"Write relative", "Write", map[string]any{"file_path": "src/x.py", "content": "X = 3\n"}, false},
		{"Edit src/app.py", "Edit", map[string]any{"file_path": filepath.Join(repo, "src", "app.py"), "old_string": "2", "new_string": "3"}, false},
		{"NotebookEdit", "NotebookEdit", map[string]any{"notebook_path": filepath.Join(repo, "n.ipynb"), "new_source": "x"}, false},
		{"README.md", "Write", map[string]any{"file_path": filepath.Join(repo, "README.md"), "content": "x\n"}, false},
		{"pi edit", "Edit", map[string]any{"file_path": src, "edits": []any{map[string]any{"old_string": "a", "new_string": "b"}}}, true},
		{"pi write", "Write", map[string]any{"file_path": src, "content": "X = 3\n"}, true},
		{"cat > src/x.py", "Bash", cmd("cat > src/x.py <<'EOF'\nX = 3\nEOF"), false},
		{"sed -i", "Bash", cmd("sed -i 's/2/3/' src/app.py"), false},
		{"pi bash tee", "Bash", cmd("echo x | tee src/x.py"), true},
	}
	for _, c := range edits {
		r := preToolCall(t, env, repo, c.tool, c.input, c.pi)
		code(t, r, 2, c.label)
		if r.Stderr != want {
			t.Errorf("%s: stderr %q", c.label, r.Stderr)
		}
	}
	// Setting LDD up, reading, and writing outside the repository pass.
	allowed := []call{
		{"jhon init", "Bash", cmd("jhon init " + repo + " --json"), false},
		{"mkdir ldd", "Bash", cmd("mkdir -p ldd/specs ldd/changes/archive"), false},
		{"Write ldd/config.yaml", "Write", map[string]any{"file_path": filepath.Join(repo, "ldd", "config.yaml"), "content": "context: x\n"}, false},
		{"pi write ldd/config.yaml", "Write", map[string]any{"file_path": filepath.Join(repo, "ldd", "config.yaml"), "content": "context: x\n"}, true},
		{"bash ldd/config.yaml", "Bash", cmd("printf 'context: x\\n' > ldd/config.yaml"), false},
		{"Write openspec/config.yaml", "Write", map[string]any{"file_path": filepath.Join(repo, "openspec", "config.yaml"), "content": ""}, false},
		{"Read", "Read", map[string]any{"file_path": src}, false},
		{"Grep", "Grep", map[string]any{"pattern": "X", "path": repo}, false},
		{"Glob", "Glob", map[string]any{"pattern": "**/*.py", "path": repo}, false},
		{"git status", "Bash", cmd("git status"), false},
		{"tests to /dev/null", "Bash", cmd("python3 -m pytest > /dev/null"), false},
		{"home redirect", "Bash", cmd("echo x > ~/.cache/jhon-probe.txt"), false},
		{"home log", "Bash", cmd("python3 -m pytest > ~/out.log"), false},
	}
	for _, c := range allowed {
		r := preToolCall(t, env, repo, c.tool, c.input, c.pi)
		code(t, r, 0, c.label)
		if strings.Contains(r.Stderr, "route-guard") || strings.Contains(r.Stdout, "route-guard") {
			t.Errorf("%s: %q %q", c.label, r.Stdout, r.Stderr)
		}
	}
	// A folder in no repository is no project: its edits are not refused.
	folder := filepath.Join(realTemp(t), "folder")
	support.Write(t, folder, map[string]string{"a.py": "x\n"})
	code(t, routeCall(t, env, folder, "Write", map[string]any{"file_path": filepath.Join(folder, "a.py"), "content": "y\n"}), 0, "a folder")
	// Once ldd/config.yaml exists, the same edits pass, by route-guard alone.
	support.Write(t, repo, map[string]string{"ldd/config.yaml": "context: x\n", "ldd/specs/.keep": "", "ldd/changes/archive/.keep": ""})
	for _, c := range edits {
		r := routeCall(t, env, repo, c.tool, c.input)
		code(t, r, 0, c.label+" once set up")
		if r.Stderr != "" || r.Stdout != "" {
			t.Errorf("%s once set up: %q %q", c.label, r.Stdout, r.Stderr)
		}
	}
}

// preToolCall is the call Claude Code's hooks make for a top-level tool
// call, or, with pi, the one the shell's judge() makes for a sub-agent's
// (agent_id pi-child, no permission_mode), the guards scoped to every agent.
func preToolCall(t *testing.T, env []string, repo, tool string, input map[string]any, pi bool) support.Result {
	t.Helper()
	payload := map[string]any{"hook_event_name": "PreToolUse", "tool_name": tool, "tool_input": input, "cwd": repo, "session_id": "s", "permission_mode": "default"}
	if pi {
		delete(payload, "permission_mode")
		payload["agent_id"] = "pi-child"
	}
	guards := append(append([]string{"--agent", "*", "safety-guard", "vcs-guard"}, vcsDirs(t)...), "framework-guard", mkdir(t, filepath.Join(realTemp(t), "fw")), "secrets-guard", "route-guard")
	return hook(t, env, "pre-tool", guards, payload)
}

// An OpenSpec repository's edits and calls are judged as before T89 (L503).
func TestOpenSpecEditsUnchanged(t *testing.T) {
	t.Parallel()
	scratch := realTemp(t)
	repo := filepath.Join(scratch, "proj")
	support.Write(t, repo, map[string]string{"openspec/config.yaml": "", "src/app.py": "X = 1\n"})
	support.GitRepo(t, repo)
	env := support.Env("HOME=/home/jhon-route-test", "XDG_STATE_HOME="+filepath.Join(scratch, "state"), "JHON_ROOT="+support.Root())
	for _, c := range []struct {
		tool  string
		input map[string]any
	}{
		{"Write", map[string]any{"file_path": filepath.Join(repo, "src", "x.py"), "content": "X = 3\n"}},
		{"Edit", map[string]any{"file_path": filepath.Join(repo, "src", "app.py"), "old_string": "1", "new_string": "3"}},
		{"Bash", cmd("sed -i 's/1/3/' src/app.py")},
	} {
		r := routeCall(t, env, repo, c.tool, c.input)
		code(t, r, 0, c.tool)
		if r.Stdout != "" || r.Stderr != "" {
			t.Errorf("%s: %q %q", c.tool, r.Stdout, r.Stderr)
		}
	}
}
