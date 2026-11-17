// The owner's mode contract by place (T59, L462, L464): one table of what
// each class of call gets, by place, mode and caller, through the call the
// Claude Code plugin makes (`jhon hook pre-tool --agent '*' <guards>`, the
// guards a writer agent's hooks list). Today's behaviour is the contract:
//
//   - asks pass in yolo but destroy, and every ask passes in super-yolo;
//   - an unreadable or unknown command passes only in super-yolo;
//   - settings and git-config are asked in every mode;
//   - deny-level destroy, credentials, framework, `.git` writes, and an
//     agent's own `jhon grant` / `jhon mode` are refused in every mode;
//   - LDD's delivery refusal holds in every mode, and an agent's `jhon ldd`
//     (the retired switch) is refused like `jhon mode`;
//   - a repository without LDD has the modes of an LDD one, but its
//     delivery, a commit or a PR, and every write of a file in it outside
//     .git and the layout's directory (its first code edit) are refused in
//     every mode until LDD is set up (T72, L557, T89, L646); a folder outside any repository is no project (T57a):
//     `jhon mode` refuses there and its calls are judged as normal mode;
//   - nobody can be asked from a sub-agent or a top level in
//     bypassPermissions: what the top level would be asked is refused there,
//     and what the mode passes passes for them too.
//
// pi's path, the shell's judge(), runs the same table in
// shell/test/mode-matrix.test.ts.

package guards

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// matrixPlaces are the three places a session works in.
var matrixPlaces = []string{"ldd", "repo", "folder"}

// matrixCallers: the top level where the owner can be asked, the top level
// in bypassPermissions, and a sub-agent.
var matrixCallers = []string{"top-default", "top-bypass", "subagent"}

// matrixRow is one class of call and what today's guards make of it.
//   - ask: a class asked of the owner, passed by the modes in passes
//     (grant.ModeCovers);
//   - refuse: refused in every mode, for every caller;
//   - delivery: route-guard's refusal in the LDD place and in a repository
//     without LDD; in a folder allowed, the safety guard asking nothing of a
//     PR's creation;
//   - commit: route-guard's refusal in a repository without LDD (T72);
//     elsewhere allowed;
//   - code: an edit of the project's code, route-guard's refusal in a
//     repository without LDD (T89); allowed in the LDD place; in a folder
//     an outside-write ask, as any write there.
//
// inRepo marks a row that writes a file in the repository: in a repository
// without LDD route-guard refuses it before anything is asked (T89).
type matrixRow struct {
	name, kind, cls string
	passes          []string
	inRepo          bool
	call            func(f matrixFixture) (tool string, input map[string]any)
}

func bashRow(command string) func(matrixFixture) (string, map[string]any) {
	return func(matrixFixture) (string, map[string]any) { return "Bash", cmd(command) }
}

func writeRow(path func(f matrixFixture) string) func(matrixFixture) (string, map[string]any) {
	return func(f matrixFixture) (string, map[string]any) {
		return "Write", map[string]any{"file_path": path(f), "content": "x"}
	}
}

// askSiteCommand is the command askSites reaches safety-guard's cls with.
func askSiteCommand(cls string) string {
	for _, s := range askSites {
		if s.guard == "safety-guard" && s.cls == cls {
			return s.label
		}
	}
	panic("no safety-guard ask site for " + cls)
}

