// T161 (L1419, L1420): the owner restored two v0.3.7 protections the T113
// pruning had removed, each with v0.3.7's verdict and words, through `jhon
// hook pre-tool` as Claude Code runs the guards. The sandbox wraps Bash
// only, so a file tool's write outside the project is asked (top level) or
// refused (sub-agent) under outside-write, and its grant, timed or for a
// session, passes it; a file tool's read of credentials is refused, since
// one that reaches the model's provider cannot be undone (L1366).

package guards

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// restoredFixture is a project (a git repository), a folder that is no
// project (outside the scratch roots), a fake HOME holding fake credentials,
// and the state, all fresh per test.
type restoredFixture struct {
	project, root, folder, home, state, fw string
	env                                    []string
}

func newRestoredFixture(t *testing.T) restoredFixture {
	t.Helper()
	scratch := realTemp(t)
	f := restoredFixture{
		project: mkdir(t, filepath.Join(scratch, "proj", "src")),
		folder:  mkdir(t, filepath.Join(outsideTmp(t), "docs")),
		home:    mkdir(t, filepath.Join(scratch, "home")),
		state:   filepath.Join(scratch, "state"),
		fw:      mkdir(t, filepath.Join(scratch, "fw")),
	}
	f.project = filepath.Dir(f.project)
	support.GitRepo(t, f.project)
	f.root = strings.TrimSpace(support.Git(t, f.project, "rev-parse", "--show-toplevel"))
	support.Write(t, f.project, map[string]string{".env": "X=fake\n", "server.pem": "fake\n"})
	support.Write(t, f.home, map[string]string{".aws/config": "fake\n"})
	f.env = support.Env("HOME="+f.home, "XDG_STATE_HOME="+f.state, "TMPDIR="+mkdir(t, filepath.Join(scratch, "tmp")), "JHON_ROOT="+support.Root())
	return f
}

// pre is one pre-tool call naming every file guard, as the Claude Code
// render does: at the top level (where the owner can be asked) or by a
// worker sub-agent.
func (f restoredFixture) pre(t *testing.T, top bool, cwd, session, tool string, input map[string]any) support.Result {
	t.Helper()
	payload := map[string]any{"hook_event_name": "PreToolUse", "tool_name": tool, "tool_input": input, "cwd": cwd, "session_id": session, "permission_mode": "default"}
	if !top {
		payload["agent_id"], payload["agent_type"], payload["permission_mode"] = "a1", "worker", "bypassPermissions"
	}
	guards := append(append([]string{"--agent", "*", "safety-guard", "vcs-guard"}, vcsDirs(t)...), "framework-guard", f.fw, "secrets-guard", "route-guard")
	return hook(t, f.env, "pre-tool", guards, payload)
}

func (f restoredFixture) write(t *testing.T, top bool, cwd, session, path string) support.Result {
	t.Helper()
	return f.pre(t, top, cwd, session, "Write", map[string]any{"file_path": path, "content": "x"})
}

// outsideAsk is v0.3.7's question on a Write of path outside root.
func outsideAsk(path, root string) string {
	return "outside-write: Write of " + path + " lands outside the project (" + root + ") and the scratch roots"
}

// outsideRefusal is v0.3.7's refusal of a sub-agent's Write of path outside root.
func outsideRefusal(path, root string) string {
	return "vcs-guard: Write of " + path + " lands outside the project (" + root + ") and the scratch roots. " +
		"outside-write needs the owner's authorization. Ask the owner; they grant it by running `! jhon grant outside-write [--for 30m]` in this project, " +
		"or in pi, which has no `!`, from /jhon:grants in the shell.\n"
}

// wantRan is a call that ran with nothing printed.
func wantRan(t *testing.T, r support.Result, label string) {
	t.Helper()
	if r.Code != 0 || r.Stdout != "" || r.Stderr != "" {
		t.Errorf("%s: exit %d stdout %q stderr %q, want 0 and nothing", label, r.Code, r.Stdout, r.Stderr)
	}
}

// wantAsk is a top-level call asked with exactly why.
func wantAsk(t *testing.T, r support.Result, why, label string) {
	t.Helper()
	d, got, ok := decision(t, r.Stdout)
	if r.Code != 0 || !ok || d != "ask" || got != why || r.Stderr != "" {
		t.Errorf("%s: exit %d, decision %q %q, stderr %q; want ask %q", label, r.Code, d, got, r.Stderr, why)
	}
}

