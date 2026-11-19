// vcs-guard, the PreToolUse hook that refuses an Edit or Write whose path
// has a segment naming a VCS directory (contracts/vocabulary.yaml,
// `vcs_dirs`), for every agent that can write.
//
// The guard is spawned exactly as Claude Code runs it: the hook payload on
// stdin, `--agent <owner>` and the directory names as arguments. Exit 0
// allows; exit 2 blocks.

package guards

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

const vcsRepo = "/home/u/project"

func vcsDirs(t *testing.T) []string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^vcs_dirs:\s*\[(.*)\]`).FindStringSubmatch(support.Read(t, support.Path("prose", "contracts", "vocabulary.yaml")))
	if m == nil {
		t.Fatal("vocabulary.yaml declares no vcs_dirs")
	}
	var dirs []string
	for _, s := range strings.Split(m[1], ",") {
		dirs = append(dirs, strings.TrimSpace(s))
	}
	return dirs
}

// vcsCall is a call by agentType (omitted when nil) to a guard owned by owner.
func vcsCall(t *testing.T, owner, filePath string, agentType *string, tool, cwd string) support.Result {
	t.Helper()
	payload := map[string]any{"tool_name": tool, "cwd": cwd, "tool_input": map[string]any{"file_path": filePath}}
	if agentType != nil {
		payload["agent_type"] = *agentType
	}
	return hook(t, nil, "vcs-guard", append([]string{"--agent", owner}, vcsDirs(t)...), payload)
}

func TestVcsGuardVocabulary(t *testing.T) {
	t.Parallel()
	if d := vcsDirs(t); !reflect.DeepEqual(d, []string{".git", ".hg", ".svn"}) {
		t.Errorf("vcs_dirs = %q", d)
	}
}

func TestVcsGuardRefuses(t *testing.T) {
	t.Parallel()
	for _, c := range [][2]string{
		{"general", vcsRepo + "/.git/config"},
		{"general", ".git/config"},
		{"worker", "x/.git/HEAD"},
		{"worker", vcsRepo + "/x/.git/HEAD"},
		{"general", vcsRepo + "/.git"}, // a linked worktree's .git file
		{"general", vcsRepo + "/.hg/hgrc"},
		{"general", vcsRepo + "/.svn/entries"},
		{"general", vcsRepo + "/sub/.svn/wc.db"},
		{"general", vcsRepo + "/src/../.git/config"}, // resolves into .git
		{"general", vcsRepo + "/.GIT/config"},        // case-insensitive filesystems map this to .git
		{"jhon-orchestrator", vcsRepo + "/.git/openspec/changes/x/research.md"},
		{"explore", vcsRepo + "/.git/openspec/changes/x/proposal.md"},
	} {
		agent, path := c[0], c[1]
		t.Run(agent+" may not write "+path, func(t *testing.T) {
			t.Parallel()
			for _, tool := range []string{"Write", "Edit"} {
				r := vcsCall(t, agent, path, &agent, tool, vcsRepo)
				if r.Code != 2 {
					t.Errorf("%s: expected a block, got %d", tool, r.Code)
					continue
				}
				matches(t, r.Stderr, `^vcs-guard: `, tool)
			}
		})
	}
}

func TestVcsGuardAllows(t *testing.T) {
	t.Parallel()
	// A write in the project needs a project: a git repository (T57a).
	repo := realTemp(t)
	support.GitRepo(t, repo)
	for _, c := range [][2]string{
		{"general", vcsRepo + "/src/app.py"},
		{"general", "src/app.py"},
		{"worker", vcsRepo + "/.github/workflows/ci.yml"}, // a prefix of `.git` is not `.git`
		{"worker", vcsRepo + "/.gitignore"},
		{"worker", vcsRepo + "/docs/git/guide.md"},
		{"explore", vcsRepo + "/openspec/changes/x/exploration.md"},
		{"worker", vcsRepo + "/openspec/config.yaml"},
	} {
		agent, path := c[0], c[1]
		t.Run(agent+" may write "+path, func(t *testing.T) {
			t.Parallel()
			path := strings.Replace(path, vcsRepo, repo, 1)
			code(t, vcsCall(t, agent, path, &agent, "Write", repo), 0, path)
		})
	}
}

func TestVcsGuardSymlinkIntoVcsDir(t *testing.T) {
	t.Parallel()
	root := realTemp(t)
	mkdir(t, filepath.Join(root, ".git"))
	symlink(t, filepath.Join(root, ".git"), filepath.Join(root, "meta"))
	general := "general"
	code(t, vcsCall(t, "general", filepath.Join(root, "meta", "config"), &general, "Write", root), 2, "absolute")
	code(t, vcsCall(t, "general", "meta/hooks/pre-commit", &general, "Write", root), 2, "relative")
}

// ── owner scoping, as the other guards (9c7e390) ────────────────────────────

func TestVcsGuardAnotherAgentPasses(t *testing.T) {
	t.Parallel()
	code(t, vcsCall(t, "jhon-orchestrator", vcsRepo+"/.git/config", ptr("general"), "Write", vcsRepo), 0, "another agent")
}

func TestVcsGuardAbsentAgentTypeJudged(t *testing.T) {
	t.Parallel()
	code(t, vcsCall(t, "general", vcsRepo+"/.git/config", nil, "Write", vcsRepo), 2, "absent")
	code(t, vcsCall(t, "general", vcsRepo+"/.git/config", ptr(""), "Write", vcsRepo), 2, "empty")
}

func TestVcsGuardAgentWithoutNameFailsClosed(t *testing.T) {
	t.Parallel()
	code(t, hook(t, nil, "vcs-guard", []string{"--agent"}, map[string]any{"tool_name": "Write", "agent_type": "general", "cwd": vcsRepo,
		"tool_input": map[string]any{"file_path": vcsRepo + "/src/a.py"}}), 2, "--agent")
}

func TestVcsGuardNoDirectoryListFailsClosed(t *testing.T) {
	t.Parallel()
	code(t, hook(t, nil, "vcs-guard", []string{"--agent", "general"}, map[string]any{"tool_name": "Write", "agent_type": "general", "cwd": vcsRepo,
		"tool_input": map[string]any{"file_path": vcsRepo + "/src/a.py"}}), 2, "no directories")
}

func TestVcsGuardBadPayloadBlocks(t *testing.T) {
	t.Parallel()
	args := append([]string{"--agent", "general"}, vcsDirs(t)...)
	code(t, hook(t, nil, "vcs-guard", args, "{not json"), 2, "bad JSON")
	code(t, hook(t, nil, "vcs-guard", args, map[string]any{"tool_name": "Write", "agent_type": "general", "tool_input": map[string]any{}}), 2, "no file_path")
}

func TestVcsGuardOtherToolPasses(t *testing.T) {
	t.Parallel()
	code(t, vcsCall(t, "general", vcsRepo+"/.git/config", ptr("general"), "Read", vcsRepo), 0, "Read")
}

// ── the Edit/Write guard for every session: grants and writes outside the project ──
// The install adds this guard, unscoped, to ~/.claude/settings.json, and every
// agent that can write carries it. Writes into the grants directory are
// refused for everyone; a write outside the project (the git toplevel of the
// payload's cwd), its sibling `<root>-worktrees/`, /tmp, $TMPDIR, ~/.claude/,
// and ~/.cache/ is the outside-write ASK class. Paths are judged through `..`
// and through the real path of their deepest existing ancestor.
//
// Each case has its own scratch root, project, state, and TMPDIR.

const writeHome = "/home/jhon-write-test"

type writeEnv struct {
	w, project, state, tmp, grants string
	env                            []string
}

func newWriteEnv(t *testing.T) writeEnv {
	t.Helper()
	w := realTemp(t)
	e := writeEnv{w: w, project: filepath.Join(w, "proj"), state: filepath.Join(w, "state"), tmp: filepath.Join(w, "tmpdir")}
	mkdir(t, filepath.Join(e.project, "src"))
	support.GitInit(t, e.project)
	mkdir(t, e.tmp)
	e.grants = filepath.Join(e.state, "jhon", "grants")
	e.env = support.Env("HOME="+writeHome, "XDG_STATE_HOME="+e.state, "TMPDIR="+e.tmp)
	return e
}

type writeOpts struct {
	top  bool
	mode *string
	tool string
}

type writeResult struct {
	code int
	err  string
	ask  *string
}

// write is a write by the top-level session (no agent_id) or by a
// sub-agent, to the unscoped settings form of the guard.
func (e writeEnv) write(t *testing.T, file string, o writeOpts) writeResult {
	t.Helper()
	if o.tool == "" {
		o.tool = "Write"
	}
	mode := "bypassPermissions"
	if o.top {
		mode = "default"
	}
	if o.mode != nil {
		mode = *o.mode
	}
	input := map[string]any{"file_path": file}
	if o.tool == "NotebookEdit" {
		input = map[string]any{"notebook_path": file}
	}
	payload := map[string]any{"tool_name": o.tool, "cwd": e.project, "permission_mode": mode, "tool_input": input}
	if !o.top {
		payload["agent_id"], payload["agent_type"] = "a1", "worker"
	}
	r := hook(t, e.env, "vcs-guard", vcsDirs(t), payload)
	res := writeResult{code: r.Code, err: r.Stderr}
	if d, why, ok := decision(t, r.Stdout); ok && d == "ask" {
		res.ask = &why
	}
	return res
}

func (e writeEnv) grant(t *testing.T, cls, root string, minutes int) {
	t.Helper()
	writeGrant(t, e.grants, cls, root, minutes, nil)
}

func (e writeEnv) clearGrants(t *testing.T) {
	t.Helper()
	if err := os.RemoveAll(e.grants); err != nil {
		t.Fatal(err)
	}
}

func asked(r writeResult) string {
	if r.ask == nil {
		return ""
	}
	return *r.ask
}

// The outside and inside paths, with <proj> and <tmpdir> standing for the
// case's project and TMPDIR, and <rel> for /usr/share/jhon-x.txt relative to
// the project.
var (
	writeOutside = []string{"/usr/share/jhon-x.txt", "/etc/jhon.conf", writeHome + "/notes.md", writeHome + "/.bashrc", "<rel>",
		"src/../../../../../../../../usr/share/x", "/var/jhon-elsewhere/x"}
	writeInside = []string{"src/app.ts", "<proj>/README.md", "<proj>-worktrees/feat/src/a.ts", "/tmp/jhon-scratch.md", "<tmpdir>/x.json",
		writeHome + "/.claude/plans/p.md", writeHome + "/.claude/projects/x/memory/m.md", writeHome + "/.cache/jhon/x.json"}
)

func (e writeEnv) expand(name string) string {
	rel, _ := filepath.Rel(e.project, "/usr/share/jhon-x.txt")
	return strings.NewReplacer("<rel>", rel, "<proj>", e.project, "<tmpdir>", e.tmp).Replace(name)
}

func TestVcsGuardOutsideWrite(t *testing.T) {
	t.Parallel()
	for _, name := range writeOutside {
		t.Run("outside-write: "+name+" is asked at the top level and refused for a sub-agent", func(t *testing.T) {
			t.Parallel()
			e := newWriteEnv(t)
			file := e.expand(name)
			top := e.write(t, file, writeOpts{top: true})
			if top.code != 0 {
				t.Errorf("top: %d: %s", top.code, top.err)
			}
			matches(t, asked(top), `^outside-write: `, file+" asked")
			sub := e.write(t, file, writeOpts{})
			if sub.code != 2 {
				t.Errorf("sub-agent: %d", sub.code)
			}
			matches(t, sub.err, "(?s)^vcs-guard: .*outside-write needs the owner's authorization.*`! jhon grant outside-write \\[--for 30m\\]`", file)
		})
	}
}

func TestVcsGuardInsideWrite(t *testing.T) {
	t.Parallel()
	for _, name := range writeInside {
		t.Run("inside the project or a scratch root: "+name+" is allowed without asking", func(t *testing.T) {
			t.Parallel()
			e := newWriteEnv(t)
			file := e.expand(name)
			for _, top := range []bool{true, false} {
				r := e.write(t, file, writeOpts{top: top})
				if r.code != 0 || r.ask != nil {
					t.Errorf("%s top=%v: %d, ask %v: %s", file, top, r.code, r.ask, r.err)
				}
			}
		})
	}
}

func TestVcsGuardOutsideWriteBypassAtTopRefused(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	if r := e.write(t, "/etc/jhon.conf", writeOpts{top: true, mode: ptr("bypassPermissions")}); r.code != 2 {
		t.Errorf("code = %d", r.code)
	}
}

func TestVcsGuardOutsideWriteGrant(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	e.grant(t, "outside-write", e.project, 30)
	if c := e.write(t, "/etc/jhon.conf", writeOpts{}).code; c != 0 {
		t.Errorf("live grant: %d", c)
	}
	e.clearGrants(t)
	e.grant(t, "outside-write", e.project, -1)
	if c := e.write(t, "/etc/jhon.conf", writeOpts{}).code; c != 2 {
		t.Errorf("expired grant: %d", c)
	}
	e.clearGrants(t)
	e.grant(t, "outside-write", "/home/u/other", 30)
	if c := e.write(t, "/etc/jhon.conf", writeOpts{}).code; c != 2 {
		t.Errorf("another root's grant: %d", c)
	}
}

func TestVcsGuardSymlinkLeadingOutside(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	symlink(t, "/usr/share", filepath.Join(e.project, "share-link"))
	matches(t, asked(e.write(t, "share-link/jhon-x.txt", writeOpts{top: true})), `^outside-write: `, "top")
	if c := e.write(t, "share-link/jhon-x.txt", writeOpts{}).code; c != 2 {
		t.Errorf("sub-agent: %d", c)
	}
}

func TestVcsGuardGrantsDirectoryUnwritable(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	e.grant(t, "outside-write", e.project, 30)
	symlink(t, e.state, filepath.Join(e.project, "state-link"))
	for _, file := range []string{filepath.Join(e.grants, "k", "push.json"), "state-link/jhon/grants/k/push.json", e.grants + "/../grants/x.json"} {
		for _, top := range []bool{true, false} {
			r := e.write(t, file, writeOpts{top: top})
			if r.code != 2 {
				t.Errorf("%s top=%v: %d", file, top, r.code)
			}
			matches(t, r.err, `grants`, file)
		}
	}
}

func TestVcsGuardNotebookEditAndEdit(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	if c := e.write(t, ".git/x.ipynb", writeOpts{tool: "NotebookEdit"}).code; c != 2 {
		t.Errorf("NotebookEdit .git: %d", c)
	}
	matches(t, asked(e.write(t, "/etc/x.ipynb", writeOpts{tool: "NotebookEdit", top: true})), `^outside-write: `, "NotebookEdit outside")
	if c := e.write(t, "src/x.ipynb", writeOpts{tool: "NotebookEdit"}).code; c != 0 {
		t.Errorf("NotebookEdit src: %d", c)
	}
	if c := e.write(t, "/etc/jhon.conf", writeOpts{tool: "Edit"}).code; c != 2 {
		t.Errorf("Edit outside: %d", c)
	}
	if c := e.write(t, ".git/config", writeOpts{tool: "Edit", top: true}).code; c != 2 {
		t.Errorf("Edit .git top: %d", c)
	}
}

// ── Claude Code settings: no agent disables the guards ──────────────────────
// The guards are installed through ~/.claude/settings.json; a project's
// .claude/settings*.json can add or override hooks and permissions too. A
// sub-agent's write there is refused, whatever grant exists; the top-level
// session is asked, never allowed silently (~/.claude is otherwise a scratch
// root).

func TestVcsGuardSettings(t *testing.T) {
	t.Parallel()
	for _, name := range []string{writeHome + "/.claude/settings.json", writeHome + "/.claude/settings.local.json", ".claude/settings.json",
		"<project>/.claude/settings.local.json", "src/../.claude/settings.json"} {
		t.Run("settings: "+name+" is refused for a sub-agent, even with an outside-write grant, and asked at the top level", func(t *testing.T) {
			t.Parallel()
			e := newWriteEnv(t)
			file := strings.Replace(name, "<project>", e.project, 1)
			e.grant(t, "outside-write", e.project, 30)
			sub := e.write(t, file, writeOpts{})
			if sub.code != 2 {
				t.Errorf("sub-agent: %d", sub.code)
			}
			matches(t, sub.err, `(?s)^vcs-guard: .*settings`, "sub-agent")
			matches(t, sub.err, "(?m)the owner edits the settings by hand in their own terminal, or with `!` in Claude Code\\.$", "the refusal names the owner's way, without a pronoun")
			if c := e.write(t, file, writeOpts{tool: "Edit"}).code; c != 2 {
				t.Errorf("Edit: %d", c)
			}
			top := e.write(t, file, writeOpts{top: true})
			if top.code != 0 {
				t.Errorf("top: %d: %s", top.code, top.err)
			}
			matches(t, asked(top), `settings`, "top asked")
			if c := e.write(t, file, writeOpts{top: true, mode: ptr("bypassPermissions")}).code; c != 2 {
				t.Errorf("bypassPermissions cannot be asked: %d", c)
			}
		})
	}
}

func TestVcsGuardScopedSettingsWrite(t *testing.T) {
	t.Parallel()
	r := hook(t, nil, "vcs-guard", append([]string{"--agent", "general"}, vcsDirs(t)...), map[string]any{
		"tool_name": "Write", "agent_type": "general", "agent_id": "a1", "cwd": vcsRepo, "tool_input": map[string]any{"file_path": vcsRepo + "/.claude/settings.json"},
	})
	code(t, r, 2, "settings")
}

func TestVcsGuardOtherClaudeFilesOrdinary(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	if c := e.write(t, writeHome+"/.claude/plans/settings.md", writeOpts{}).code; c != 0 {
		t.Errorf("plans/settings.md: %d", c)
	}
	if c := e.write(t, ".claude/agents/x.md", writeOpts{}).code; c != 0 {
		t.Errorf(".claude/agents/x.md: %d", c)
	}
}

func TestVcsGuardOutsideWriteModes(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	for _, mode := range []string{"auto", "weird", ""} {
		r := e.write(t, "/etc/jhon.conf", writeOpts{top: true, mode: ptr(mode)})
		if r.code != 2 || r.ask != nil {
			t.Errorf("%q: %d, ask %v: %s", mode, r.code, r.ask, r.err)
		}
	}
	if e.write(t, "/etc/jhon.conf", writeOpts{top: true, mode: ptr("acceptEdits")}).ask == nil {
		t.Error("acceptEdits is asked")
	}
}

// The write guard never judges how a change plans (S20): a write to its
// tasks.md or proposal.md that departs from the adaptive flow passes with no
// output, and `jhon status` notes the departure. Each case below was a
// refusal before: tasks planned ahead of their slice, a flat list, a slice
// without its `Delivers:` line, a slice past 12 tasks, a task nested past
// one level, a proposal without acceptance criteria.
func TestVcsGuardLeavesThePlanToNotes(t *testing.T) {
	t.Parallel()
	root := realTemp(t)
	dir := mkdir(t, filepath.Join(root, "openspec", "changes", "c"))
	tasks, proposal := filepath.Join(dir, "tasks.md"), filepath.Join(dir, "proposal.md")
	current := "## Slice 1: a\nDelivers: CA-01\n- [ ] 1.1 RED\n## Slice 2: b\nDelivers: CA-02\n- [ ] 2.1 Plan the slice\n"
	if err := os.WriteFile(tasks, []byte(current), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(proposal, []byte("# Proposal: x\n\n## Why\n\nw\n\n## Acceptance Criteria\n\n- a user sees y\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	call := func(file, tool string, input map[string]any) support.Result {
		input["file_path"] = file
		return hook(t, nil, "vcs-guard", append([]string{"--agent", "worker"}, vcsDirs(t)...), map[string]any{"tool_name": tool, "cwd": root, "tool_input": input})
	}
	slice := func(n int) string {
		var b strings.Builder
		b.WriteString("## Slice 1: catalog\n\nDelivers: CA-01\n\n")
		for i := 1; i <= n; i++ {
			fmt.Fprintf(&b, "- [ ] 1.%d task\n", i)
		}
		return b.String()
	}
	ahead := "- [ ] 2.1 RED\n- [ ] 2.2 GREEN\n"
	for label, r := range map[string]support.Result{
		"Write planning slice 2 ahead":               call(tasks, "Write", map[string]any{"content": strings.Replace(current, "- [ ] 2.1 Plan the slice\n", ahead, 1)}),
		"Edit planning slice 2 ahead":                call(tasks, "Edit", map[string]any{"old_string": "- [ ] 2.1 Plan the slice\n", "new_string": ahead}),
		"pi's edits planning slice 2 ahead":          call(tasks, "Edit", map[string]any{"edits": []any{map[string]any{"old_string": "- [ ] 2.1 Plan the slice\n", "new_string": ahead}}}),
		"a flat list":                                call(tasks, "Write", map[string]any{"content": "# Tasks\n- [ ] a\n- [ ] b\n"}),
		"a slice without Delivers":                   call(tasks, "Write", map[string]any{"content": "## Slice 1: catalog\n\n- [ ] 1.1 RED\n"}),
		"a slice of 13 tasks":                        call(tasks, "Write", map[string]any{"content": slice(10) + "  - [ ] 1.10.1 sub\n  - [ ] 1.10.2 sub\n  - [ ] 1.10.3 sub\n"}),
		"a sub-task of a sub-task":                   call(tasks, "Write", map[string]any{"content": slice(1) + "  - [ ] 1.1.1 sub\n    - [ ] 1.1.1.1 deeper\n"}),
		"a new proposal without acceptance criteria": call(proposal, "Write", map[string]any{"content": "# Proposal: x\n\n## Why\n\nw\n"}),
		"an edit removing acceptance criteria":       call(proposal, "Edit", map[string]any{"old_string": "## Acceptance Criteria\n\n- a user sees y\n", "new_string": ""}),
	} {
		code(t, r, 0, label)
		if r.Stdout != "" || r.Stderr != "" {
			t.Errorf("%s: no output, got stdout %q stderr %q", label, r.Stdout, r.Stderr)
		}
	}
}
