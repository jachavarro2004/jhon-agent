// The owner's permission modes (S31): `jhon mode yolo` lets the guards
// pass the outward-facing ask classes without asking, `super-yolo` destroy
// too, and `normal` is the guards as they are. A mode lives until the owner
// turns it off, covers the project and its worktrees, and is set by the
// owner alone. What is refused stays refused in every mode.

package guards

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// setMode runs `jhon mode <mode>` in dir as the owner does.
func (s sgEnv) setMode(t *testing.T, dir, mode string) {
	t.Helper()
	r := support.Jhon(t, dir, s.env, "mode", mode)
	code(t, r, 0, "jhon mode "+mode)
}

// modeNote is the note an allowed call prints for the agent, "" for none.
func modeNote(t *testing.T, out string) string {
	t.Helper()
	if strings.TrimSpace(out) == "" {
		return ""
	}
	var j struct {
		HookSpecificOutput struct {
			PermissionDecision string `json:"permissionDecision"`
			AdditionalContext  string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &j); err != nil {
		t.Fatalf("modeNote: %v: %q", err, out)
	}
	if j.HookSpecificOutput.PermissionDecision != "" {
		t.Errorf("a mode-allowed call printed a decision: %s", out)
	}
	return j.HookSpecificOutput.AdditionalContext
}

func (s sgEnv) allowedBy(t *testing.T, command, mode string, o sgOpts, label string) {
	t.Helper()
	r := s.bash(t, command, o)
	code(t, r, 0, label)
	matches(t, modeNote(t, r.Stdout), `^note \(not a refusal; the call ran\): allowed by the owner's `+regexp.QuoteMeta(mode)+` mode: `, label)
}

func TestModeYoloAllowsOutwardStillAsksDestroy(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	s.setMode(t, s.p, "yolo")
	for _, c := range []string{"git push", "git push -u origin feat", "gh pr merge 1 --squash", "gh release create v1", "ssh host", "npm publish", "aws ecs run-task --cluster c"} {
		// A sub-agent, where nobody can be asked, and the top level alike.
		s.allowedBy(t, c, "yolo", sgOpts{}, "sub-agent "+c)
		s.allowedBy(t, c, "yolo", top(), "top-level "+c)
	}
	sub := s.bash(t, "git reset --hard", sgOpts{})
	code(t, sub, 2, "destroy, sub-agent")
	matches(t, sub.Stderr, `destroy needs the owner's authorization`, "destroy, sub-agent")
	matches(t, askedWhy(t, s.bash(t, "git reset --hard", top()).Stdout, "destroy, top-level"), `^destroy: `, "destroy, top-level")
	// A command with a covered class and destroy asks for destroy alone.
	matches(t, askedWhy(t, s.bash(t, "git push && git reset --hard", top()).Stdout, "push and destroy"), `^destroy: [^;]*$`, "push and destroy")
}

// A subagent payload (agent_id set) in dontAsk or with no permission mode:
// canAsk is false, and yolo still lets a worker push.
func TestModeYoloWithNobodyToAsk(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	s.setMode(t, s.p, "yolo")
	for _, o := range []sgOpts{{}, {mode: ptr("dontAsk")}, {noMode: true}, {top: true, mode: ptr("dontAsk")}, {top: true, mode: ptr("bypassPermissions")}} {
		s.allowedBy(t, "git push origin feat", "yolo", o, "push")
	}
}

func TestModeSuperYoloAllowsDestroy(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	s.setMode(t, s.p, "super-yolo")
	for _, c := range []string{"git reset --hard", "git clean -f", "git push", "gh pr merge 1 --squash"} {
		s.allowedBy(t, c, "super-yolo", sgOpts{}, "sub-agent "+c)
		s.allowedBy(t, c, "super-yolo", top(), "top-level "+c)
	}
}

// What is refused stays refused: credentials, the always-refused set, a
// write into .git/, the grant and the mode commands. A command the guard
// cannot read is still asked, but in super-yolo, which asks nothing (L154,
// L155; every ask site: mode_ask_sites_test.go).
func TestModeEveryModeStillRefuses(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"normal", "yolo", "super-yolo"} {
		s := newSG(t)
		s.setMode(t, s.p, mode)
		for _, c := range []string{"rm .env", "git push --force", "git clean -fdx",
			"echo x > .git/config", "jhon grant push", "jhon mode yolo", "rm -rf /", "rm -rf .git"} {
			for _, o := range []sgOpts{{}, top()} {
				r := s.bash(t, c, o)
				code(t, r, 2, mode+": "+c)
				noDecision(t, r.Stdout, mode+": "+c)
			}
		}
		if mode == "super-yolo" {
			s.allowedBy(t, "echo (pwd)", mode, top(), mode+": an unreadable command passes")
		} else {
			matches(t, askedWhy(t, s.bash(t, "echo (pwd)", top()).Stdout, mode), `^unreadable: `, mode+": an unreadable command is asked")
		}
		w := newWriteEnv(t)
		code(t, support.Jhon(t, w.project, w.env, "mode", mode), 0, "jhon mode "+mode)
		for _, o := range []writeOpts{{}, {top: true}} {
			if r := w.write(t, filepath.Join(w.project, ".git", "config"), o); r.code != 2 || r.ask != nil {
				t.Errorf("%s: a write into .git/ is not refused: %+v", mode, r)
			}
		}
		read := map[string]any{"tool_name": "Read", "cwd": w.project, "tool_input": map[string]any{"file_path": filepath.Join(w.project, ".env")}}
		code(t, hook(t, w.env, "secrets-guard", []string{"--agent", "*"}, read), 2, mode+": secrets-guard reads .env")
		edit := map[string]any{"tool_name": "Edit", "cwd": w.project, "tool_input": map[string]any{"file_path": filepath.Join(w.project, ".env"), "old_string": "a", "new_string": "b"}}
		code(t, hook(t, w.env, "secrets-guard", []string{"--agent", "*"}, edit), 2, mode+": secrets-guard edits .env")
	}
}

