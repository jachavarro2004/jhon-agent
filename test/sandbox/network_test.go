// T128g (L1302, L1303): a line of git network commands or gh runs outside
// the sandbox in both runtimes, with the guards' decision as before; every
// other line keeps the work profile. A wrapped command that fails on a
// read-only path gets one note that it is a stop, and every run records a
// jhon.sandbox event with its profile and exit code.
package sandbox

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// G1: `jhon sandbox profile -- <line>` is the runtimes' question: unsandboxed
// for a line of git network commands or gh, work for any other.
func TestProfileAnswersUnsandboxedOnlyForGitNetworkAndGhLines(t *testing.T) {
	env := support.Env("JHON_TELEMETRY=off")
	proj := t.TempDir()
	support.GitRepo(t, proj)
	for line, want := range map[string]string{
		"git fetch origin":                    "unsandboxed",
		"git pull --rebase":                   "unsandboxed",
		"git push -u origin feat/x":           "unsandboxed",
		"git ls-remote origin":                "unsandboxed",
		"git clone https://example.invalid/r": "unsandboxed",
		"git remote update":                   "unsandboxed",
		"gh pr create --title t --body b":     "unsandboxed",
		"gh pr view 1 && git fetch":           "unsandboxed",
		"git fetch && make":                   "work",
		"git status":                          "work",
		"gh auth token":                       "work",
		"gh pr checkout 1":                    "work",
		"git fetch --upl=x":                   "work",
		"git fetch 'unterminated":             "work",
		"":                                    "work",
	} {
		r := support.Jhon(t, proj, env, "sandbox", "profile", "--", line)
		if r.Code != 0 || r.Stdout != want+"\n" || r.Stderr != "" {
			t.Errorf("profile %q: exit %d, stdout %q, stderr %q; want 0 and %s", line, r.Code, r.Stdout, r.Stderr, want)
		}
	}
	for _, args := range [][]string{{"sandbox", "profile"}, {"sandbox", "profile", "git fetch"}, {"sandbox", "profile", "--", "a", "b"}} {
		r := support.Jhon(t, t.TempDir(), env, args...)
		if r.Code != 2 || r.Stdout != "" || !strings.Contains(r.Stderr, "usage: jhon sandbox profile [--project <folder>] -- <command>") {
			t.Errorf("%q: exit %d, stdout %q, stderr %q; want the usage and exit 2", args, r.Code, r.Stdout, r.Stderr)
		}
	}
}

// unwrapped is the line Claude Code runs for an unsandboxed command.
func unwrapped(t *testing.T, proj, shell, command string) string {
	t.Helper()
	return support.Bin(t) + " sandbox run --project '" + proj + "' --profile unsandboxed -- " + shell + " -c '" + strings.ReplaceAll(command, "'", `'\''`) + "'"
}