// wantRefused is a call refused with exactly stderr.
func wantRefused(t *testing.T, r support.Result, stderr, label string) {
	t.Helper()
	if r.Code != 2 || r.Stderr != stderr || r.Stdout != "" {
		t.Errorf("%s: exit %d stdout %q stderr %q, want 2 and %q", label, r.Code, r.Stdout, r.Stderr, stderr)
	}
}

// An Edit or Write outside the project and the scratch roots is asked at the
// top level and refused to a sub-agent (v0.3.7).
func TestRestoredOutsideWriteAskedAndRefused(t *testing.T) {
	t.Parallel()
	f := newRestoredFixture(t)
	wantAsk(t, f.write(t, true, f.project, "s1", "/etc/jhon.conf"), outsideAsk("/etc/jhon.conf", f.root), "top level")
	wantRefused(t, f.write(t, false, f.project, "s1", "/etc/jhon.conf"), outsideRefusal("/etc/jhon.conf", f.root), "sub-agent")
	r := f.pre(t, false, f.project, "s1", "Edit", map[string]any{"file_path": "/etc/jhon.conf", "old_string": "a", "new_string": "b"})
	if r.Code != 2 || !strings.HasPrefix(r.Stderr, "vcs-guard: Edit of /etc/jhon.conf lands outside the project") {
		t.Errorf("sub-agent Edit: exit %d stderr %q, want 2 and the outside-write refusal", r.Code, r.Stderr)
	}
}

// The owner's timed grant (`jhon grant outside-write`) passes the write,
// for a sub-agent too; its revoke brings the refusal back.
func TestRestoredOutsideWriteTimedGrant(t *testing.T) {
	t.Parallel()
	f := newRestoredFixture(t)
	r := support.Jhon(t, f.project, f.env, "grant", "outside-write", "--for", "30m")
	code(t, r, 0, "jhon grant outside-write")
	matches(t, r.Stdout, `^granted outside-write for `+regexp.QuoteMeta(f.root)+` until `, "jhon grant outside-write")
	wantRan(t, f.write(t, false, f.project, "s1", "/etc/jhon.conf"), "sub-agent under the grant")
	wantRan(t, f.write(t, true, f.project, "s1", "/etc/jhon.conf"), "top level under the grant")
	code(t, support.Jhon(t, f.project, f.env, "grant", "--revoke", "outside-write"), 0, "revoke")
	wantRefused(t, f.write(t, false, f.project, "s1", "/etc/jhon.conf"), outsideRefusal("/etc/jhon.conf", f.root), "after the revoke")
}

// In a folder that is no project, pi's "for this session" answer
// (`jhon grant outside-write --session <id>`) passes that session's writes
// under the folder, and no other session's; its revoke ends it.
func TestRestoredOutsideWriteSessionGrant(t *testing.T) {
	t.Parallel()
	f := newRestoredFixture(t)
	in := filepath.Join(f.folder, "sub", "a.txt")
	noRoot := "no project root: " + f.folder + " is not a git repository"
	ask := "outside-write: Write of " + in + " lands outside the project (" + noRoot + ") and the scratch roots"
	wantAsk(t, f.write(t, true, f.folder, "s1", in), ask, "before the grant")
	r := support.Jhon(t, f.folder, f.env, "grant", "outside-write", "--session", "s1")
	code(t, r, 0, "the session grant")
	matches(t, r.Stdout, `^granted outside-write for `+regexp.QuoteMeta(f.folder)+` for session s1 \(at the latest until `, "the session grant")
	wantRan(t, f.write(t, true, f.folder, "s1", in), "the granted session")
	wantAsk(t, f.write(t, true, f.folder, "s2", in), ask, "another session")
	r = support.Jhon(t, f.folder, f.env, "grant", "--revoke", "outside-write", "--session", "s1")
	code(t, r, 0, "the session's end")
	matches(t, r.Stdout, `^revoked outside-write for `+regexp.QuoteMeta(f.folder)+` \(session s1\)\n$`, "the session's end")
	wantAsk(t, f.write(t, true, f.folder, "s1", in), ask, "after the session")
}

