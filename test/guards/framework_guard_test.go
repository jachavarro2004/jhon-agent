// framework-guard (docket AV2), the PreToolUse hook every Jhon agent
// carries, the orchestrator included: no Jhon agent writes the framework's
// source, its releases, the `current` pointer, the CLI link, the owned
// projections, or ~/.claude/CLAUDE.md, in any session. There is no
// framework-session exemption (docket AV2, amendment of 2026-10-04): the
// payload's cwd is not an unforgeable origin
// (test/testdata/spikes/claude-code-session-origin/record.json). A Bash
// write or git run there is the sandbox's, where they are read-only
// (T128f); on Bash the guard keeps the T81 rule: Jhon's own code is never
// run by hand.
//
// The guard is spawned as Claude Code runs it: the payload on stdin,
// `--agent <owner>` and the protected paths as arguments, here scratch roots.
// Exit 0 allows; exit 2 blocks with the reason on stderr.

package guards

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

type fwEnv struct {
	home, fw, releases, release, current, cli, claudeMd, agent, skill, command, project string
	protected                                                                           []string
}

// newFwEnv is a scratch HOME holding a framework source root, a release
// with `current` pointing at it, the CLI link, CLAUDE.md, one owned agent,
// skill, and command projection, and a project with a symlink into the
// source.
func newFwEnv(t *testing.T) fwEnv {
	t.Helper()
	home := realTemp(t)
	e := fwEnv{home: home, fw: filepath.Join(home, ".agents", "jhon")}
	mkdir(t, filepath.Join(e.fw, "hooks"))
	support.Write(t, e.fw, map[string]string{"src/tdd-evidence.ts": "export {};\n"})
	e.releases = filepath.Join(home, ".local", "share", "jhon", "releases")
	e.release = filepath.Join(e.releases, strings.Repeat("a", 40))
	support.Write(t, e.release, map[string]string{"build/jhon": "#!/bin/sh\n"})
	e.current = filepath.Join(home, ".local", "share", "jhon", "current")
	symlink(t, e.release, e.current)
	e.cli = filepath.Join(home, ".local", "bin", "jhon")
	mkdir(t, filepath.Dir(e.cli))
	symlink(t, filepath.Join(e.current, "build", "jhon"), e.cli)
	claude := filepath.Join(home, ".claude")
	mkdir(t, filepath.Join(claude, "commands"))
	e.claudeMd = filepath.Join(claude, "CLAUDE.md")
	e.agent = filepath.Join(claude, "agents", "general.md")
	support.Write(t, claude, map[string]string{"CLAUDE.md": "<!-- jhon:start -->\n<!-- jhon:end -->\n", "agents/general.md": "---\nname: general\n---\n"})
	e.command = filepath.Join(claude, "commands", "jhon-ldd.md")
	symlink(t, filepath.Join(e.current, "commands", "jhon-ldd.md"), e.command)
	e.skill = filepath.Join(home, ".agents", "skills", "ldd")
	support.Write(t, e.skill, map[string]string{"SKILL.md": "skill\n"})
	e.project = filepath.Join(home, "Projects", "work", "acme_app")
	mkdir(t, filepath.Join(e.project, "src"))
	symlink(t, e.fw, filepath.Join(e.project, "fwlink"))
	e.protected = []string{e.fw, e.releases, e.current, e.cli, e.claudeMd, e.agent, e.skill, e.command}
	return e
}

type fwWho struct{ owner, caller, cwd string }

func (e fwEnv) guard(t *testing.T, tool string, input map[string]any, who fwWho) support.Result {
	t.Helper()
	if who.owner == "" {
		who.owner = "general"
	}
	caller := who.caller
	if caller == "-" {
		caller = ""
	} else if caller == "" {
		caller = who.owner
	}
	if who.cwd == "" {
		who.cwd = e.project
	}
	payload := map[string]any{"tool_name": tool, "cwd": who.cwd, "tool_input": input, "agent_type": caller}
	return hook(t, support.Env("HOME="+e.home), "framework-guard", append([]string{"--agent", who.owner}, e.protected...), payload)
}

func fwRefused(t *testing.T, r support.Result, label string) {
	t.Helper()
	if r.Code != 2 {
		t.Errorf("%s: expected a block, got %d: %s", label, r.Code, r.Stderr)
		return
	}
	matches(t, r.Stderr, `^framework-guard: `, label)
}