// G1 for Claude Code: a Bash or Monitor call of a network line is handed back
// as an unsandboxed run, with no directory to follow; a mixed line keeps the
// work wrap; the guards' ask goes with the unsandboxed line as before.
func TestClaudeCodeRunsANetworkLineUnsandboxed(t *testing.T) {
	bash := realShell(t, "bash")
	b, env := wired(t, bash)
	for _, c := range []struct{ tool, input, command string }{
		{"Bash", `{"command":"git fetch origin","timeout":5}`, "git fetch origin"},
		{"Bash", `{"command":"gh pr list","run_in_background":true}`, "gh pr list"},
		{"Monitor", `{"command":"gh run watch","timeout_ms":1000}`, "gh run watch"},
	} {
		r, a := preTool(t, b, env, bashGuards, c.tool, c.input, false)
		if r.Code != 0 || r.Stderr != "" || a.Out.Decision != "allow" {
			t.Fatalf("%s %q: exit %d, stderr %q, stdout %q", c.tool, c.command, r.Code, r.Stderr, r.Stdout)
		}
		if got, want := a.command(t), unwrapped(t, b.proj, bash, c.command); got != want {
			t.Errorf("%s %q: the command is\n %s\nwant\n %s", c.tool, c.command, got, want)
		}
	}
	_, a := preTool(t, b, env, bashGuards, "Monitor", `{"command":"git fetch && make","timeout_ms":1000}`, false)
	if got, want := a.command(t), expected(t, "Monitor", a.command(t), b.proj, bash, "git fetch && make"); got != want {
		t.Errorf("a mixed line: the command is\n %s\nwant the work wrap\n %s", got, want)
	}
	// The push ask is the guard's, as with the mode off.
	off := modeOff(t, b, env)
	_, asked := preTool(t, b, off, bashGuards, "Bash", `{"command":"git push origin main"}`, true)
	r, a := preTool(t, b, env, bashGuards, "Bash", `{"command":"git push origin main"}`, true)
	if r.Code != 0 || asked.Out.Decision != "ask" || a.Out.Decision != "ask" || a.Out.Reason != asked.Out.Reason || a.command(t) != unwrapped(t, b.proj, bash, "git push origin main") {
		t.Errorf("a push: exit %d, %q (%q), %s; want the guard's ask %q with the unsandboxed line", r.Code, a.Out.Decision, a.Out.Reason, a.command(t), asked.Out.Reason)
	}
	// A refusal stays the guard's refusal.
	if r, _ := preTool(t, b, env, bashGuards, "Bash", `{"command":"git push --force origin main"}`, false); r.Code != 2 || r.Stdout != "" {
		t.Errorf("a force push: exit %d, stdout %q; want the guard's refusal", r.Code, r.Stdout)
	}
}

