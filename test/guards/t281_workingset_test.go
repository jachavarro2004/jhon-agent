// T281 (S12): several repositories at once. The session's working set is
// the project, its worktrees, the listed and joined members and the
// sandbox.allow_write folders; inside it nothing is "outside the project".
// A repository outside the set is judged as before, a joined sibling the
// primary does not list is said with the entry that lists it, and what S10
// keeps refused stays refused inside every repository of the set.

package guards

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// wsSet is a scratch working set: app, the primary, lists lib (joined) and
// web (not joined); site joined app but is not listed; shared is a
// repository and media a folder holding the repository inner, both named
// by sandbox.allow_write; other is declared nowhere.
type wsSet struct {
	scratch, app, lib, web, site, shared, media, other, state string
	env                                                       []string
}

func newWSSet(t *testing.T) wsSet {
	t.Helper()
	scratch := outsideTemp(t)
	at := func(n string) string { return filepath.Join(scratch, n) }
	w := wsSet{scratch: scratch, app: at("app"), lib: at("lib"), web: at("web"), site: at("site"), shared: at("shared"),
		media: at("media"), other: at("other"), state: at("state")}
	settings := at("settings.toml")
	support.Write(t, scratch, map[string]string{"settings.toml": "[sandbox]\nmode = \"off\"\nallow_write = [\"" + w.shared + "\", \"" + w.media + "\"]\n"})
	w.env = support.Env("HOME="+sgHome, "XDG_STATE_HOME="+w.state, "TMPDIR="+mkdir(t, at("tmp")), "JHON_ROOT="+support.Root(),
		"JHON_SETTINGS="+settings, "GOCACHE="+goCache(t))
	for _, d := range []string{w.app, w.lib, w.web, w.site, w.shared, w.other, filepath.Join(w.media, "inner")} {
		support.Write(t, d, map[string]string{"src/a.py": "X = 1\n", ".env": "KEY=v\n"})
		support.GitRepo(t, d)
	}
	support.Write(t, w.media, map[string]string{"clips/a.txt": "x\n"})
	support.Write(t, w.app, map[string]string{"openspec/config.yaml": "workspace:\n  lib: ../lib\n  web: ../web\n"})
	for _, d := range []string{w.lib, w.site} {
		code(t, support.Jhon(t, d, w.env, "workspace", "join", w.app), 0, "jhon workspace join in "+d)
	}
	return w
}

// call is one call through the plugin's pre-tool, made in cwd: at the top
// level (the owner can be asked) or from a sub-agent.
func (w wsSet) call(t *testing.T, top bool, cwd, tool string, input map[string]any) support.Result {
	t.Helper()
	payload := map[string]any{"hook_event_name": "PreToolUse", "tool_name": tool, "tool_input": input, "cwd": cwd, "session_id": "s",
		"permission_mode": "default"}
	if !top {
		payload["permission_mode"], payload["agent_id"], payload["agent_type"] = "bypassPermissions", "a1", "worker"
	}
	return hook(t, w.env, "pre-tool", []string{"--agent", "*", "safety-guard", "vcs-guard", ".git", "secrets-guard"}, payload)
}

func edit(p string) map[string]any {
	return map[string]any{"file_path": p, "old_string": "1", "new_string": "2"}
}

// Inside the set nothing is outside the project: an Edit, a Write and a
// removal in a repository or folder of sandbox.allow_write pass with no
// note, from the primary and from a member, as in the project itself.
func TestT281InsideTheWorkingSetNothingIsOutside(t *testing.T) {
	t.Parallel()
	w := newWSSet(t)
	for _, cwd := range []string{w.app, w.lib} {
		for _, c := range []struct {
			tool  string
			input map[string]any
		}{
			{"Edit", edit(filepath.Join(w.shared, "src", "a.py"))},
			{"Write", map[string]any{"file_path": filepath.Join(w.media, "clips", "b.txt"), "content": "y\n"}},
			{"Write", map[string]any{"file_path": filepath.Join(w.media, "new", "c.txt"), "content": "y\n"}},
			{"Edit", edit(filepath.Join(w.lib, "src", "a.py"))},
			{"Bash", cmd("rm -r " + filepath.Join(w.shared, "src"))},
			{"Bash", cmd("rm -rf " + filepath.Join(w.media, "clips"))},
			{"Bash", cmd("cd " + w.shared + " && rm -rf build")},
		} {
			label := "from " + filepath.Base(cwd) + ": " + c.tool + " " + js(t, c.input)
			r := w.call(t, false, cwd, c.tool, c.input)
			code(t, r, 0, label)
			noDecision(t, r.Stdout, label)
			if note := adviceOf(t, r.Stdout); note != "" {
				t.Errorf("%s: a note inside the working set: %q", label, note)
			}
		}
	}
}

