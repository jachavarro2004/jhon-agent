// A folder that is no project (no git repository) is judged as normal mode,
// and the owner's yes to a write there lasts the session (T57a, L460:
// "Preguntar y recordar"): `jhon grant outside-write --session <id>` in
// the folder lets that session's writes under it pass, and nothing else.

package guards

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// outsideTmp is a fresh directory outside /tmp and $TMPDIR, where a write
// is no scratch write: under the user's cache directory. The test is
// skipped where that is under /tmp too.
func outsideTmp(t *testing.T) string {
	t.Helper()
	base, err := os.UserCacheDir()
	if err != nil || strings.HasPrefix(base, "/tmp") {
		t.Skip("no directory outside /tmp to hold a folder")
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	d, err := os.MkdirTemp(base, "jhon-session-grant-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	real, err := filepath.EvalSymlinks(d)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

type sessionFixture struct {
	folder, other, fw string
	env               []string
}

func newSessionFixture(t *testing.T) sessionFixture {
	t.Helper()
	base := outsideTmp(t)
	scratch := realTemp(t)
	f := sessionFixture{folder: mkdir(t, filepath.Join(base, "docs")), other: mkdir(t, filepath.Join(base, "elsewhere")), fw: mkdir(t, filepath.Join(scratch, "fw"))}
	support.Write(t, f.folder, map[string]string{"notes.txt": "x\n", ".env": "TOKEN=x\n"})
	f.env = support.Env("HOME="+sgHome, "XDG_STATE_HOME="+filepath.Join(scratch, "state"), "TMPDIR="+mkdir(t, filepath.Join(scratch, "tmp")), "JHON_ROOT="+support.Root())
	return f
}

// call is the Claude Code plugin's pre-tool call, top level, where the owner can be asked.
func (f sessionFixture) call(t *testing.T, cwd, session, tool string, input map[string]any) support.Result {
	t.Helper()
	payload := map[string]any{"hook_event_name": "PreToolUse", "tool_name": tool, "tool_input": input, "cwd": cwd, "session_id": session, "permission_mode": "default"}
	guards := append(append([]string{"--agent", "*", "safety-guard", "vcs-guard"}, vcsDirs(t)...), "framework-guard", f.fw, "secrets-guard", "route-guard")
	return hook(t, f.env, "pre-tool", guards, payload)
}

func (f sessionFixture) write(t *testing.T, cwd, session, path string) support.Result {
	return f.call(t, cwd, session, "Write", map[string]any{"file_path": path, "content": "x"})
}

func wantAsked(t *testing.T, r support.Result, cls, label string) {
	t.Helper()
	code(t, r, 0, label)
	matches(t, askedWhy(t, r.Stdout, label), `(^|\n)`+cls+`: `, label)
}

func wantAllowed(t *testing.T, r support.Result, label string) {
	t.Helper()
	code(t, r, 0, label)
	noDecision(t, r.Stdout, label)
}

func TestSessionGrantFolder(t *testing.T) {
	t.Parallel()
	f := newSessionFixture(t)
	in := filepath.Join(f.folder, "sub", "a.txt")

	// The first write asks, as normal mode does; no mode is set there.
	wantAsked(t, f.write(t, f.folder, "s1", in), "outside-write", "before the grant")
	r := support.Jhon(t, f.folder, f.env, "mode", "yolo")
	code(t, r, 2, "jhon mode in a folder")
	matches(t, r.Stderr, `not a git repository`, "jhon mode in a folder")

	r = support.Jhon(t, f.folder, f.env, "grant", "outside-write", "--session", "s1")
	code(t, r, 0, "the owner's session grant")
	matches(t, r.Stdout, `granted outside-write for `+f.folder+` for session s1`, "grant")

	// The granted folder passes, by a tool and by the shell, below it too.
	wantAllowed(t, f.write(t, f.folder, "s1", in), "granted folder")
	wantAllowed(t, f.call(t, f.folder, "s1", "Bash", cmd("echo x > sub/b.txt")), "granted folder, bash")
	wantAllowed(t, f.write(t, filepath.Join(f.folder, "sub"), "s1", in), "granted folder from below")

	// Another folder, and another session, ask.
	wantAsked(t, f.write(t, f.folder, "s1", filepath.Join(f.other, "a.txt")), "outside-write", "another folder")
	wantAsked(t, f.write(t, f.other, "s1", filepath.Join(f.other, "a.txt")), "outside-write", "another folder as cwd")
	wantAsked(t, f.write(t, f.folder, "s2", in), "outside-write", "another session")
	wantAsked(t, f.write(t, f.folder, "", in), "outside-write", "no session")

	// What is refused, or asked as its own class, stays so.
	for _, c := range []struct {
		tool  string
		input map[string]any
	}{
		{"Write", map[string]any{"file_path": filepath.Join(f.folder, ".git", "config"), "content": "x"}},
		{"Read", map[string]any{"file_path": filepath.Join(f.folder, ".env")}},
		{"Bash", cmd("rm .env")},
		{"Bash", cmd("git clean -fdx")},
		{"Bash", cmd("git push --force")},
		{"Write", map[string]any{"file_path": filepath.Join(f.fw, "orchestrator.md"), "content": "x"}},
		{"Bash", cmd("jhon mode yolo")},
	} {
		r := f.call(t, f.folder, "s1", c.tool, c.input)
		code(t, r, 2, fmt.Sprintf("still refused: %s %v", c.tool, c.input))
	}
	wantAsked(t, f.write(t, f.folder, "s1", filepath.Join(f.folder, ".claude", "settings.json")), "settings", "settings")
	if r := f.call(t, f.folder, "s1", "Bash", cmd("rm notes.txt")); r.Code == 0 && !strings.Contains(r.Stdout, `"ask"`) {
		t.Errorf("a deletion passed by a write grant: %s", r.Stdout)
	}

	// The session's end revokes it.
	r = support.Jhon(t, f.folder, f.env, "grant", "--revoke", "outside-write", "--session", "s1")
	code(t, r, 0, "revoke")
	matches(t, r.Stdout, `revoked outside-write for `+f.folder, "revoke")
	wantAsked(t, f.write(t, f.folder, "s1", in), "outside-write", "after the session")
}

func TestSessionGrantNeverWidens(t *testing.T) {
	t.Parallel()
	f := newSessionFixture(t)
	home := mkdir(t, filepath.Join(filepath.Dir(f.folder), "home"))
	env := append(append([]string{}, f.env...), "HOME="+home)
	repo := realTemp(t)
	support.GitRepo(t, repo)
	for _, c := range []struct{ name, dir, why string }{
		{"HOME", home, "home directory or /"},
		{"/", "/", "home directory or /"},
		{"above HOME", filepath.Dir(home), "home directory or /"},
		{"a project", repo, "inside a project"},
	} {
		r := support.Jhon(t, f.folder, env, "grant", "outside-write", "--session", "s1", "--root", c.dir)
		code(t, r, 2, c.name)
		matches(t, r.Stderr, c.why, c.name)
	}
	for _, args := range [][]string{
		{"grant", "push", "--session", "s1"},
		{"grant", "outside-write", "push", "--session", "s1"},
		{"grant", "outside-write", "--session", "s1", "--for", "1d"},
	} {
		code(t, support.Jhon(t, f.folder, env, args...), 2, strings.Join(args, " "))
	}
}

func TestSessionGrantNotAnAgents(t *testing.T) {
	t.Parallel()
	f := newSessionFixture(t)
	state := ""
	for _, kv := range f.env {
		if v, ok := strings.CutPrefix(kv, "XDG_STATE_HOME="); ok {
			state = v
		}
	}
	for _, c := range []string{
		"jhon grant outside-write --session s1",
		"jhon grant outside-write --session s1 --root " + f.folder,
		"mkdir -p " + state + "/jhon/session-grants && echo '{}' > " + state + "/jhon/session-grants/k.json",
	} {
		for _, caller := range []map[string]any{{"permission_mode": "default"}, {"agent_id": "a1", "agent_type": "worker"}} {
			payload := map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": cmd(c), "cwd": f.folder, "session_id": "s1"}
			for k, v := range caller {
				payload[k] = v
			}
			r := hook(t, f.env, "pre-tool", []string{"--agent", "*", "safety-guard", "vcs-guard", ".git"}, payload)
			code(t, r, 2, c)
		}
	}
	// In a repository, in every mode, the agent's own session grant is refused too.
	repo := realTemp(t)
	support.GitRepo(t, repo)
	for _, mode := range []string{"normal", "yolo", "super-yolo"} {
		code(t, support.Jhon(t, repo, f.env, "mode", mode), 0, "jhon mode "+mode)
		for _, caller := range []map[string]any{{"permission_mode": "default"}, {"permission_mode": "bypassPermissions"}, {"agent_id": "a1", "agent_type": "worker"}} {
			payload := map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": cmd("jhon grant outside-write --session s1 --root " + f.folder), "cwd": repo, "session_id": "s1"}
			for k, v := range caller {
				payload[k] = v
			}
			code(t, hook(t, f.env, "pre-tool", []string{"--agent", "*", "safety-guard", "vcs-guard", ".git"}, payload), 2, mode+": an agent's session grant")
		}
	}
	r := f.write(t, f.folder, "s1", filepath.Join(state, "jhon", "session-grants", "k.json"))
	code(t, r, 2, "a write into the session grants")
	// None of it granted anything.
	wantAsked(t, f.write(t, f.folder, "s1", filepath.Join(f.folder, "a.txt")), "outside-write", "no grant made")
}

// grantFolder is f's folder granted to session s1 by the owner.
func (f sessionFixture) grantFolder(t *testing.T) {
	t.Helper()
	code(t, support.Jhon(t, f.folder, f.env, "grant", "outside-write", "--session", "s1"), 0, "grant")
}

func (f sessionFixture) state() string {
	for _, kv := range f.env {
		if v, ok := strings.CutPrefix(kv, "XDG_STATE_HOME="); ok {
			return v
		}
	}
	return ""
}

// A link inside the granted folder to one outside it lends the outside
// folder nothing: the write's resolved path decides. A shell write is the
// sandbox's (T128f).
func TestSessionGrantSymlinkOut(t *testing.T) {
	t.Parallel()
	f := newSessionFixture(t)
	if err := os.Symlink(f.other, filepath.Join(f.folder, "link")); err != nil {
		t.Fatal(err)
	}
	support.Write(t, f.folder, map[string]string{"a.txt": "a\n"})
	support.Write(t, f.other, map[string]string{"e.txt": "a\n"})
	f.grantFolder(t)
	wantAsked(t, f.write(t, f.folder, "s1", filepath.Join(f.folder, "link", "y.txt")), "outside-write", "Write through the link")
	wantAsked(t, f.call(t, f.folder, "s1", "Edit", map[string]any{"file_path": filepath.Join(f.folder, "link", "e.txt"), "old_string": "a", "new_string": "b"}), "outside-write", "Edit through the link")
	// The folder itself still passes.
	wantAllowed(t, f.call(t, f.folder, "s1", "Bash", cmd("echo x > in.txt")), "a write in the folder")
}

// In a repository, a write through a link to outside it passes today: the
// shell's written path is inside the project (contract L464). A known
// finding (L472), pinned as it is until it is decided.
func TestRepoSymlinkOutPinned(t *testing.T) {
	t.Parallel()
	f := newSessionFixture(t)
	repo := realTemp(t)
	support.GitRepo(t, repo)
	if err := os.Symlink(f.other, filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	wantAllowed(t, f.call(t, repo, "s1", "Bash", cmd("echo x > link/y.txt")), "repo: bash through the link")
	wantAsked(t, f.write(t, repo, "s1", filepath.Join(repo, "link", "y.txt")), "outside-write", "repo: Write through the link")
}

// A git repository under the granted folder is a project of its own: the
// folder's grant does not cover it.
func TestSessionGrantNestedRepo(t *testing.T) {
	t.Parallel()
	f := newSessionFixture(t)
	nested := mkdir(t, filepath.Join(f.folder, "nested"))
	support.GitRepo(t, nested)
	f.grantFolder(t)
	wantAsked(t, f.write(t, f.folder, "s1", filepath.Join(nested, "a.txt")), "outside-write", "Write into the nested repo")
}

// Whatever the session id, its record is one hashed file in the session-grants directory.
func TestSessionGrantIdStaysInDir(t *testing.T) {
	t.Parallel()
	f := newSessionFixture(t)
	dir := filepath.Join(f.state(), "jhon", "session-grants")
	for _, id := range []string{"../../x", "a/b", "/etc/x"} {
		code(t, support.Jhon(t, f.folder, f.env, "grant", "outside-write", "--session", id), 0, id)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("%d records, want 3", len(entries))
	}
	for _, e := range entries {
		matches(t, e.Name(), `^[0-9a-f]{64}\.json$`, "record name")
	}
}

func TestSessionGrantRefusesBadIds(t *testing.T) {
	t.Parallel()
	f := newSessionFixture(t)
	for _, id := range []string{"a\nb", "a\rb", "a\tb", "\x1b[31m"} {
		r := support.Jhon(t, f.folder, f.env, "grant", "outside-write", "--session", id)
		code(t, r, 2, "id "+fmt.Sprintf("%q", id))
	}
}

func TestSessionGrantExpired(t *testing.T) {
	t.Parallel()
	f := newSessionFixture(t)
	f.grantFolder(t)
	dir := filepath.Join(f.state(), "jhon", "session-grants")
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("%d records", len(entries))
	}
	file := filepath.Join(dir, entries[0].Name())
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	b = regexp.MustCompile(`"expires_at": "[^"]*"`).ReplaceAll(b, []byte(`"expires_at": "2020-01-01T00:00:00.000Z"`))
	if err := os.WriteFile(file, b, 0o600); err != nil {
		t.Fatal(err)
	}
	wantAsked(t, f.write(t, f.folder, "s1", filepath.Join(f.folder, "a.txt")), "outside-write", "an expired grant")
}

// A mode recorded for the folder before it stopped being a project lends nothing.
func TestSessionGrantOldFolderMode(t *testing.T) {
	t.Parallel()
	f := newSessionFixture(t)
	sum := sha256.Sum256([]byte(f.folder))
	support.Write(t, filepath.Join(f.state(), "jhon", "modes"), map[string]string{
		hex.EncodeToString(sum[:]) + ".json": `{"mode":"super-yolo","root":"` + f.folder + `","since":"2026-01-01T00:00:00.000Z"}`,
	})
	wantAsked(t, f.write(t, f.folder, "s1", filepath.Join(f.folder, "a.txt")), "outside-write", "an old folder mode")
}

// A folder that holds jhon's state is never a grant's.
func TestSessionGrantRefusesStateHolder(t *testing.T) {
	t.Parallel()
	f := newSessionFixture(t)
	env := append(append([]string{}, f.env...), "XDG_STATE_HOME="+filepath.Join(f.folder, "state"))
	r := support.Jhon(t, f.folder, env, "grant", "outside-write", "--session", "s1")
	code(t, r, 2, "a folder holding the state")
	matches(t, r.Stderr, `jhon's state`, "a folder holding the state")
}

// Existing files of a repository nested in the granted folder are that
// repository's: the folder's grant does not cover them (L475).
func TestSessionGrantNestedRepoExistingFiles(t *testing.T) {
	t.Parallel()
	f := newSessionFixture(t)
	nested := mkdir(t, filepath.Join(f.folder, "nested"))
	support.GitRepo(t, nested)
	support.Write(t, nested, map[string]string{"e.txt": "a\n", "src/main.go": "package main\n"})
	f.grantFolder(t)
	wantAsked(t, f.write(t, f.folder, "s1", filepath.Join(nested, "e.txt")), "outside-write", "Write an existing file")
	wantAsked(t, f.write(t, f.folder, "s1", filepath.Join(nested, "src", "main.go")), "outside-write", "Write an existing deep file")
	wantAsked(t, f.call(t, f.folder, "s1", "Edit", map[string]any{"file_path": filepath.Join(nested, "e.txt"), "old_string": "a", "new_string": "b"}), "outside-write", "Edit")
}