func fwAllowed(t *testing.T, r support.Result, label string) { t.Helper(); code(t, r, 0, label) }

func write(path string) map[string]any  { return map[string]any{"file_path": path, "content": "x"} }
func cmd(command string) map[string]any { return map[string]any{"command": command} }

// ── the incident (session 6506479f) ─────────────────────────────────────────

func TestFrameworkGuardIncidentEdit(t *testing.T) {
	t.Parallel()
	e := newFwEnv(t)
	fwRefused(t, e.guard(t, "Edit", map[string]any{"file_path": filepath.Join(e.fw, "src", "tdd-evidence.ts"), "old_string": "export", "new_string": "x"}, fwWho{}), "Edit")
}

func TestFrameworkGuardIncidentVerbatimPaths(t *testing.T) {
	t.Parallel()
	// Pure path arithmetic: the guard writes nothing and reads only the payload.
	const real = "/home/owner/.agents/jhon"
	run := func(tool string, input map[string]any, owner string) support.Result {
		return hook(t, nil, "framework-guard", []string{"--agent", owner, real},
			map[string]any{"tool_name": tool, "cwd": "/home/owner/Projects/work/acme_app", "tool_input": input, "agent_type": owner})
	}
	code(t, run("Edit", map[string]any{"file_path": real + "/src/tdd-evidence.ts", "old_string": "a", "new_string": "b"}, "general"), 2, "edit")
}

// ── Edit and Write ──────────────────────────────────────────────────────────

func TestFrameworkGuardEditWriteUnderSource(t *testing.T) {
	t.Parallel()
	e := newFwEnv(t)
	target := filepath.Join(e.fw, "src", "tdd-evidence.ts")
	fwRefused(t, e.guard(t, "Write", write(target), fwWho{}), "absolute")
	fwRefused(t, e.guard(t, "Write", write("../../../.agents/jhon/src/tdd-evidence.ts"), fwWho{}), "relative")
	fwRefused(t, e.guard(t, "Write", write(filepath.Join(e.project, "src", "..", "..", "..", "..", ".agents", "jhon", "x.ts")), fwWho{}), "dotted")
	fwRefused(t, e.guard(t, "Edit", map[string]any{"file_path": filepath.Join(e.project, "fwlink", "src", "tdd-evidence.ts"), "old_string": "a", "new_string": "b"}, fwWho{}), "symlinked")
	fwRefused(t, e.guard(t, "Write", write(filepath.Join(e.project, "fwlink", "new", "deep", "x.ts")), fwWho{}), "a new file under a symlinked ancestor")
	fwRefused(t, e.guard(t, "Write", map[string]any{"file_path": target}, fwWho{cwd: e.fw}), "from a cwd inside the root: no exemption")
}

func TestFrameworkGuardActivationSurfaces(t *testing.T) {
	t.Parallel()
	e := newFwEnv(t)
	fwRefused(t, e.guard(t, "Write", write(filepath.Join(e.release, "hooks", "x.mjs")), fwWho{}), "a release")
	fwRefused(t, e.guard(t, "Write", write(filepath.Join(e.current, "hooks", "x.mjs")), fwWho{}), "through current")
	fwRefused(t, e.guard(t, "Write", write(e.current), fwWho{}), "current itself")
	fwRefused(t, e.guard(t, "Write", write(e.cli), fwWho{}), "the CLI link")
	fwRefused(t, e.guard(t, "Edit", map[string]any{"file_path": e.claudeMd, "old_string": "jhon", "new_string": "x"}, fwWho{}), "CLAUDE.md")
	fwRefused(t, e.guard(t, "Write", write(e.agent), fwWho{}), "an owned agent")
	fwRefused(t, e.guard(t, "Write", write(filepath.Join(e.skill, "SKILL.md")), fwWho{}), "an owned skill")
	fwRefused(t, e.guard(t, "Write", write(e.command), fwWho{}), "an owned command")
}

