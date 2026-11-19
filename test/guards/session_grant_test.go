// A folder that is no project (no git repository) is judged as normal mode
// (T57a). A file tool's write there, or anywhere outside the project and the
// scratch roots, runs with vcs-guard's note (T153, L957), so nothing asks
// under outside-write and its grant is gone (T113, L1402): `jhon grant
// outside-write`, for a while or `--session <id>`, is a usage error, and a
// grant or session-grant record left from before lends nothing.

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

func (f sessionFixture) state() string {
	for _, kv := range f.env {
		if v, ok := strings.CutPrefix(kv, "XDG_STATE_HOME="); ok {
			return v
		}
	}
	return ""
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

// wantNoted is a write that runs with vcs-guard's outside note, and no other.
func wantNoted(t *testing.T, r support.Result, label string) {
	t.Helper()
	code(t, r, 0, label)
	matches(t, adviceOf(t, r.Stdout), outsideNote, label)
}

func TestSessionGrantFolder(t *testing.T) {
	t.Parallel()
	f := newSessionFixture(t)
	in := filepath.Join(f.folder, "sub", "a.txt")

	// The write runs with the note, as in normal mode; no mode is set there.
	wantNoted(t, f.write(t, f.folder, "s1", in), "a write in the folder")
	r := support.Jhon(t, f.folder, f.env, "mode", "yolo")
	code(t, r, 2, "jhon mode in a folder")
	matches(t, r.Stderr, `not a git repository`, "jhon mode in a folder")

	// The owner's session grant is no more (T113, L1402).
	r = support.Jhon(t, f.folder, f.env, "grant", "outside-write", "--session", "s1")
	code(t, r, 2, "the owner's session grant")
	matches(t, r.Stderr, `^jhon grant: unknown option: --session\n`, "grant")

	// A shell write is the sandbox's; from below, to another folder, from
	// another session or none, a file tool's write runs with the note.
	wantAllowed(t, f.call(t, f.folder, "s1", "Bash", cmd("echo x > sub/b.txt")), "folder, bash")
	wantNoted(t, f.write(t, filepath.Join(f.folder, "sub"), "s1", in), "the folder from below")
	wantNoted(t, f.write(t, f.folder, "s1", filepath.Join(f.other, "a.txt")), "another folder")
	wantNoted(t, f.write(t, f.other, "s1", filepath.Join(f.other, "a.txt")), "another folder as cwd")
	wantNoted(t, f.write(t, f.folder, "s2", in), "another session")
	wantNoted(t, f.write(t, f.folder, "", in), "no session")

	// What is refused, or asked as its own class, stays so; a read of
	// credentials is not judged (T151, L1366), an edit of them is.
	for _, c := range []struct {
		tool  string
		input map[string]any
	}{
		{"Write", map[string]any{"file_path": filepath.Join(f.folder, ".git", "config"), "content": "x"}},
		{"Edit", map[string]any{"file_path": filepath.Join(f.folder, ".env"), "old_string": "x", "new_string": "y"}},
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
		t.Errorf("a deletion passed: %s", r.Stdout)
	}
	r = support.Jhon(t, f.folder, f.env, "grant", "--revoke", "outside-write", "--session", "s1")
	code(t, r, 2, "revoke a session grant")
	matches(t, r.Stderr, `^jhon grant: unknown option: --session\n`, "revoke a session grant")
}

// No form of an outside-write grant is made, for a session or a while, of
// a folder, HOME, / or a project.
func TestSessionGrantNeverWidens(t *testing.T) {
	t.Parallel()
	f := newSessionFixture(t)
	home := mkdir(t, filepath.Join(filepath.Dir(f.folder), "home"))
	env := append(append([]string{}, f.env...), "HOME="+home)
	repo := realTemp(t)
	support.GitRepo(t, repo)
	for _, dir := range []string{home, "/", filepath.Dir(home), repo} {
		code(t, support.Jhon(t, f.folder, env, "grant", "outside-write", "--session", "s1", "--root", dir), 2, dir)
	}
	for _, c := range []struct {
		dir  string
		args []string
		why  string
	}{
		{f.folder, []string{"grant", "push", "--session", "s1"}, "unknown option: --session"},
		{f.folder, []string{"grant", "outside-write", "push", "--session", "s1"}, "unknown option: --session"},
		{f.folder, []string{"grant", "outside-write", "--session", "s1", "--for", "1d"}, "unknown option: --session"},
		{repo, []string{"grant", "outside-write"}, "unknown class 'outside-write'"},
		{repo, []string{"grant", "outside-write", "--for", "30m"}, "unknown class 'outside-write'"},
	} {
		r := support.Jhon(t, c.dir, env, c.args...)
		code(t, r, 2, strings.Join(c.args, " "))
		matches(t, r.Stderr, `^jhon grant: `+regexp.QuoteMeta(c.why)+`\n`, strings.Join(c.args, " "))
	}
	r := support.Jhon(t, repo, env, "grant", "--list", "--json")
	code(t, r, 0, "grant --list --json")
	if strings.Contains(r.Stdout, "outside-write") {
		t.Errorf("outside-write is still a class: %s", r.Stdout)
	}
}

// An agent may not make itself a grant, nor write the session-grants
// folder an older jhon kept, in a folder or in a repository in any mode.
func TestSessionGrantNotAnAgents(t *testing.T) {
	t.Parallel()
	f := newSessionFixture(t)
	state := f.state()
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
	wantNoted(t, f.write(t, f.folder, "s1", filepath.Join(f.folder, "a.txt")), "no grant made")
}

// Grants left from before T113: an outside-write record in the grants and a
// session grant of the folder load without error and lend nothing. The
// write still runs with the note, the list leaves the record out, and
// --revoke removes it.
func TestSessionGrantOldRecordsLendNothing(t *testing.T) {
	t.Parallel()
	f := newSessionFixture(t)
	repo := realTemp(t)
	support.GitRepo(t, repo)
	root := strings.TrimSpace(support.Git(t, repo, "rev-parse", "--show-toplevel"))
	live := "2999-01-01T00:00:00.000Z"
	sum := sha256.Sum256([]byte(root))
	grants := filepath.Join(f.state(), "jhon", "grants", hex.EncodeToString(sum[:]))
	support.Write(t, grants, map[string]string{"outside-write.json": `{"class":"outside-write","root":"` + root + `","granted_at":"2026-01-01T00:00:00.000Z","expires_at":"` + live + `"}`})
	ssum := sha256.Sum256([]byte(f.folder + "\ns1"))
	support.Write(t, filepath.Join(f.state(), "jhon", "session-grants"), map[string]string{
		hex.EncodeToString(ssum[:]) + ".json": `{"class":"outside-write","root":"` + f.folder + `","granted_at":"2026-01-01T00:00:00.000Z","expires_at":"` + live + `","session":"s1"}`,
	})
	wantNoted(t, f.write(t, repo, "s1", "/etc/jhon.conf"), "an old timed grant")
	wantNoted(t, f.write(t, f.folder, "s1", filepath.Join(f.folder, "a.txt")), "an old session grant")
	r := support.Jhon(t, repo, f.env, "grant", "--list")
	code(t, r, 0, "grant --list")
	if r.Stdout != "no grants\n" {
		t.Errorf("grant --list: %q", r.Stdout)
	}
	r = support.Jhon(t, repo, f.env, "grant", "--revoke", "outside-write")
	code(t, r, 0, "revoke the old grant")
	matches(t, r.Stdout, `^revoked outside-write for `+regexp.QuoteMeta(root)+`\n$`, "revoke the old grant")
	if _, err := os.Stat(filepath.Join(grants, "outside-write.json")); !os.IsNotExist(err) {
		t.Errorf("the old grant's file is still there: %v", err)
	}
}

// A link inside the folder to one outside it: the write's resolved path is
// outside, and it runs with the note. A shell write is the sandbox's (T128f).
func TestSessionGrantSymlinkOut(t *testing.T) {
	t.Parallel()
	f := newSessionFixture(t)
	if err := os.Symlink(f.other, filepath.Join(f.folder, "link")); err != nil {
		t.Fatal(err)
	}
	support.Write(t, f.folder, map[string]string{"a.txt": "a\n"})
	support.Write(t, f.other, map[string]string{"e.txt": "a\n"})
	wantNoted(t, f.write(t, f.folder, "s1", filepath.Join(f.folder, "link", "y.txt")), "Write through the link")
	wantNoted(t, f.call(t, f.folder, "s1", "Edit", map[string]any{"file_path": filepath.Join(f.folder, "link", "e.txt"), "old_string": "a", "new_string": "b"}), "Edit through the link")
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
	wantNoted(t, f.write(t, repo, "s1", filepath.Join(repo, "link", "y.txt")), "repo: Write through the link")
}

// A git repository under the folder is a project of its own: a write into
// it from the folder, of a new file or an existing one, runs with the note
// (L475).
func TestSessionGrantNestedRepo(t *testing.T) {
	t.Parallel()
	f := newSessionFixture(t)
	nested := mkdir(t, filepath.Join(f.folder, "nested"))
	support.GitRepo(t, nested)
	support.Write(t, nested, map[string]string{"e.txt": "a\n", "src/main.go": "package main\n"})
	wantNoted(t, f.write(t, f.folder, "s1", filepath.Join(nested, "a.txt")), "Write into the nested repo")
	wantNoted(t, f.write(t, f.folder, "s1", filepath.Join(nested, "e.txt")), "Write an existing file")
	wantNoted(t, f.write(t, f.folder, "s1", filepath.Join(nested, "src", "main.go")), "Write an existing deep file")
	wantNoted(t, f.call(t, f.folder, "s1", "Edit", map[string]any{"file_path": filepath.Join(nested, "e.txt"), "old_string": "a", "new_string": "b"}), "Edit")
}

// A mode recorded for the folder before it stopped being a project lends nothing.
func TestSessionGrantOldFolderMode(t *testing.T) {
	t.Parallel()
	f := newSessionFixture(t)
	sum := sha256.Sum256([]byte(f.folder))
	support.Write(t, filepath.Join(f.state(), "jhon", "modes"), map[string]string{
		hex.EncodeToString(sum[:]) + ".json": `{"mode":"super-yolo","root":"` + f.folder + `","since":"2026-01-01T00:00:00.000Z"}`,
	})
	wantNoted(t, f.write(t, f.folder, "s1", filepath.Join(f.folder, "a.txt")), "an old folder mode")
	wantAsked(t, f.write(t, f.folder, "s1", filepath.Join(f.folder, ".claude", "settings.json")), "settings", "an old folder mode: settings")
}