func TestModeNormalIsTheGuardsAsTheyAre(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	check := func(label string) {
		t.Helper()
		sub := s.bash(t, "git push", sgOpts{})
		code(t, sub, 2, label+", sub-agent")
		matches(t, sub.Stderr, `push needs the owner's authorization`, label+", sub-agent")
		matches(t, askedWhy(t, s.bash(t, "git push", top()).Stdout, label), `^push: `, label+", top-level")
		w := s.bash(t, "git status", sgOpts{})
		code(t, w, 0, label+", git status")
		if w.Stdout != "" {
			t.Errorf("%s: an allowed call printed %q", label, w.Stdout)
		}
	}
	check("no mode ever set")
	s.setMode(t, s.p, "yolo")
	s.allowedBy(t, "git push", "yolo", sgOpts{}, "yolo")
	s.setMode(t, s.p, "normal")
	check("after jhon mode normal")
}

func TestModeCLI(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	r := support.Jhon(t, s.p, s.env, "mode")
	code(t, r, 0, "jhon mode")
	equalStr(t, r.Stdout, "normal for "+s.p+"\n", "no mode set")
	r = support.Jhon(t, filepath.Join(s.p, "src"), s.env, "mode", "yolo")
	code(t, r, 0, "jhon mode yolo")
	matches(t, r.Stdout, `^yolo for `+regexp.QuoteMeta(s.p)+` since \d{4}-\d\d-\d\dT[\d:.]+Z\n$`, "jhon mode yolo")
	var got map[string]any
	if err := json.Unmarshal([]byte(support.Jhon(t, s.other, s.env, "mode", "--json", "--root", s.p).Stdout), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got["mode"] != "yolo" || got["root"] != s.p || got["since"] == nil {
		t.Errorf("jhon mode --json = %v", got)
	}
	// The record carries no expiry: a mode set long ago is still on.
	file := filepath.Join(s.state, "jhon", "modes", rootKey(s.p)+".json")
	rec := support.Read(t, file)
	if strings.Contains(rec, "expires") {
		t.Errorf("a mode record carries an expiry: %s", rec)
	}
	support.Write(t, filepath.Dir(file), map[string]string{filepath.Base(file): `{"mode":"yolo","root":"` + s.p + `","since":"2000-01-01T00:00:00.000Z"}`})
	s.allowedBy(t, "git push", "yolo", sgOpts{}, "a mode set in 2000")
	// Another repository has its own mode.
	code(t, s.bash(t, "git push", sgOpts{cwd: s.other}), 2, "another repository")
	// Every change is in the history, the owner's.
	s.setMode(t, s.p, "normal")
	var modes []string
	for _, l := range strings.Split(strings.TrimSpace(support.Read(t, filepath.Join(s.state, "jhon", "modes", "history.jsonl"))), "\n") {
		var h map[string]any
		if err := json.Unmarshal([]byte(l), &h); err != nil {
			t.Fatal(err)
		}
		if h["root"] != s.p || h["by"] != "owner" || h["at"] == nil {
			t.Errorf("history line %s", l)
		}
		modes = append(modes, h["mode"].(string))
	}
	equalStr(t, strings.Join(modes, " "), "yolo normal", "history")
	r = support.Jhon(t, s.p, s.env, "mode", "--json")
	matches(t, r.Stdout, `^\{"mode":"normal","root":"`+regexp.QuoteMeta(s.p)+`","since":"`, "normal --json")
	// Usage errors write nothing.
	for _, args := range [][]string{{"mode", "turbo"}, {"mode", "yolo", "normal"}, {"mode", "--root"}, {"mode", "--for", "1h"}} {
		r := support.Jhon(t, s.p, s.env, args...)
		code(t, r, 2, strings.Join(args, " "))
		matches(t, r.Stderr, `^jhon mode: .*\n\nusage: jhon mode `, strings.Join(args, " "))
	}
	if r := support.Jhon(t, s.p, s.env, "mode", "yolo", "--root", "/"); r.Code != 2 || !strings.Contains(r.Stderr, "/ is not inside a project") {
		t.Errorf("outside a project: %d %s", r.Code, r.Stderr)
	}
}

func TestModeOnlyTheOwnerSetsIt(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range []string{"jhon mode yolo", "jhon mode super-yolo", "jhon mode normal", "jhon mode yolo --root .", "jhon mode --root . yolo",
		sgHome + "/.local/bin/jhon mode yolo", "env jhon mode yolo", "bash -c 'jhon mode yolo'", "sh <<'EOF'\njhon mode super-yolo\nEOF",
		"node -e \"require('child_process').execSync('jhon mode yolo')\"",
		"mkdir -p <STATE>/jhon/modes && echo '{}' > <STATE>/jhon/modes/k.json", "tee -a <STATE>/jhon/modes/history.jsonl",
		"node -e \"require('fs').writeFileSync('<STATE>/jhon/modes/k.json', '{}')\"", "rm -rf <STATE>/jhon/modes"} {
		c = strings.ReplaceAll(c, "<STATE>", s.state)
		for _, o := range []sgOpts{{}, top()} {
			r := s.bash(t, c, o)
			code(t, r, 2, c)
			noDecision(t, r.Stdout, c)
		}
	}
	r := s.bash(t, "jhon mode yolo", sgOpts{})
	matches(t, r.Stderr, `a mode is the owner's act; no agent sets one`, "jhon mode yolo")
	for _, c := range []string{"jhon mode", "jhon mode --json", "jhon mode --root .", "jhon mode --json --root .", "jhon mode --help"} {
		s.code(t, c, sgOpts{}, 0, c)
	}
	w := newWriteEnv(t)
	for _, f := range []string{filepath.Join(w.state, "jhon", "modes", "k.json"), filepath.Join(w.state, "jhon", "modes", "history.jsonl")} {
		for _, o := range []writeOpts{{}, {top: true}} {
			if r := w.write(t, f, o); r.code != 2 || r.ask != nil {
				t.Errorf("a write into the modes is not refused: %s %+v", f, r)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(s.state, "jhon", "modes")); err == nil {
		t.Error("an agent's refused call created the modes")
	}
}

// ownerActProbes are the ways an agent reaches `jhon <act>` or a state
// directory without naming jhon (L126): a program run from source, a
// copied, built or linked binary, a subprocess with the subcommand split in
// two (pyArgs, a Python list's items), and writes through a glob (glob,
// under the state's jhon/), a link, or a path concatenated from two halves.
func ownerActProbes(s sgEnv, act, pyArgs, glob string, halves [2]string) []string {
	st := filepath.Join(s.state, "jhon")
	return []string{
		"go run ./engine/cmd/jhon " + act,
		"go build -o /tmp/j ./engine/cmd/jhon && /tmp/j " + act,
		"cp build/jhon /tmp/x && /tmp/x " + act,
		"ln -s ~/.local/bin/jhon /tmp/jj; /tmp/jj " + act,
		"./jhon-dev " + act + " --root .",
		"cp /tmp/f.json " + st + "/" + glob,
		"echo '{}' > " + st + "/" + glob,
		"ln -s " + st + " /tmp/st; cp f /tmp/st/" + glob,
	}
}

// TestModeNoAgentSetsItByAnyName: whatever the program is called, its
// arguments `mode <a mode>` are the owner's act; and no write reaches the
// modes through a glob, a link, or a concatenated path.
func TestModeNoAgentSetsItByAnyName(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	probes := append(ownerActProbes(s, "mode yolo", "'mo'+'de','yolo'", "mo*/k.json", [2]string{"mo", "des/k.json"}),
		"go build -o /tmp/j ./engine/cmd/jhon && /tmp/j mode super-yolo", "/tmp/x mode normal", "/tmp/x mode --root . yolo", "/tmp/x mode yolo --json")
	for _, c := range probes {
		for _, o := range []sgOpts{{}, top()} {
			r := s.bash(t, c, o)
			code(t, r, 2, c)
			noDecision(t, r.Stdout, c)
		}
	}
	matches(t, s.bash(t, "/tmp/x mode yolo", sgOpts{}).Stderr, `a mode is the owner's act; no agent sets one`, "/tmp/x mode yolo")
	// A link made earlier, outside this command, is read on disk.
	mkdir(t, filepath.Join(s.state, "jhon"))
	link := filepath.Join(s.tmp, "st")
	if err := os.Symlink(filepath.Join(s.state, "jhon"), link); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"cp f " + link + "/mo*/k.json", "cp f " + link + "/modes/k.json", "echo x > " + link + "/m?des/history.jsonl"} {
		for _, o := range []sgOpts{{}, top()} {
			r := s.bash(t, c, o)
			code(t, r, 2, c)
			noDecision(t, r.Stdout, c)
		}
	}
	w := newWriteEnv(t)
	for _, f := range []string{filepath.Join(w.state, "jhon", "mo*", "k.json"), filepath.Join(w.state, "j*", "[m]odes", "history.jsonl")} {
		for _, o := range []writeOpts{{}, {top: true}} {
			if r := w.write(t, f, o); r.code != 2 || r.ask != nil {
				t.Errorf("a globbed write into the modes is not refused: %s %+v", f, r)
			}
		}
	}
	// Reading the mode, by any name, changes none.
	for _, c := range []string{"/tmp/x mode", "/tmp/x mode --json", "go run ./engine/cmd/jhon mode --json --root .", "/tmp/x mode --help"} {
		s.code(t, c, sgOpts{}, 0, c)
	}
	if _, err := os.Stat(filepath.Join(s.state, "jhon", "modes")); err == nil {
		t.Error("an agent's refused call created the modes")
	}
}

// The argument check matches the exact tokens `mode <a mode>` and
// `grant <a class>` in sequence, and nothing like them.
func TestModeArgumentCheckFalsePositives(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range []string{"git config core.mode normal", "echo mode", "echo grant", "chmod 644 src", "chmod u+x src", "chmod --mode=755 src",
		"docker run --mode yolo-test img", "install -m 644 src/a src/b", "mkdir -m 700 src/c", "grep -rn 'mode yolo' src", "rg mode src",
		"git log --grep 'grant push'", "echo 'jhon mode yolo is the owner act'", "python3 -c \"print('mode')\"", "/tmp/x grant --list", "/tmp/x grant --help",
		"/tmp/x mode yolo-test", "/tmp/x grant pushy", "cp a src/modes.json", "echo x > src/grants"} {
		s.code(t, c, sgOpts{}, 0, c)
	}
}

func TestModeCoversWorktrees(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	base := varTemp(t, "jhon-mode-wt-")
	repo := support.GitRepo(t, mkdir(t, filepath.Join(base, "repo")))
	wt := filepath.Join(base, "repo-worktrees", "feature")
	support.Git(t, repo, "worktree", "add", "-q", wt, "-b", "feature")
	s.setMode(t, repo, "yolo")
	s.allowedBy(t, "git push", "yolo", sgOpts{cwd: wt}, "the project's mode covers its worktree")
	s.setMode(t, repo, "normal")
	code(t, s.bash(t, "git push", sgOpts{cwd: wt}), 2, "normal again")
	// Set from the worktree, it is the project's.
	r := support.Jhon(t, wt, s.env, "mode", "super-yolo")
	code(t, r, 0, "jhon mode super-yolo")
	matches(t, r.Stdout, `^super-yolo for `+regexp.QuoteMeta(repo)+` since `, "from the worktree")
	s.allowedBy(t, "git reset --hard", "super-yolo", sgOpts{cwd: repo}, "the main checkout")
	code(t, s.bash(t, "git push", sgOpts{cwd: s.other}), 2, "no other repository")
}

// outside-write, vcs-guard's ask, is a yolo class.
func TestModeVcsGuardOutsideWrite(t *testing.T) {
	t.Parallel()
	w := newWriteEnv(t)
	out := "/etc/jhon.conf"
	if r := w.write(t, out, writeOpts{}); r.code != 2 {
		t.Fatalf("normal: an outside write is not refused: %+v", r)
	}
	code(t, support.Jhon(t, w.project, w.env, "mode", "yolo"), 0, "jhon mode yolo")
	for _, o := range []writeOpts{{}, {top: true}} {
		payload := map[string]any{"tool_name": "Write", "cwd": w.project, "permission_mode": "default", "tool_input": map[string]any{"file_path": out}}
		if !o.top {
			payload["agent_id"], payload["agent_type"], payload["permission_mode"] = "a1", "worker", "bypassPermissions"
		}
		r := hook(t, w.env, "vcs-guard", vcsDirs(t), payload)
		code(t, r, 0, "yolo outside write")
		matches(t, modeNote(t, r.Stdout), `^note \(not a refusal; the call ran\): allowed by the owner's yolo mode: outside-write: `, "yolo outside write")
	}
	// A Claude Code settings file stays the owner's, asked or refused.
	if r := w.write(t, filepath.Join(w.project, ".claude", "settings.json"), writeOpts{}); r.code != 2 {
		t.Errorf("yolo: a sub-agent wrote the settings: %+v", r)
	}
	if r := w.write(t, filepath.Join(w.project, ".claude", "settings.json"), writeOpts{top: true}); r.ask == nil {
		t.Errorf("yolo: the settings are not asked at the top level: %+v", r)
	}
}

// pre-tool, one agent's guards in one process, keeps a mode's note and the
// other guards' notes, and an ask over any note.
func TestModeNoteThroughPreTool(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	s.setMode(t, s.p, "yolo")
	payload := map[string]any{"tool_name": "Bash", "tool_input": cmd("git push"), "cwd": s.p, "session_id": "s", "agent_id": "a1", "agent_type": "jhon-builder", "permission_mode": "bypassPermissions"}
	// The note unwrapped (T128f: the wrap is on by default on Linux).
	r := hook(t, sandboxEnv(t, s, "off"), "pre-tool", []string{"--agent", "jhon-builder", "safety-guard"}, payload)
	code(t, r, 0, "pre-tool push")
	matches(t, modeNote(t, r.Stdout), `^note \(not a refusal; the call ran\): allowed by the owner's yolo mode: push: git push$`, "pre-tool push")
}

// Each call a mode allowed is in the modes' log, which jhon audit counts.
func TestModeAllowedCallsAreLogged(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	s.setMode(t, s.p, "yolo")
	s.allowedBy(t, "git push", "yolo", sgOpts{}, "push")
	s.setMode(t, s.p, "super-yolo")
	s.allowedBy(t, "git reset --hard", "super-yolo", sgOpts{}, "destroy")
	code(t, s.bash(t, "git status", sgOpts{}), 0, "status")
	var got []string
	for _, l := range strings.Split(strings.TrimSpace(support.Read(t, filepath.Join(s.state, "jhon", "modes", "allowed.jsonl"))), "\n") {
		var a map[string]any
		if err := json.Unmarshal([]byte(l), &a); err != nil {
			t.Fatal(err)
		}
		if a["root"] != s.p || a["guard"] != "safety-guard" || a["at"] == nil || a["session"] != "s" || a["agent"] != "jhon-builder" {
			t.Errorf("allowed line %s", l)
		}
		got = append(got, a["mode"].(string)+" "+a["class"].(string))
	}
	equalStr(t, strings.Join(got, ", "), "yolo push, super-yolo destroy", "allowed")
	home := realTemp(t)
	env := support.Env("HOME="+home, "XDG_STATE_HOME="+s.state, "TMPDIR="+s.tmp, "JHON_ROOT="+support.Root())
	r := support.Jhon(t, home, env, "audit", "--all")
	code(t, r, 0, "jhon audit --all")
	matches(t, r.Stdout, `(?m)^  allowed by mode: 1 \(yolo\), 1 \(super-yolo\)$`, "jhon audit --all")
	r = support.Jhon(t, home, env, "audit", "--all", "--json")
	matches(t, r.Stdout, `"mode_allowed": \{\s*"yolo": 1,\s*"super-yolo": 1\s*\}`, "jhon audit --all --json")
}

func TestStatusJSONCarriesMode(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	var got map[string]any
	status := func() string {
		t.Helper()
		got = nil
		env := append(append([]string{}, s.env...), "JHON_ROOT="+support.Root())
		if err := json.Unmarshal([]byte(support.Jhon(t, s.p, env, "status", "--json").Stdout), &got); err != nil {
			t.Fatal(err)
		}
		m, _ := got["mode"].(string)
		return m
	}
	equalStr(t, status(), "normal", "no mode")
	s.setMode(t, s.p, "super-yolo")
	equalStr(t, status(), "super-yolo", "super-yolo")
}

func equalStr(t *testing.T, got, want, label string) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %q, want %q", label, got, want)
	}
}

// The state directory that holds the grants and the modes is the owner's
// too: replacing it, or copying a tree into it, would plant a mode that
// history.jsonl never records. yolo passes outside writes, so without this
// a yolo agent could raise itself to super-yolo (re-verify of T38b).
func TestModeStateDirReplaceRefused(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	s.setMode(t, s.p, "yolo")
	j := filepath.Join(s.state, "jhon")
	fk := filepath.Join(s.tmp, "fk")
	for _, c := range []string{
		"mv " + j + " " + j + ".old",
		"mv " + fk + " " + j,
		"cp -rT " + fk + " " + j,
		"cp -r " + fk + "/. " + j + "/",
		"rsync -a " + fk + "/ " + j + "/",
		"ln -sfn " + fk + " " + j,
	} {
		for _, o := range []sgOpts{{}, top()} {
			r := s.bash(t, c, o)
			code(t, r, 2, c)
			noDecision(t, r.Stdout, c)
		}
	}
	// Reading it stays allowed.
	code(t, s.bash(t, "ls "+j, sgOpts{}), 0, "ls the state dir")
}