// Grants files written by v0.3.7, a timed grant and a session grant, load
// and pass the write; the timed one is listed.
func TestRestoredOutsideWriteOldGrantsLoad(t *testing.T) {
	t.Parallel()
	f := newRestoredFixture(t)
	live := "2999-01-01T00:00:00.000Z"
	support.Write(t, filepath.Join(f.state, "jhon", "grants", rootKey(f.root)), map[string]string{
		"outside-write.json": "{\n  \"class\": \"outside-write\",\n  \"root\": \"" + f.root + "\",\n  \"granted_at\": \"2026-01-01T00:00:00.000Z\",\n  \"expires_at\": \"" + live + "\"\n}\n",
	})
	sum := sha256.Sum256([]byte(f.folder + "\ns1"))
	support.Write(t, filepath.Join(f.state, "jhon", "session-grants"), map[string]string{
		hex.EncodeToString(sum[:]) + ".json": "{\n  \"class\": \"outside-write\",\n  \"root\": \"" + f.folder + "\",\n  \"granted_at\": \"2026-01-01T00:00:00.000Z\",\n  \"expires_at\": \"" + live + "\",\n  \"session\": \"s1\"\n}\n",
	})
	wantRan(t, f.write(t, false, f.project, "s1", "/etc/jhon.conf"), "an old timed grant")
	wantRan(t, f.write(t, true, f.folder, "s1", filepath.Join(f.folder, "a.txt")), "an old session grant")
	r := support.Jhon(t, f.project, f.env, "grant", "--list")
	code(t, r, 0, "grant --list")
	matches(t, r.Stdout, `(?m)^outside-write `, "grant --list")
}

// yolo and super-yolo pass the write with the mode's note, as in v0.3.7.
func TestRestoredOutsideWriteModesPass(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"yolo", "super-yolo"} {
		f := newRestoredFixture(t)
		code(t, support.Jhon(t, f.project, f.env, "mode", mode), 0, "jhon mode "+mode)
		for _, top := range []bool{true, false} {
			r := f.write(t, top, f.project, "s1", "/etc/jhon.conf")
			code(t, r, 0, mode)
			if d, _, _ := decision(t, r.Stdout); d != "" {
				t.Errorf("%s top=%v: decision %q: %s", mode, top, d, r.Stdout)
			}
			want := "note (not a refusal; the call ran): allowed by the owner's " + mode + " mode: " + outsideAsk("/etc/jhon.conf", f.root)
			if got := adviceOf(t, r.Stdout); got != want {
				t.Errorf("%s top=%v: note %q, want %q", mode, top, got, want)
			}
		}
	}
}

// secrets-guard refuses every agent's Read, Grep and Glob of a credentials
// file or a home credentials folder, with v0.3.7's words.
func TestRestoredCredentialReadsRefused(t *testing.T) {
	t.Parallel()
	f := newRestoredFixture(t)
	env := filepath.Join(f.project, ".env")
	why := " holds credentials, which the owner handles: no agent reads, edits, or deletes it. " +
		"A template such as .env.example is read and edited like any file; ask the owner for a value the work needs"
	for _, top := range []bool{true, false} {
		wantRefused(t, f.pre(t, top, f.project, "s1", "Read", map[string]any{"file_path": env}), "secrets-guard: Read of "+env+why+".\n", "Read .env")
		wantRefused(t, f.pre(t, top, f.project, "s1", "Grep", map[string]any{"pattern": "x", "path": "~/.aws"}),
			"secrets-guard: Grep searching ~/.aws, which is or reaches into a home credentials directory.\n", "Grep ~/.aws")
		wantRefused(t, f.pre(t, top, f.project, "s1", "Glob", map[string]any{"pattern": "**/*.pem"}),
			"secrets-guard: Glob of `**/*.pem` matches a credentials file"+strings.TrimPrefix(why, " holds credentials")+".\n", "Glob **/*.pem")
	}
	// A template, and an ordinary file, still read.
	wantRan(t, f.pre(t, false, f.project, "s1", "Read", map[string]any{"file_path": filepath.Join(f.project, ".env.example")}), "Read .env.example")
	wantRan(t, f.pre(t, false, f.project, "s1", "Grep", map[string]any{"pattern": "x", "glob": "*.go"}), "Grep *.go")
}