var (
	yoloUp      = []string{"yolo", "super-yolo"}
	superOnly   = []string{"super-yolo"}
	matrixTable = []matrixRow{
		{name: "destroy-ask", kind: "ask", cls: "destroy", passes: superOnly, call: bashRow(askSiteCommand("destroy"))},
		// From TestModeEveryModeStillRefuses' every-mode-refused set.
		{name: "destroy-deny-force-push", kind: "refuse", call: bashRow("git push --force")},
		{name: "destroy-deny-clean-fdx", kind: "refuse", call: bashRow("git clean -fdx")},
		{name: "outside-write-tool", kind: "ask", cls: "outside-write", passes: yoloUp, call: writeRow(func(matrixFixture) string { return "/etc/jhon.conf" })},
		{name: "outside-write-bash", kind: "ask", cls: "outside-write", passes: yoloUp, call: bashRow(askSiteCommand("outside-write"))},
		{name: "push", kind: "ask", cls: "push", passes: yoloUp, call: bashRow(askSiteCommand("push"))},
		{name: "gh-write", kind: "ask", cls: "gh-write", passes: yoloUp, call: bashRow(askSiteCommand("gh-write"))},
		{name: "remote", kind: "ask", cls: "remote", passes: yoloUp, call: bashRow(askSiteCommand("remote"))},
		{name: "publish", kind: "ask", cls: "publish", passes: yoloUp, call: bashRow(askSiteCommand("publish"))},
		{name: "deploy", kind: "ask", cls: "deploy", passes: yoloUp, call: bashRow(askSiteCommand("deploy"))},
		{name: "credentials-read", kind: "refuse", call: func(f matrixFixture) (string, map[string]any) {
			return "Read", map[string]any{"file_path": filepath.Join(f.dir, ".env")}
		}},
		{name: "credentials-bash", kind: "refuse", call: bashRow("cat .env")},
		{name: "settings-write", kind: "ask", cls: "settings", inRepo: true, call: writeRow(func(f matrixFixture) string { return filepath.Join(f.dir, ".claude", "settings.json") })},
		{name: "git-config", kind: "ask", cls: "git-config", call: bashRow(askSiteCommand("git-config"))},
		{name: "framework-write", kind: "refuse", call: writeRow(func(f matrixFixture) string { return filepath.Join(f.fw, "orchestrator.md") })},
		{name: "dot-git-write", kind: "refuse", call: writeRow(func(f matrixFixture) string { return filepath.Join(f.dir, ".git", "config") })},
		{name: "agent-jhon-grant", kind: "refuse", call: bashRow("jhon grant push")},
		{name: "agent-jhon-mode", kind: "refuse", call: bashRow("jhon mode yolo")},
		{name: "agent-jhon-ldd-off", kind: "refuse", call: bashRow("jhon ldd off")},
		{name: "agent-jhon-ldd-on", kind: "refuse", call: bashRow("jhon ldd on")},
		{name: "unreadable", kind: "ask", cls: "unreadable", passes: superOnly, call: bashRow(askSiteCommand("unreadable"))},
		{name: "unknown", kind: "ask", cls: "unknown", passes: superOnly, call: bashRow(askSiteCommand("unknown"))},
		{name: "delivery-pr-without-red-green", kind: "delivery", call: bashRow("gh pr create --title x --body y")},
		{name: "delivery-commit", kind: "commit", call: bashRow("git commit -q -m x")},
		{name: "code-write", kind: "code", cls: "outside-write", passes: yoloUp, call: writeRow(func(f matrixFixture) string { return filepath.Join(f.dir, "src", "app.py") })},
		{name: "code-edit", kind: "code", cls: "outside-write", passes: yoloUp, call: func(f matrixFixture) (string, map[string]any) {
			return "Edit", map[string]any{"file_path": filepath.Join(f.dir, "src", "app.py"), "old_string": "2", "new_string": "3"}
		}},
		{name: "code-bash-write", kind: "code", cls: "outside-write", passes: yoloUp, call: bashRow("printf 'X = 3\\n' > src/app.py")},
	}
)

// matrixFixture is one place under its own state, with the owner's mode set
// (or, outside a repository, refused).
type matrixFixture struct {
	place, dir, fw string
	env            []string
}

// newMatrixFixture builds place: an LDD repository (openspec/config.yaml,
// a behaviour change with no RED before GREEN), the same repository
// without LDD, or a folder in no repository.
func newMatrixFixture(t *testing.T, place string) matrixFixture {
	t.Helper()
	scratch := realTemp(t)
	f := matrixFixture{place: place, dir: filepath.Join(scratch, "proj"), fw: mkdir(t, filepath.Join(scratch, "fw"))}
	state := filepath.Join(scratch, "state")
	tmp := mkdir(t, filepath.Join(scratch, "tmpdir"))
	// JHON_ROOT, as newRG: route-guard reads the tree's prose.
	f.env = support.Env("HOME="+sgHome, "XDG_STATE_HOME="+state, "TMPDIR="+tmp, "JHON_ROOT="+support.Root())
	if place == "folder" {
		f.dir = filepath.Join(outsideTemp(t), "proj")
		mkdir(t, f.dir)
		support.Write(t, f.dir, map[string]string{"src/app.py": "X = 1\n"})
		return f
	}
	files := map[string]string{"src/app.py": "X = 1\n"}
	if place == "ldd" {
		files["openspec/config.yaml"] = ""
	}
	mkdir(t, f.dir)
	support.Write(t, f.dir, files)
	support.GitRepo(t, f.dir)
	support.Git(t, f.dir, "add", "-A")
	support.Git(t, f.dir, "commit", "-q", "-m", "base")
	// A change that alters behaviour, its record written and no RED before GREEN run.
	change := map[string]string{"src/app.py": "X = 2\n", "tests/test_app.py": "def test_x(): pass\n"}
	if place == "ldd" {
		change["openspec/changes/c1/tasks.md"] = "## Intent\nDelivers: X is 2\n"
	}
	support.Write(t, f.dir, change)
	return f
}