// Outside the set, as before: an Edit in a repository declared nowhere, a
// listed member that has not joined, or a joined sibling the primary does
// not list asks at the top level and is refused to a sub-agent, and a
// removal there is refused; the sibling's stop says why and the entry
// that lists it.
func TestT281OutsideTheWorkingSetIsJudgedAsBefore(t *testing.T) {
	t.Parallel()
	w := newWSSet(t)
	note := w.site + " joined the workspace of " + w.app + ", whose LDD config lists no path to it, so it is no member and stays outside the session's working set; to make it one, add `site: ../site` under `workspace:` in " +
		filepath.Join(w.app, "openspec", "config.yaml") + " (or the owner runs `jhon workspace leave` in it)"
	for _, repo := range []string{w.other, w.web, w.site} {
		file := filepath.Join(repo, "src", "a.py")
		label := "Edit in " + filepath.Base(repo)
		r := w.call(t, true, w.app, "Edit", edit(file))
		code(t, r, 0, label+" (top level)")
		why := askedWhy(t, r.Stdout, label)
		matches(t, why, "^"+regexp.QuoteMeta("outside-write: Edit of "+file+" lands outside the project ("+w.app+") and the scratch roots"), label)
		if repo == w.site && !strings.HasSuffix(why, "; "+note) {
			t.Errorf("%s: the ask does not name the unlisted sibling:\n %s\nwant the suffix\n %s", label, why, note)
		}
		if repo != w.site && strings.Contains(why, "joined the workspace") {
			t.Errorf("%s: the ask names a sibling: %s", label, why)
		}
		r = w.call(t, false, w.app, "Edit", edit(file))
		code(t, r, 2, label+" (sub-agent)")
		if repo == w.site && !strings.Contains(r.Stderr, note) {
			t.Errorf("%s: the refusal does not name the unlisted sibling: %s", label, r.Stderr)
		}
		r = w.call(t, false, w.app, "Bash", cmd("rm -r "+filepath.Join(repo, "src")))
		code(t, r, 2, "rm -r in "+filepath.Base(repo))
		matches(t, r.Stderr, regexp.QuoteMeta("it is outside the project and the scratch roots"), "rm -r in "+filepath.Base(repo))
	}
}

// What S10 keeps refused stays refused in every repository of the set: the
// top of a member, an allow_write folder itself and a repository inside
// one are never removed; a credentials file, a .git config or hook, and
// Claude Code's settings are judged exactly as in the project.
func TestT281TheHardLineHoldsInsideTheWorkingSet(t *testing.T) {
	t.Parallel()
	w := newWSSet(t)
	inner := filepath.Join(w.media, "inner")
	for _, c := range []struct{ line, why string }{
		{"rm -rf " + w.lib, "it reaches the project root"},
		{"rm -rf " + w.shared, "it reaches the project root"},
		{"rm -rf " + w.media, "it reaches the project root"},
		{"rm -rf " + inner, "it reaches the project root"},
		{"rm -rf " + w.app, "it reaches the project root"},
		{"cat " + filepath.Join(w.shared, ".env"), ".env holds credentials"},
		{"cat " + filepath.Join(w.lib, ".env"), ".env holds credentials"},
		{"echo x > " + filepath.Join(w.shared, ".git", "config"), "it writes into the VCS directory .git"},
	} {
		r := w.call(t, true, w.app, "Bash", cmd(c.line))
		code(t, r, 2, c.line)
		matches(t, r.Stderr, regexp.QuoteMeta(c.why), c.line)
	}
	// The file tools: each answer in a repository of the set is the answer
	// the same call gets in the project itself, the repository's path aside.
	for _, repo := range []string{w.shared, w.lib, inner} {
		for _, c := range []struct {
			tool, rel string
		}{
			{"Read", ".env"}, {"Edit", ".env"}, {"Write", ".git/config"}, {"Write", ".git/hooks/pre-commit"}, {"Write", ".claude/settings.json"},
		} {
			input := func(root string) map[string]any {
				return map[string]any{"file_path": filepath.Join(root, c.rel), "content": "x", "old_string": "a", "new_string": "b"}
			}
			for _, top := range []bool{true, false} {
				here, there := w.call(t, top, w.app, c.tool, input(w.app)), w.call(t, top, w.app, c.tool, input(repo))
				label := c.tool + " " + c.rel + " in " + filepath.Base(repo)
				if here.Code != there.Code || strings.ReplaceAll(there.Stdout+there.Stderr, repo, "<R>") != strings.ReplaceAll(here.Stdout+here.Stderr, w.app, "<R>") {
					t.Errorf("%s (top %v): %d %q %q; in the project: %d %q %q", label, top, there.Code, there.Stdout, there.Stderr, here.Code, here.Stdout, here.Stderr)
				}
				if here.Code == 0 && here.Stdout == "" {
					t.Errorf("%s: passes silently in the project", label)
				}
			}
		}
	}
}