func TestFrameworkGuardOutsideAllowed(t *testing.T) {
	t.Parallel()
	e := newFwEnv(t)
	fwAllowed(t, e.guard(t, "Write", write(filepath.Join(e.project, "src", "a.ts")), fwWho{}), "the project")
	fwAllowed(t, e.guard(t, "Write", write(e.fw+"-worktrees/av/src/a.ts"), fwWho{}), "a sibling sharing the prefix")
	fwAllowed(t, e.guard(t, "Write", map[string]any{"file_path": filepath.Join(e.home, ".claude", "settings.json"), "content": "{}"}, fwWho{}), "another ~/.claude file")
	fwAllowed(t, e.guard(t, "Write", write(filepath.Join(e.home, ".claude", "agents", "mine.md")), fwWho{}), "an agent Jhon does not own")
	fwAllowed(t, e.guard(t, "Read", map[string]any{"file_path": filepath.Join(e.fw, "src", "tdd-evidence.ts")}, fwWho{}), "a read")
}

// ── Bash: the T81 rule ──────────────────────────────────────────────────────

// A script run from a file is code the guard cannot read: handed a protected
// path, or run inside a protected directory, it may run Jhon's own code. A
// worker wrote the framework's source this way (2026-10-29): its script in
// /tmp, the file as its argument. A path only in the text a heredoc writes
// is no script's argument (T152, L1311).
func TestFrameworkGuardScriptsFromFiles(t *testing.T) {
	t.Parallel()
	e := newFwEnv(t)
	for _, command := range []string{
		"mkdir -p /tmp/x && cat > /tmp/x/t.py <<'PY'\nimport sys\nopen(sys.argv[1],'w').write('x')\nPY\npython3 -I /tmp/x/t.py " + e.fw + "/src/tdd-evidence.ts",
		"node /tmp/fix.js " + e.fw + "/src/a.ts",
		"cd " + e.fw + " && python3 /tmp/fix.py src/a.ts",
		"cd " + e.fw + " && python3 /tmp/fix.py",
		"bash /tmp/fix.sh " + e.fw + "/README.md",
		"perl /tmp/fix.pl " + e.claudeMd,
	} {
		fwRefused(t, e.guard(t, "Bash", cmd(command), fwWho{}), command)
	}
	for _, command := range []string{
		"python3 /tmp/fix.py src/a.ts",
		"node --experimental-strip-types --test test/*.test.ts",
		"bash scripts/build.sh",
		"python3 -c 'print(1)'",
		"cat > /tmp/t.py <<'PY'\nopen('" + e.fw + "/src/a.ts','w').write('x')\nPY\npython3 /tmp/t.py",
	} {
		fwAllowed(t, e.guard(t, "Bash", cmd(command), fwWho{}), command)
	}
}

func TestFrameworkGuardBashReadsAllowed(t *testing.T) {
	t.Parallel()
	e := newFwEnv(t)
	for _, command := range []string{
		"cat " + e.fw + "/src/tdd-evidence.ts",
		"rg export " + e.fw,
		"git status && git log --oneline -3",
		"git -C " + e.project + " diff",
		"echo x > src/a.ts",
		"sed -i s/a/b/ " + e.fw + "-worktrees/av/x.ts",
	} {
		fwAllowed(t, e.guard(t, "Bash", cmd(command), fwWho{}), command)
	}
}

func TestFrameworkGuardScopedAndFailsClosed(t *testing.T) {
	t.Parallel()
	e := newFwEnv(t)
	fwAllowed(t, e.guard(t, "Write", write(e.agent), fwWho{owner: "jhon-orchestrator", caller: "worker"}), "another agent's call")
	fwRefused(t, e.guard(t, "Write", write(e.agent), fwWho{owner: "jhon-orchestrator", caller: "-"}), "the main session")
	bare := hook(t, nil, "framework-guard", []string{"--agent", "general"}, map[string]any{"tool_name": "Write", "tool_input": map[string]any{"file_path": "/x"}})
	code(t, bare, 2, "no protected paths: the render is broken")
	code(t, hook(t, nil, "framework-guard", []string{"--agent", "general", "/x"}, "not json"), 2, "not json")
}

// ── verified bypasses (harness safety commit): inline interpreter deletes ──
// Interpreter code naming a protected path stays refused (T81); a plain
// program's write there is the sandbox's (T128f).