// fakeSSH is a folder, put first on PATH, with an ssh that opens no
// connection (as on 1ac9872's delivery tests): it appends "ran" and the
// GIT_CONFIG_KEY_ and _VALUE_ variables git hands it to note, and runs,
// here, the git command the remote side would run, so an ssh:// remote is a
// local bare repository. The note is outside every folder a sandboxed
// command may write: only an unsandboxed ssh can write it.
func fakeSSH(t *testing.T, b *box, note string) {
	t.Helper()
	bin := filepath.Join(b.base, "bin")
	support.Write(t, bin, map[string]string{"ssh": "#!/bin/sh\n{ echo ran; env | grep -E '^GIT_CONFIG_(KEY|VALUE)_' | LC_ALL=C sort; } >> " + note + "\nfor last; do :; done\nexec sh -c \"$last\"\n"})
	if err := os.Chmod(filepath.Join(bin, "ssh"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i, kv := range b.env {
		if path, ok := strings.CutPrefix(kv, "PATH="); ok {
			b.env[i] = "PATH=" + bin + ":" + path
		}
	}
}

// originOverSSH is a bare repository outside the project holding the
// project's HEAD as main, the project's origin as an ssh:// remote reached
// through fakeSSH, with main tracking it; the ssh notes go to note.
func originOverSSH(t *testing.T, b *box, note string) string {
	t.Helper()
	remote := filepath.Join(b.base, "remote.git")
	support.Git(t, b.base, "init", "-q", "--bare", remote)
	support.Git(t, b.proj, "push", "-q", remote, "HEAD:refs/heads/main")
	fakeSSH(t, b, note)
	support.Git(t, b.proj, "remote", "add", "origin", "ssh://example.invalid"+remote)
	support.Git(t, b.proj, "config", "branch.main.remote", "origin")
	support.Git(t, b.proj, "config", "branch.main.merge", "refs/heads/main")
	return remote
}

// unsandboxed runs line as the runtimes run an unsandboxed one.
func (b box) unsandboxed(t *testing.T, env []string, line string) support.Result {
	t.Helper()
	return support.Jhon(t, b.proj, env, "sandbox", "run", "--project", b.proj, "--profile", "unsandboxed", "--", realShell(t, "sh"), "-c", line)
}

// G1 for the run: the unsandboxed profile runs a network line outside
// bubblewrap (its ssh writes outside every writable folder), and anything
// else it is given with the work profile: a line that is not one, a git
// working in a folder outside the project (-C, a clone's target).
func TestTheUnsandboxedProfileRunsOnlyANetworkLineOutsideTheSandbox(t *testing.T) {
	b := newBox(t)
	ssh := filepath.Join(b.base, "ssh-ran")
	remote := originOverSSH(t, &b, ssh)
	sh := realShell(t, "sh")
	if r := b.unsandboxed(t, b.env, "git clone -q ssh://example.invalid"+remote+" inside"); r.Code != 0 || !exists(filepath.Join(b.proj, "inside", ".git")) || !exists(ssh) {
		t.Fatalf("an unsandboxed clone into the project: exit %d, stderr %q, cloned %v, ssh wrote outside %v; want all", r.Code, r.Stderr, exists(filepath.Join(b.proj, "inside", ".git")), exists(ssh))
	}
	os.Remove(ssh)
	if r := b.sh(t, b.proj, "work", "git ls-remote origin"); exists(ssh) {
		t.Errorf("a work ls-remote wrote outside the work area (exit %d)", r.Code)
	}
	for name, line := range map[string]string{
		"a clone into the home":          "git clone -q ssh://example.invalid" + remote + " " + filepath.Join(b.home, "outside"),
		"a clone named into the home":    "git -C " + b.home + " clone -q ssh://example.invalid" + remote,
		"a clone through ..":             "git clone -q ssh://example.invalid" + remote + " ../outside",
		"-C another repository":          "git -C " + b.home + " ls-remote origin",
		"-C up and out":                  "git -C .. fetch -q origin",
		"a second -C out of the project": "git -C sub -C ../.. ls-remote origin",
	} {
		os.Remove(ssh)
		if r := b.unsandboxed(t, b.env, line); exists(ssh) || exists(filepath.Join(b.home, "outside")) || exists(filepath.Join(b.home, "remote")) {
			t.Errorf("%s: exit %d, stderr %q; want it run with the work profile", name, r.Code, r.Stderr)
		}
	}
	if r := support.Jhon(t, b.proj, b.env, "sandbox", "profile", "--project", b.proj, "--", "git -C "+b.home+" fetch"); r.Code != 0 || r.Stdout != "work\n" {
		t.Errorf("profile of -C outside: exit %d, %q; want work", r.Code, r.Stdout)
	}
	if r := support.Jhon(t, b.proj, b.env, "sandbox", "profile", "--project", b.proj, "--", "git -C "+b.proj+" fetch"); r.Code != 0 || r.Stdout != "unsandboxed\n" {
		t.Errorf("profile of -C inside: exit %d, %q; want unsandboxed", r.Code, r.Stdout)
	}
	escape := filepath.Join(b.home, "escape.txt")
	for name, argv := range map[string][]string{
		"a line that is not a network line": {sh, "-c", `echo x > "$HOME/escape.txt"`},
		"a mixed line":                      {sh, "-c", `git ls-remote origin > "$HOME/escape.txt"`},
		"no shell":                          {"git", "clone", "-q", "ssh://example.invalid" + remote, filepath.Join(b.home, "bare-clone")},
		"a shell that is not one":           {filepath.Join(b.proj, "fake-sh"), "-c", "git fetch"},
		"more words after the line":         {sh, "-c", `echo x > "$1"`, "git fetch", escape},
	} {
		r := support.Jhon(t, b.proj, b.env, append([]string{"sandbox", "run", "--project", b.proj, "--profile", "unsandboxed", "--"}, argv...)...)
		if r.Code == 0 || exists(escape) || exists(filepath.Join(b.home, "bare-clone")) {
			t.Errorf("%s: exit %d, stderr %q; want it run in the work profile, and refused", name, r.Code, r.Stderr)
		}
	}
}

// note is the G3 note for path, on its own line: a path that does not
// exist, directly in the home (T239, L26 A4).
func note(path string) string { return homeNote(path) }

// G3: a wrapped command that fails on a read-only path gets the note once,
// after its own output, on stderr; one that does not fail, or fails for
// another reason, gets none.
func TestAReadOnlyFailureEndsWithTheStopNote(t *testing.T) {
	b := newBox(t)
	escape := filepath.Join(b.home, "escape.txt")
	// The shell's own message differs (dash's "cannot create <path>",
	// bash's "<path>"); both end with the path and the reason.
	refused := escape + ": Read-only file system\n"
	r := b.sh(t, b.proj, "", `echo before; echo x > "$HOME/escape.txt"; echo y > "$HOME/escape.txt"`)
	if r.Code == 0 || r.Stdout != "before\n" || !strings.HasSuffix(r.Stderr, refused+note(escape)) || strings.Count(r.Stderr, "the sandbox refused a write") != 1 {
		t.Errorf("a refused write: exit %d, stdout %q, stderr %q; want a failure ending with one note for %s", r.Code, r.Stdout, r.Stderr, escape)
	}
	r = b.sh(t, b.proj, "", `exec 2>&1; echo x > "$HOME/escape.txt"`)
	if r.Code == 0 || !strings.HasSuffix(r.Stdout, refused) || r.Stderr != note(escape) {
		t.Errorf("the error on stdout: exit %d, stdout %q, stderr %q; want the note alone on stderr", r.Code, r.Stdout, r.Stderr)
	}
	for name, script := range map[string]string{
		"a success that mentions it": `echo "x: /a: Read-only file system" >&2`,
		"another failure":            `echo nope >&2; exit 3`,
	} {
		r := b.sh(t, b.proj, "", script)
		if strings.Contains(r.Stderr, "the sandbox refused a write") {
			t.Errorf("%s: stderr %q; want no note", name, r.Stderr)
		}
	}
	// Claude Code's wrapped line carries the note too.
	bash := realShell(t, "bash")
	wb, env := wired(t, bash)
	_, a := preTool(t, wb, env, []string{"vcs-guard", ".git"}, "Bash", `{"command":"echo x > \"$HOME/escape.txt\""}`, false)
	ran := support.Run(t, support.Cmd{Args: []string{bash, "-c", a.command(t)}, Dir: wb.proj, Env: env})
	if want := note(filepath.Join(wb.home, "escape.txt")); ran.Code == 0 || !strings.HasSuffix(ran.Stderr, want) {
		t.Errorf("Claude Code's line: exit %d, stderr %q; want it to end with %q", ran.Code, ran.Stderr, want)
	}
}

// events are the jhon.sandbox events the box's telemetry holds.
func events(t *testing.T, b box) []map[string]any {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(b.home, ".local", "share", "jhon", "telemetry", "*.jsonl"))
	var out []map[string]any
	for _, f := range files {
		file, err := os.Open(f)
		if err != nil {
			t.Fatal(err)
		}
		s := bufio.NewScanner(file)
		for s.Scan() {
			var e struct {
				Name       string         `json:"name"`
				Attributes map[string]any `json:"attributes"`
			}
			if json.Unmarshal(s.Bytes(), &e) == nil && e.Name == "jhon.sandbox" {
				out = append(out, e.Attributes)
			}
		}
		file.Close()
	}
	return out
}

// G4: every run, wrapped or unsandboxed, records jhon.sandbox with its
// profile and exit code, when telemetry is on; with it off nothing.
func TestEveryRunRecordsItsProfileAndExitCode(t *testing.T) {
	b := newBox(t)
	b.settings(t, "schema = 1\n\n[integrations]\ntelemetry = true\n")
	originOverSSH(t, &b, filepath.Join(b.base, "ssh-ran"))
	on := append(append([]string{}, b.env...), "JHON_TELEMETRY=on")
	sh := realShell(t, "sh")
	if r := support.Jhon(t, b.proj, on, "sandbox", "run", "--", sh, "-c", "exit 3"); r.Code != 3 {
		t.Fatalf("a work run: exit %d, stderr %q", r.Code, r.Stderr)
	}
	if r := b.unsandboxed(t, on, "git ls-remote -q origin"); r.Code != 0 {
		t.Fatalf("an unsandboxed run: exit %d, stderr %q", r.Code, r.Stderr)
	}
	refused := support.Jhon(t, b.proj, on, "sandbox", "run", "--", sh, "-c", `echo x > "$HOME/escape.txt"`)
	if refused.Code == 0 {
		t.Fatalf("a refused write: exit 0")
	}
	got := events(t, b)
	if len(got) != 3 {
		t.Fatalf("%d jhon.sandbox events, want 3: %v", len(got), got)
	}
	for i, want := range []struct {
		profile string
		code    float64
	}{{"work", 3}, {"unsandboxed", 0}, {"work", float64(refused.Code)}} {
		if got[i]["jhon.sandbox.profile"] != want.profile || got[i]["process.exit.code"] != want.code {
			t.Errorf("event %d is %v; want profile %s and exit %v", i, got[i], want.profile, want.code)
		}
	}
	if got[2]["jhon.sandbox.refused_write"] != filepath.Join(b.home, "escape.txt") {
		t.Errorf("the refused write's event is %v; want the path", got[2])
	}
	// Telemetry off records nothing.
	if r := support.Jhon(t, b.proj, b.env, "sandbox", "run", "--", sh, "-c", "true"); r.Code != 0 || len(events(t, b)) != 3 {
		t.Errorf("with telemetry off: exit %d, %d events; want none added", r.Code, len(events(t, b)))
	}
}

// L1306: an unsandboxed git runs none of the repository's code: a planted
// pre-push, reference-transaction and post-merge hook, a core.sshCommand
// and a core.fsmonitor in .git/config (all writable from the work profile)
// do not run on a push, a fetch or a pull, whose remote is a local bare
// repository reached through the owner's ssh (fakeSSH), which runs.
// L1318 (b): git 2.54 and later run hooks named in the configuration
// (hook.<name>.command with hook.<name>.event), which core.hooksPath does
// not turn off. One planted in the project's .git/config for every event a
// network command fires runs on no unsandboxed pull, fetch, push or clone.
func TestAConfigHookDoesNotRunInAnUnsandboxedPull(t *testing.T) {
	b := newBox(t)
	ssh := filepath.Join(b.base, "ssh-ran")
	remote := originOverSSH(t, &b, ssh)
	// The remote is one commit ahead, so the pull fast-forwards.
	support.Git(t, b.proj, "commit", "-q", "--allow-empty", "-m", "ahead")
	support.Git(t, b.proj, "push", "-q", remote, "HEAD:refs/heads/main")
	support.Git(t, b.proj, "update-ref", "refs/heads/main", "HEAD~1")
	ran := filepath.Join(b.proj, "ran")
	hook := "[hook \"x\"]\n\tcommand = echo \"$0 $*\" >> " + ran + "\n"
	for _, event := range []string{"pre-push", "post-merge", "post-checkout", "post-rewrite", "reference-transaction", "pre-auto-gc", "pre-rebase", "pre-merge-commit", "prepare-commit-msg", "commit-msg", "post-index-change"} {
		hook += "\tevent = " + event + "\n"
	}
	f, err := os.OpenFile(filepath.Join(b.proj, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(hook); err != nil {
		t.Fatal(err)
	}
	f.Close()
	for _, line := range []string{"git pull -q --ff-only", "git fetch -q origin", "git push -q origin HEAD:refs/heads/other", "git clone -q ssh://example.invalid" + remote + " cloned"} {
		if r := b.unsandboxed(t, b.env, line); r.Code != 0 {
			t.Errorf("unsandboxed %q: exit %d, stderr %q; want 0", line, r.Code, r.Stderr)
		}
	}
	if got, err := os.ReadFile(ran); err == nil {
		t.Errorf("a configured hook ran outside the sandbox:\n%s", got)
	}
	if got, want := support.Git(t, b.proj, "rev-parse", "HEAD"), support.Git(t, b.proj, "--git-dir", remote, "rev-parse", "main"); got != want {
		t.Errorf("the pull did not fast-forward: HEAD %s, the remote's main %s", got, want)
	}
	// The same pull in the work profile runs the hook: the plant is live.
	support.Git(t, b.proj, "update-ref", "refs/heads/main", "HEAD~1")
	support.Git(t, b.proj, "reset", "-q", "--hard")
	if r := b.sh(t, b.proj, "work", "git -c core.sshCommand=ssh pull -q --ff-only"); !exists(ran) {
		t.Errorf("the planted hook does not run in a work pull (exit %d, stderr %q): the test proves nothing", r.Code, r.Stderr)
	}
}

// L1318 (c): when the caller gives stdout and stderr one descriptor
// (2>&1), the program gets one too, so its lines keep their order; the
// note and the event still come.
func TestMergedOutputKeepsItsOrder(t *testing.T) {
	b := newBox(t)
	b.settings(t, "schema = 1\n\n[integrations]\ntelemetry = true\n")
	on := append(append([]string{}, b.env...), "JHON_TELEMETRY=on")
	var want strings.Builder
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&want, "out %d\nerr %d\n", i, i)
	}
	merged := func(script string) (string, int) {
		var out bytes.Buffer
		cmd := exec.Command(support.Bin(t), "sandbox", "run", "--", "sh", "-c", script)
		cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = b.proj, on, &out, &out
		err := cmd.Run()
		code := 0
		if e, ok := err.(*exec.ExitError); ok {
			code = e.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return out.String(), code
	}
	script := `i=1; while [ $i -le 40 ]; do echo "out $i"; echo "err $i" >&2; i=$((i+1)); done`
	for run := 0; run < 10; run++ {
		if got, code := merged(script); got != want.String() || code != 0 {
			t.Fatalf("run %d: exit %d, the merged output is\n%s\nwant the lines in order", run, code, got)
		}
	}
	escape := filepath.Join(b.home, "escape.txt")
	got, code := merged(`echo before; echo x > "$HOME/escape.txt"`)
	if code == 0 || !strings.HasPrefix(got, "before\n") || !strings.HasSuffix(got, escape+": Read-only file system\n"+note(escape)) {
		t.Errorf("a refused write, merged: exit %d, output %q; want the note last", code, got)
	}
	if n := len(events(t, b)); n != 11 {
		t.Errorf("%d jhon.sandbox events, want 11", n)
	}
}

func TestTheRepositorysHooksAndSshCommandDoNotRunUnsandboxed(t *testing.T) {
	b := newBox(t)
	ssh := filepath.Join(b.base, "ssh-ran")
	remote := originOverSSH(t, &b, ssh)
	// The remote is one commit ahead, so the pull fast-forwards.
	support.Git(t, b.proj, "commit", "-q", "--allow-empty", "-m", "ahead")
	support.Git(t, b.proj, "push", "-q", remote, "HEAD:refs/heads/main")
	support.Git(t, b.proj, "update-ref", "refs/heads/main", "HEAD~1")
	ran := filepath.Join(b.proj, "ran")
	support.Git(t, b.proj, "config", "core.sshCommand", "echo sshCommand >> "+ran+"; false")
	support.Git(t, b.proj, "config", "core.fsmonitor", "echo fsmonitor >> "+ran+"; false")
	for _, hook := range []string{"pre-push", "reference-transaction", "post-merge"} {
		p := filepath.Join(b.proj, ".git", "hooks", hook)
		support.Write(t, filepath.Dir(p), map[string]string{hook: "#!/bin/sh\necho " + hook + " >> " + ran + "\n"})
		if err := os.Chmod(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, line := range []string{"git pull -q --ff-only", "git push -q origin HEAD:refs/heads/other", "git fetch -q origin", "git ls-remote origin"} {
		if r := b.unsandboxed(t, b.env, line); r.Code != 0 {
			t.Errorf("unsandboxed %q: exit %d, stderr %q; want 0", line, r.Code, r.Stderr)
		}
	}
	if got, err := os.ReadFile(ran); err == nil {
		t.Errorf("the repository's code ran outside the sandbox: %q", got)
	}
	if !exists(ssh) {
		t.Errorf("the owner's ssh never ran")
	}
	if got, want := support.Git(t, b.proj, "--git-dir", remote, "rev-parse", "other"), support.Git(t, b.proj, "rev-parse", "HEAD"); got != want {
		t.Errorf("the pull and the push: the remote's other is %s, HEAD %s", got, want)
	}
}

// L1306: the overrides come after the owner's own GIT_CONFIG_COUNT entries,
// which stay, and the repository's credential helpers are reset, the
// owner's global ones added back; a local-path remote is refused.
func TestTheOverridesFollowTheOwnersOwnAndRefuseALocalPath(t *testing.T) {
	b := newBox(t, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=user.name", "GIT_CONFIG_VALUE_0=the-owner", "GIT_CONFIG_PARAMETERS='core.hookspath'='/tmp/x'", "GIT_CONFIG_SYSTEM=/dev/null")
	ssh := filepath.Join(b.base, "ssh-ran")
	originOverSSH(t, &b, ssh)
	support.Write(t, b.home, map[string]string{".gitconfig": "[credential]\n\thelper = cache --timeout=1\n[credential \"https://example.invalid\"]\n\thelper = store\n"})
	if r := b.unsandboxed(t, b.env, "git ls-remote -q origin"); r.Code != 0 {
		t.Fatalf("ls-remote: exit %d, stderr %q", r.Code, r.Stderr)
	}
	// The entries in their order, then in the fake ssh's byte order (LC_ALL=C sort).
	entries := [][2]string{{"user.name", "the-owner"}, {"core.hooksPath", "/dev/null"}, {"core.sshCommand", "ssh"}, {"core.fsmonitor", "false"},
		{"credential.helper", ""}, {"credential.helper", "cache --timeout=1"}, {"credential.https://example.invalid.helper", "store"},
		{"protocol.ext.allow", "never"}, {"protocol.fd.allow", "never"}, {"protocol.file.allow", "never"}, {"submodule.recurse", "false"}, {"fetch.recurseSubmodules", "false"},
		{"gc.autoDetach", "false"}, {"maintenance.autoDetach", "false"}}
	for _, event := range []string{"applypatch-msg", "pre-applypatch", "post-applypatch", "pre-commit", "pre-merge-commit", "prepare-commit-msg", "commit-msg", "post-commit",
		"pre-rebase", "post-checkout", "post-merge", "pre-push", "pre-receive", "update", "proc-receive", "post-receive", "post-update",
		"reference-transaction", "push-to-checkout", "pre-auto-gc", "post-rewrite", "sendemail-validate", "fsmonitor-watchman", "post-index-change"} {
		entries = append(entries, [2]string{"hook." + event + ".enabled", "false"})
	}
	var want []string
	for i, e := range entries {
		want = append(want, fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", i, e[0]), fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", i, e[1]))
	}
	slices.Sort(want)
	want = append([]string{"ran"}, want...)
	if got := strings.Split(strings.TrimSpace(support.Read(t, ssh)), "\n"); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("git handed ssh\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	local := filepath.Join(b.base, "local.git")
	support.Git(t, b.base, "init", "-q", "--bare", local)
	hook := filepath.Join(b.base, "pre-receive-ran")
	support.Write(t, filepath.Join(local, "hooks"), map[string]string{"pre-receive": "#!/bin/sh\necho x >> " + hook + "\n"})
	if err := os.Chmod(filepath.Join(local, "hooks", "pre-receive"), 0o755); err != nil {
		t.Fatal(err)
	}
	if r := b.unsandboxed(t, b.env, "git push -q "+local+" HEAD:refs/heads/main"); r.Code == 0 || exists(hook) {
		t.Errorf("a push to a local path: exit %d, stderr %q, its hook ran %v; want git's refusal", r.Code, r.Stderr, exists(hook))
	}
}

// L1305: a work command's uv tools go to the sandbox's own folder under the
// cache, and the owner's ~/.local/share/uv/tools stays read-only; a
// UV_TOOL_DIR the owner set is kept.
func TestUvToolsLandInTheSandboxsOwnFolder(t *testing.T) {
	b := newBox(t)
	mkdirs(t, filepath.Join(b.home, ".local", "share", "uv", "tools"))
	dir := filepath.Join(b.home, ".cache", "jhon", "sandbox", "uv-tools")
	r := b.sh(t, b.proj, "", `echo "$UV_TOOL_DIR $UV_TOOL_BIN_DIR" && mkdir -p "$UV_TOOL_DIR/ruff/bin" "$UV_TOOL_BIN_DIR" && echo x > "$UV_TOOL_DIR/ruff/bin/ruff" && ln -s "$UV_TOOL_DIR/ruff/bin/ruff" "$UV_TOOL_BIN_DIR/ruff"`)
	if r.Code != 0 || r.Stdout != dir+" "+filepath.Join(dir, "bin")+"\n" || !exists(filepath.Join(dir, "ruff", "bin", "ruff")) || !exists(filepath.Join(dir, "bin", "ruff")) {
		t.Errorf("a uvx-style write: exit %d, stdout %q, stderr %q; want it in %s", r.Code, r.Stdout, r.Stderr, dir)
	}
	owner := filepath.Join(b.home, ".local", "share", "uv", "tools", "ruff")
	if r := b.sh(t, b.proj, "", `mkdir "$HOME/.local/share/uv/tools/ruff"`); !readOnly(r) || exists(owner) {
		t.Errorf("a write to ~/.local/share/uv/tools: exit %d, stderr %q; want a read-only refusal", r.Code, r.Stderr)
	}
	mine := filepath.Join(b.home, ".cache", "my-uv-tools")
	env := append(append([]string{}, b.env...), "UV_TOOL_DIR="+mine)
	if r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", `echo "$UV_TOOL_DIR $UV_TOOL_BIN_DIR"`); r.Code != 0 || r.Stdout != mine+" "+filepath.Join(dir, "bin")+"\n" {
		t.Errorf("the owner's UV_TOOL_DIR: exit %d, stdout %q; want it kept", r.Code, r.Stdout)
	}
}

// L1306 for Claude Code: a git line working outside the project keeps the
// work wrap.
func TestClaudeCodeWrapsAGitLineOutsideTheProject(t *testing.T) {
	bash := realShell(t, "bash")
	b, env := wired(t, bash)
	for _, command := range []string{"git -C " + b.home + " fetch", "git clone https://example.invalid/r.git " + filepath.Join(b.home, "r")} {
		_, a := preTool(t, b, env, noGuard, "Monitor", `{"command":`+quoted(t, command)+`,"timeout_ms":1000}`, false)
		if got, want := a.command(t), expected(t, "Monitor", a.command(t), b.proj, bash, command); got != want {
			t.Errorf("%q: the command is\n %s\nwant the work wrap\n %s", command, got, want)
		}
	}
	_, a := preTool(t, b, env, noGuard, "Monitor", `{"command":"git clone https://example.invalid/r.git","timeout_ms":1000}`, false)
	if got, want := a.command(t), unwrapped(t, b.proj, bash, "git clone https://example.invalid/r.git"); got != want {
		t.Errorf("a clone into the project: the command is\n %s\nwant\n %s", got, want)
	}
}