// A path is judged by where it leads: a link into an allow_write folder is
// inside, a link to a repository declared nowhere is outside, and an
// allow_write folder that is a link opens nothing.
func TestT281ALinkIsJudgedByItsRealPath(t *testing.T) {
	t.Parallel()
	w := newWSSet(t)
	into, via := filepath.Join(w.scratch, "into"), filepath.Join(w.scratch, "via")
	symlink(t, w.shared, into)
	symlink(t, w.other, via)
	r := w.call(t, false, w.app, "Edit", edit(filepath.Join(into, "src", "a.py")))
	code(t, r, 0, "a link into the shared repository")
	r = w.call(t, false, w.app, "Edit", edit(filepath.Join(via, "src", "a.py")))
	code(t, r, 2, "a link to a repository declared nowhere")
	support.Write(t, w.scratch, map[string]string{"settings.toml": "[sandbox]\nmode = \"off\"\nallow_write = [\"" + via + "\"]\n"})
	r = w.call(t, false, w.app, "Edit", edit(filepath.Join(w.other, "src", "a.py")))
	code(t, r, 2, "an allow_write folder that is a link")
}

// A bounded writer's surfaces may name a path that exists only in a folder
// of the working set; one that matches nothing anywhere still stops the
// launch, as before.
func TestT281AWritersSurfacesMayNameAPathOfTheWorkingSet(t *testing.T) {
	t.Parallel()
	w := newWSSet(t)
	launch := func(surface string) support.Result {
		payload := map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Agent", "cwd": w.app, "session_id": "s", "permission_mode": "default",
			"tool_input": map[string]any{"subagent_type": "worker", "prompt": "Do it.\n\n## Allowed edit surfaces\n- " + surface + "\n"}}
		return hook(t, w.env, "route-guard", []string{"--agent", "*", "worker"}, payload)
	}
	code(t, launch("clips/a.txt"), 0, "a surface that exists in an allow_write folder")
	r := launch("nowhere/a.txt")
	code(t, r, 2, "a surface that matches nothing")
	matches(t, r.Stderr, regexp.QuoteMeta("nowhere/a.txt (its folder nowhere does not exist), which matches nothing in "+w.app), "a surface that matches nothing")
}

// The joined sibling the primary does not list is said where the owner
// looks: `jhon workspace list` under its pointer, and `jhon doctor` in the
// primary and in the sibling; a member is not.
func TestT281TheUnlistedSiblingIsSaidWhereTheOwnerLooks(t *testing.T) {
	t.Parallel()
	w := newWSSet(t)
	note := w.site + " joined the workspace of " + w.app + ", whose LDD config lists no path to it, so it is no member and stays outside the session's working set; to make it one, add `site: ../site` under `workspace:` in " +
		filepath.Join(w.app, "openspec", "config.yaml") + " (or the owner runs `jhon workspace leave` in it)"
	r := support.Jhon(t, w.lib, w.env, "workspace", "list")
	code(t, r, 0, "jhon workspace list")
	matches(t, r.Stdout, "(?m)^"+regexp.QuoteMeta(w.site)+`  -> `+regexp.QuoteMeta(w.app)+`  \(joined \S+\)\n  warning: `+regexp.QuoteMeta(note)+"$", "list: the sibling")
	if strings.Count(r.Stdout, "warning: ") != 1 {
		t.Errorf("list warns on a member too:\n%s", r.Stdout)
	}
	for _, dir := range []string{w.app, w.site} {
		r = support.Jhon(t, dir, w.env, "doctor")
		if !strings.Contains(r.Stdout, "  warn  "+note+"\n") {
			t.Errorf("doctor in %s does not name the sibling:\n%s", dir, r.Stdout)
		}
	}
	if r = support.Jhon(t, w.lib, w.env, "doctor"); strings.Contains(r.Stdout, "joined the workspace of") {
		t.Errorf("doctor in a member names a sibling:\n%s", r.Stdout)
	}
}

// A project with no workspace and no allow_write is judged as before.
func TestT281AProjectWithNoWorkspaceIsJudgedAsBefore(t *testing.T) {
	t.Parallel()
	w := newWSSet(t)
	if err := os.WriteFile(filepath.Join(w.scratch, "settings.toml"), []byte("[sandbox]\nmode = \"off\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := w.call(t, false, w.other, "Edit", edit(filepath.Join(w.other, "src", "a.py")))
	code(t, r, 0, "an Edit in the project")
	for _, repo := range []string{w.shared, w.app} {
		r = w.call(t, false, w.other, "Edit", edit(filepath.Join(repo, "src", "a.py")))
		code(t, r, 2, "an Edit in "+filepath.Base(repo)+" from a project with no workspace")
		matches(t, r.Stderr, regexp.QuoteMeta("lands outside the project ("+w.other+")"), "outside")
	}
}