// outsideTemp is a new directory by its real path, removed when t ends,
// in no git repository and under neither /tmp nor $TMPDIR: the guards take
// any write under those for scratch (project.TmpRoots), so a folder there is
// no folder of the matrix. The test's own TMPDIR is often under /tmp, so the
// directory goes in the user's cache directory, else HOME (L664).
func outsideTemp(t *testing.T) string {
	t.Helper()
	var bases []string
	if d, err := os.UserCacheDir(); err == nil {
		bases = append(bases, d)
	}
	if d, err := os.UserHomeDir(); err == nil {
		bases = append(bases, d)
	}
	tmps := []string{"/tmp", os.TempDir()}
	for _, base := range bases {
		real, err := filepath.EvalSymlinks(base)
		if err != nil || underAny(real, tmps) {
			continue
		}
		d, err := os.MkdirTemp(real, "jhon-guards-folder-")
		if err != nil {
			continue
		}
		t.Cleanup(func() { os.RemoveAll(d) })
		git := exec.Command("git", "-C", d, "rev-parse", "--show-toplevel")
		git.Env = support.Env()
		if out, err := git.CombinedOutput(); err == nil {
			t.Fatalf("%s is in the git repository %s: no place for a folder in no repository", d, strings.TrimSpace(string(out)))
		}
		return d
	}
	t.Fatalf("no writable directory outside /tmp and TMPDIR (%s) among %q", os.TempDir(), bases)
	return ""
}

// underAny is whether p is one of roots, or under one, by real path.
func underAny(p string, roots []string) bool {
	for _, r := range roots {
		if real, err := filepath.EvalSymlinks(r); err == nil {
			r = real
		}
		if rel, err := filepath.Rel(r, p); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
			return true
		}
	}
	return false
}

// preTool is the call the Claude Code plugin makes for a writer agent's
// tool call, its guards scoped to every agent.
func (f matrixFixture) preTool(t *testing.T, tool string, input map[string]any, caller string) support.Result {
	t.Helper()
	payload := map[string]any{"hook_event_name": "PreToolUse", "tool_name": tool, "tool_input": input, "cwd": f.dir, "session_id": "s", "permission_mode": "default"}
	switch caller {
	case "top-bypass":
		payload["permission_mode"] = "bypassPermissions"
	case "subagent":
		payload["permission_mode"] = "bypassPermissions"
		payload["agent_id"], payload["agent_type"] = "a1", "worker"
	}
	guards := append(append([]string{"--agent", "*", "safety-guard", "vcs-guard"}, vcsDirs(t)...), "framework-guard", f.fw, "secrets-guard", "route-guard")
	return hook(t, f.env, "pre-tool", guards, payload)
}

// matrixWant is a cell's expected outcome: "pass" (allowed with the mode's
// note), "allow" (with no decision), "ask", or "refuse".
func matrixWant(row matrixRow, place, mode, caller string) string {
	if place == "folder" {
		mode = "normal"
	}
	switch {
	case row.kind == "refuse", row.kind == "delivery" && place != "folder", (row.kind == "commit" || row.kind == "code" || row.inRepo) && place == "repo":
		return "refuse"
	case row.kind == "delivery", row.kind == "commit", row.kind == "code" && place == "ldd":
		return "allow"
	}
	switch {
	case slices.Contains(row.passes, mode):
		return "pass"
	case caller == "top-default":
		return "ask"
	default:
		return "refuse"
	}
}

func TestModeMatrixClaudeCodeHook(t *testing.T) {
	t.Parallel()
	for _, place := range matrixPlaces {
		for _, mode := range []string{"normal", "yolo", "super-yolo"} {
			t.Run(place+"/"+mode, func(t *testing.T) {
				t.Parallel()
				f := newMatrixFixture(t, place)
				r := support.Jhon(t, f.dir, f.env, "mode", mode)
				if place == "folder" {
					// A folder in no repository is no project: it takes no mode (T57a).
					code(t, r, 2, "jhon mode "+mode+" in a folder")
					if !strings.Contains(r.Stderr, "not a git repository") {
						t.Errorf("jhon mode %s in a folder: %q", mode, r.Stderr)
					}
				} else {
					code(t, r, 0, "jhon mode "+mode)
				}
				for _, row := range matrixTable {
					for _, caller := range matrixCallers {
						cell := place + "/" + mode + "/" + row.name + "/" + caller
						t.Run(row.name+"/"+caller, func(t *testing.T) {
							tool, input := row.call(f)
							checkMatrixCell(t, cell, f.preTool(t, tool, input, caller), matrixWant(row, place, mode, caller), row, mode)
						})
					}
				}
			})
		}
	}
}

func checkMatrixCell(t *testing.T, cell string, r support.Result, want string, row matrixRow, mode string) {
	t.Helper()
	switch want {
	case "pass":
		code(t, r, 0, cell)
		matches(t, modeNote(t, r.Stdout), `allowed by the owner's `+regexp.QuoteMeta(mode)+` mode: `+regexp.QuoteMeta(row.cls)+`: `, cell)
	case "allow":
		code(t, r, 0, cell)
		noDecision(t, r.Stdout, cell)
	case "ask":
		code(t, r, 0, cell)
		matches(t, askedWhy(t, r.Stdout, cell), `(^|\n)`+regexp.QuoteMeta(row.cls)+`: `, cell)
	case "refuse":
		code(t, r, 2, cell)
		noDecision(t, r.Stdout, cell)
		if (row.kind == "delivery" || row.kind == "commit" || row.kind == "code" && strings.HasPrefix(cell, "repo/")) && !strings.Contains(r.Stderr, "route-guard: ") {
			t.Errorf("%s: not route-guard's refusal: %q", cell, r.Stderr)
		}
	}
}