func TestFrameworkGuardInterpreterDeletes(t *testing.T) {
	t.Parallel()
	e := newFwEnv(t)
	for _, command := range []string{
		`python3 -c "open('` + e.fw + `/src/tdd-evidence.ts','w').write('x')"`,
		`node -e "require('fs').unlinkSync('` + e.fw + `/src/tdd-evidence.ts')"`,
		`node -e "require('fs').rmSync('` + e.fw + `/src', { recursive: true })"`,
		`python3 -c "import os; os.remove('` + e.fw + `/src/tdd-evidence.ts')"`,
		`python3 -c "import shutil; shutil.rmtree('` + e.fw + `/src')"`,
	} {
		fwRefused(t, e.guard(t, "Bash", cmd(command), fwWho{}), command)
	}
}

// pi's calls name no agent, so its framework guard runs with the owner `*`,
// which judges every agent's call.
func TestFrameworkGuardStarOwner(t *testing.T) {
	t.Parallel()
	e := newFwEnv(t)
	fwRefused(t, e.guard(t, "Write", write(e.agent), fwWho{owner: "*", caller: "worker"}), "a role's call under *")
	fwRefused(t, e.guard(t, "Write", write(e.agent), fwWho{owner: "*", caller: "-"}), "the main thread under *")
}

// T81 (L596): an orchestrator launched sub-agents by importing Jhon's own
// shell/lib/agents.ts from inline node code, outside every Jhon tool and
// invisible to jhon audit. Interpreter code that names a protected path is
// refused, naming the subagent tool; reading the files stays allowed.
func TestFrameworkGuardJhonInternalsByHand(t *testing.T) {
	t.Parallel()
	e := newFwEnv(t)
	agents := e.release + "/shell/lib/agents.ts"
	for _, command := range []string{
		`node --experimental-strip-types --input-type=module -e "import { runAgent } from '` + agents + `'; await runAgent()"`,
		`node --experimental-strip-types -e "require('` + e.current + `/shell/lib/agents.ts')"`,
		"node --experimental-strip-types --input-type=module <<'EOF'\nimport { runAgent } from '" + agents + "';\nawait runAgent();\nEOF",
		`python3 -c "import runpy; runpy.run_path('` + e.fw + `/lib/x.py')"`,
		"node --experimental-strip-types " + agents,
	} {
		r := e.guard(t, "Bash", cmd(command), fwWho{})
		fwRefused(t, r, command)
		matches(t, r.Stderr, "subagent tool", command)
	}
	for _, command := range []string{
		"cat " + agents,
		`node -e "console.log(1)"`,
		"node --experimental-strip-types --test test/*.test.ts",
	} {
		fwAllowed(t, e.guard(t, "Bash", cmd(command), fwWho{}), command)
	}
}

// T152 (L1311): the T81 rule reads what a Bash line visibly runs. A path the
// line names elsewhere (a reader, a heredoc it writes, a function
// substitution's text) is no script's: the line passes. Interpreter code
// naming a protected path, or a script run on one, stays refused. Through
// `jhon hook pre-tool`, as the runtimes call it.
func TestT152FrameworkGuardReadsTheVisibleProgram(t *testing.T) {
	t.Parallel()
	e := newFwEnv(t)
	pre := func(command string) support.Result {
		payload := map[string]any{"tool_name": "Bash", "cwd": e.project, "tool_input": cmd(command), "agent_type": "general"}
		return hook(t, support.Env("HOME="+e.home), "pre-tool", append([]string{"--agent", "*", "framework-guard"}, e.protected...), payload)
	}
	a := e.fw + "/src/a.ts"
	for _, command := range []string{
		"cat > /tmp/t.py <<'PY'\nopen('" + a + "','w').write('x')\nPY\npython3 /tmp/t.py",
		"cat " + a + " | python3 /tmp/t.py",
		"echo " + a + " > /tmp/p.txt && python3 /tmp/t.py",
		"python3 /tmp/t.py && bash -c 'cat " + a + "'",
		"x=${ echo " + a + "; }; python3 /tmp/t.py",
		"ls -d " + a + "; ${ echo python3 /tmp/t.py; }",
		"python3 -m unittest discover -s tests && cat " + e.current + "/prose/contracts/ldd.md",
	} {
		fwAllowed(t, pre(command), command)
	}
	for _, command := range []string{
		`node --experimental-strip-types -e "require('` + e.current + `/shell/lib/agents.ts')"`,
		"python3 " + e.fw + "/lib/x.py",
		"cd " + e.fw + " && python3 x.py",
	} {
		r := pre(command)
		fwRefused(t, r, command)
		matches(t, r.Stderr, "subagent tool", command)
	}
}
