package hook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
)

// T134 for Claude Code (L1036 K1-K5): an Agent call of the main session the
// guards allow launches with its brief followed by the decisions on record
// and the code map, pi's cases ported. Fakes only: an `engram` (JHON_ENGRAM)
// the handoff packet asks, a `codegraph` first on PATH; each logs its argv.

const launchBrief = "Fix `runAgent` in shell/lib/agents.ts:180 so the launch is bounded.\n\n## Allowed edit surfaces\n- shell/lib/agents.ts\n"

const launchQuery = `[{"node":{"kind":"function","name":"runAgent","filePath":"shell/lib/agents.ts","startLine":264,"signature":"(agent: AgentDef, task: string): Promise<ChildResult>"},"score":9},` +
	`{"node":{"kind":"function","name":"runAgentLater","filePath":"shell/lib/other.ts","startLine":3,"signature":"()"},"score":2}]`

const launchCallers = `{"symbol":"runAgent","definitions":[{"callers":[{"name":"foreground","kind":"function","filePath":"shell/extensions/jhon.ts","startLine":316}]}]}`

const wantDecisions = "\n\n## Decisions on record\nAdded at launch from `jhon context --handoff`; search Engram before deciding against one.\n" +
	"- #41 (decision) Launch context goes at the end of the brief\n- #7 (architecture) The guard reads the task file"

const wantCodeMap = "\n\n## Code map\nAdded at launch from CodeGraph for what the brief names; lines are as of the launch.\n" +
	"- function runAgent shell/lib/agents.ts:264 (agent: AgentDef, task: string): Promise<ChildResult>; callers: foreground (shell/extensions/jhon.ts:316)"

type launchFakes struct{ dir string }

// fakeLaunchTools puts a fake engram (JHON_ENGRAM) and a fake codegraph
// (first on PATH) in place for the test; FAKE_* variables steer them.
func fakeLaunchTools(t *testing.T) launchFakes {
	t.Helper()
	dir := launchTemp(t)
	script := func(name, body string) {
		file := filepath.Join(dir, name)
		text := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + filepath.Join(dir, name+".log") + "'\n" + body
		if err := os.WriteFile(file, []byte(text), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script("engram", `[ -n "$FAKE_ENGRAM_SLEEP" ] && exec sleep "$FAKE_ENGRAM_SLEEP"
[ -n "$FAKE_ENGRAM_EXIT" ] && exit "$FAKE_ENGRAM_EXIT"
case "$*" in
  *"--type decision"*) printf '[1] #41 (decision) — Launch context goes at the end of the brief\n' ;;
  *) printf '[1] #7 (architecture) — The guard reads the task file\n' ;;
esac
`)
	script("codegraph", `[ -n "$FAKE_CG_SLEEP" ] && exec sleep "$FAKE_CG_SLEEP"
[ -n "$FAKE_CG_EXIT" ] && exit "$FAKE_CG_EXIT"
case "$1" in callers) printf '%s' "$FAKE_CALLERS" ;; *) printf '%s' "$FAKE_QUERY" ;; esac
`)
	t.Setenv("JHON_ENGRAM", filepath.Join(dir, "engram"))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_QUERY", launchQuery)
	t.Setenv("FAKE_CALLERS", launchCallers)
	saved := codegraphProgram
	codegraphProgram = "codegraph"
	t.Cleanup(func() { codegraphProgram = saved })
	return launchFakes{dir}
}

// log is what a fake was run with, one argv per line.
func (f launchFakes) log(name string) []string {
	data, _ := os.ReadFile(filepath.Join(f.dir, name+".log"))
	return strings.Fields(strings.ReplaceAll(strings.TrimSpace(string(data)), " ", "\x00"))
}

func (f launchFakes) ran(name, argv string) bool {
	return slices.Contains(f.log(name), strings.ReplaceAll(argv, " ", "\x00"))
}

func launchTemp(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "jhon-launch-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	d, _ = filepath.EvalSymlinks(d)
	return d
}

func launchGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Dir = dir
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "GIT_") })
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

func launchWrite(t *testing.T, file, text string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(file), 0o777)
	if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// launchRepo is a git repository holding shell/lib/agents.ts; indexed gives
// it a CodeGraph database. With ldd it uses LDD (openspec/config.yaml).
func launchRepo(t *testing.T, indexed, ldd bool) string {
	t.Helper()
	d := launchTemp(t)
	launchGit(t, d, "init", "-q")
	launchWrite(t, filepath.Join(d, "shell/lib/agents.ts"), "export function runAgent() {}\n")
	if ldd {
		launchWrite(t, filepath.Join(d, "openspec/config.yaml"), "")
	}
	launchGit(t, d, "add", "-A")
	launchGit(t, d, "commit", "-q", "-m", "init")
	// An edit in the tree: the handoff's search names what it touches.
	launchWrite(t, filepath.Join(d, "shell/lib/agents.ts"), "export function runAgent() { return 1 }\n")
	if indexed {
		launchWrite(t, filepath.Join(d, ".codegraph/codegraph.db"), "")
	}
	return d
}

// agentCall is pre-tool's answer to an Agent call in cwd, as the plugin
// runs it for jhon-orchestrator, with extra payload keys.
func agentCall(t *testing.T, cwd, kind, prompt, mode string, extra map[string]any) (code int, stdout, stderr string) {
	t.Helper()
	payload := map[string]any{"session_id": "s1", "hook_event_name": "PreToolUse", "cwd": cwd, "permission_mode": mode, "tool_name": "Agent",
		"tool_input": map[string]any{"description": "Fix it", "prompt": prompt, "subagent_type": kind, "model": "sonnet"}}
	for k, v := range extra {
		payload[k] = v
	}
	data, _ := json.Marshal(payload)
	var out, errs bytes.Buffer
	code = PreTool([]string{"--agent", "jhon-orchestrator", "route-guard", "worker"}, IO{bytes.NewReader(data), &out, &errs})
	return code, out.String(), errs.String()
}

// answerOf is the PreToolUse output of an answer.
func answerOf(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var j map[string]map[string]any
	if err := json.Unmarshal([]byte(stdout), &j); err != nil || len(j) != 1 {
		t.Fatalf("not one hook answer: %q", stdout)
	}
	return j["hookSpecificOutput"]
}

// K1, K2: the answer is ONE JSON object, the original input with the brief
// followed by both blocks; allow in the modes that launch without asking,
// no decision in plan and auto mode.
func TestLaunchAppendsTheDecisionsAndTheCodeMap(t *testing.T) {
	f := fakeLaunchTools(t)
	cwd := launchRepo(t, true, false)
	code, stdout, stderr := agentCall(t, cwd, "worker", launchBrief, "default", nil)
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	a := answerOf(t, stdout)
	in, _ := a["updatedInput"].(map[string]any)
	want := map[string]any{"description": "Fix it", "subagent_type": "worker", "model": "sonnet", "prompt": launchBrief + wantDecisions + wantCodeMap}
	if !reflect.DeepEqual(in, want) {
		t.Errorf("updatedInput:\n%#v\nwant\n%#v", in, want)
	}
	if a["hookEventName"] != "PreToolUse" || a["permissionDecision"] != "allow" || a["permissionDecisionReason"] != launchAllowReason || len(a) != 4 {
		t.Errorf("answer: %s", stdout)
	}
	if !f.ran("codegraph", "query runAgent --json") || !f.ran("codegraph", "query agents --json") || !f.ran("codegraph", "callers runAgent --json --limit 5") {
		t.Errorf("codegraph ran: %q", f.log("codegraph"))
	}
	if !slices.ContainsFunc(f.log("engram"), func(a string) bool { return strings.HasPrefix(a, "search\x00") }) {
		t.Errorf("engram ran: %q", f.log("engram"))
	}
	for _, mode := range []string{"plan", "auto", "dontAsk"} {
		_, stdout, _ := agentCall(t, cwd, "worker", launchBrief, mode, nil)
		a := answerOf(t, stdout)
		if _, decided := a["permissionDecision"]; decided || a["updatedInput"].(map[string]any)["prompt"] != want["prompt"] {
			t.Errorf("%s mode: %s", mode, stdout)
		}
	}
}

// K1, K2: a deny or an ask of route-guard stands as it came, nothing added.
func TestLaunchLeavesADenyOrAnAskAsItCame(t *testing.T) {
	fakeLaunchTools(t)
	cwd := launchRepo(t, true, true)
	code, stdout, stderr := agentCall(t, cwd, "worker", "Fix `runAgent`.", "default", nil)
	if code != 2 || stdout != "" || !strings.Contains(stderr, "launched with its scope") {
		t.Errorf("a worker with no scope: %d %q %q", code, stdout, stderr)
	}
	code, stdout, _ = agentCall(t, cwd, "prism-safety", launchBrief, "default", nil)
	// route-guard's own answer, alone.
	data, _ := json.Marshal(map[string]any{"session_id": "s1", "hook_event_name": "PreToolUse", "cwd": cwd, "permission_mode": "default", "tool_name": "Agent",
		"tool_input": map[string]any{"description": "Fix it", "prompt": launchBrief, "subagent_type": "prism-safety", "model": "sonnet"}})
	var want bytes.Buffer
	RouteGuard([]string{"--agent", "jhon-orchestrator", "worker"}, IO{bytes.NewReader(data), &want, &want})
	if code != 0 || stdout != want.String() || !strings.Contains(stdout, `"permissionDecision":"ask"`) {
		t.Errorf("a reviewer at the top level:\n%s\nwant\n%s", stdout, want.String())
	}
}

// K1: with nothing to add the answer is today's, byte for byte.
func TestLaunchWithNothingToAddIsAnsweredAsToday(t *testing.T) {
	f := fakeLaunchTools(t)
	t.Setenv("JHON_ENGRAM", "/dev/null/engram")
	cwd := launchRepo(t, false, false)
	if code, stdout, stderr := agentCall(t, cwd, "worker", launchBrief, "default", nil); code != 0 || stdout != "" || stderr != "" {
		t.Errorf("no Engram, no index: %d %q %q", code, stdout, stderr)
	}
	if len(f.log("codegraph")) != 0 {
		t.Errorf("no index, yet codegraph ran: %q", f.log("codegraph"))
	}
	// Failing, unreadable or empty sources leave the headings out too.
	t.Setenv("JHON_ENGRAM", filepath.Join(f.dir, "engram"))
	indexed := launchRepo(t, true, false)
	for i, env := range []map[string]string{
		{"FAKE_ENGRAM_EXIT": "1", "FAKE_CG_EXIT": "2"},
		{"FAKE_ENGRAM_EXIT": "1", "FAKE_QUERY": "[", "FAKE_CALLERS": "not json"},
		{"FAKE_ENGRAM_EXIT": "0", "FAKE_QUERY": "[]", "FAKE_CALLERS": "{}"},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			for k, v := range env {
				t.Setenv(k, v)
			}
			if code, stdout, stderr := agentCall(t, indexed, "worker", launchBrief, "default", nil); code != 0 || stdout != "" || stderr != "" {
				t.Errorf("%v: %d %q %q", env, code, stdout, stderr)
			}
		})
	}
}

// A slow Engram and a slow CodeGraph are dropped at their deadlines (2 s, 3 s).
func TestLaunchDropsSlowSources(t *testing.T) {
	fakeLaunchTools(t)
	t.Setenv("FAKE_ENGRAM_SLEEP", "8")
	t.Setenv("FAKE_CG_SLEEP", "8")
	cwd := launchRepo(t, true, false)
	start := time.Now()
	code, stdout, _ := agentCall(t, cwd, "worker", launchBrief, "default", nil)
	if took := time.Since(start); code != 0 || stdout != "" || took > 6*time.Second {
		t.Errorf("slow sources: %d %q in %s", code, stdout, took)
	}
}

// No index: no code map, and CodeGraph never runs; the decisions still come.
func TestLaunchWithNoIndexHasNoCodeMap(t *testing.T) {
	f := fakeLaunchTools(t)
	cwd := launchRepo(t, false, false)
	_, stdout, _ := agentCall(t, cwd, "worker", launchBrief, "default", nil)
	if got := answerOf(t, stdout)["updatedInput"].(map[string]any)["prompt"]; got != launchBrief+wantDecisions {
		t.Errorf("prompt: %q", got)
	}
	if len(f.log("codegraph")) != 0 {
		t.Errorf("codegraph ran: %q", f.log("codegraph"))
	}
}

// launchWorktree is a change's linked worktree of an indexed checkout.
func launchWorktree(t *testing.T) (main, wt string) {
	t.Helper()
	main = launchRepo(t, true, false)
	wt = filepath.Join(launchTemp(t), "change")
	launchGit(t, main, "worktree", "add", "-q", "-b", "change", wt)
	return main, wt
}

// A launch in a linked worktree asks the main checkout's index, and keeps
// only what is at the same path in the worktree.
func TestLaunchInALinkedWorktreeUsesTheMainCheckoutsIndex(t *testing.T) {
	f := fakeLaunchTools(t)
	t.Setenv("JHON_ENGRAM", "/dev/null/engram")
	main, wt := launchWorktree(t)
	t.Setenv("FAKE_CALLERS", `{"definitions":[{"callers":[{"name":"launch","filePath":"shell/lib/agents.ts","startLine":20},{"name":"foreground","filePath":"shell/extensions/jhon.ts","startLine":316}]}]}`)
	_, stdout, _ := agentCall(t, wt, "worker", launchBrief, "default", nil)
	want := launchBrief + "\n\n## Code map\nAdded at launch from CodeGraph for what the brief names; lines are as of the launch.\n" +
		"- function runAgent shell/lib/agents.ts:264 (agent: AgentDef, task: string): Promise<ChildResult>; callers: launch (shell/lib/agents.ts:20)"
	if got := answerOf(t, stdout)["updatedInput"].(map[string]any)["prompt"]; got != want {
		t.Errorf("prompt:\n%v", got)
	}
	for _, argv := range []string{"query runAgent --json -p " + main, "query agents --json -p " + main, "callers runAgent --json --limit 5 -p " + main} {
		if !f.ran("codegraph", argv) {
			t.Errorf("not run: %s; ran %q", argv, f.log("codegraph"))
		}
	}
	if _, err := os.Stat(filepath.Join(wt, ".codegraph")); err == nil {
		t.Error("the worktree got an index")
	}
	// Results whose files are not in the worktree are left out: no heading.
	t.Setenv("FAKE_QUERY", `[{"node":{"kind":"function","name":"runAgent","filePath":"shell/lib/moved.ts","startLine":3}}]`)
	t.Setenv("FAKE_CALLERS", "{}")
	if _, stdout, _ := agentCall(t, wt, "worker", launchBrief, "default", nil); stdout != "" {
		t.Errorf("nothing kept: %q", stdout)
	}
}

// K4: a sub-agent's own Agent call, and pi's (scoped to `*`), are answered as today.
func TestLaunchLeavesASubAgentsCallAsItIs(t *testing.T) {
	f := fakeLaunchTools(t)
	cwd := launchRepo(t, true, false)
	code, stdout, stderr := agentCall(t, cwd, "worker", launchBrief, "bypassPermissions", map[string]any{"agent_id": "a1", "agent_type": "jhon-orchestrator"})
	if code != 0 || stdout != "" || stderr != "" {
		t.Errorf("a sub-agent's launch: %d %q %q", code, stdout, stderr)
	}
	data, _ := json.Marshal(map[string]any{"cwd": cwd, "tool_name": "Agent", "tool_input": map[string]any{"prompt": launchBrief, "subagent_type": "worker"}})
	var out bytes.Buffer
	if code := PreTool([]string{"--agent", "*", "route-guard", "worker"}, IO{bytes.NewReader(data), &out, &out}); code != 0 || out.Len() != 0 {
		t.Errorf("pi's call: %d %q", code, out.String())
	}
	if len(f.log("codegraph")) != 0 || len(f.log("engram")) != 0 {
		t.Errorf("asked: %q %q", f.log("codegraph"), f.log("engram"))
	}
}

// K3: the scope a writer is held to is its brief's: the appended blocks,
// which name other files, are not read as surfaces.
func TestLaunchBlocksAreNotReadAsSurfaces(t *testing.T) {
	cwd := launchRepo(t, false, false)
	for _, brief := range []string{launchBrief, strings.TrimSuffix(launchBrief, "\n"), "## Allowed edit surfaces\n"} {
		launched := brief + wantDecisions + wantCodeMap
		got, ok := guard.SurfacesOf(launched)
		want, _ := guard.SurfacesOf(brief)
		if !ok || !slices.Equal(got, want) {
			t.Errorf("%q: surfaces %q, want %q", brief, got, want)
		}
	}
	edit := func(file string) int {
		data, _ := json.Marshal(map[string]any{"cwd": cwd, "agent_id": "a1", "agent_type": "worker", "launch_prompt": launchBrief + wantDecisions + wantCodeMap,
			"tool_name": "Edit", "tool_input": map[string]any{"file_path": filepath.Join(cwd, file), "old_string": "a", "new_string": "b"}})
		var out bytes.Buffer
		return ScopeGuard([]string{"--agent", "worker"}, IO{bytes.NewReader(data), &out, &out})
	}
	if edit("shell/lib/agents.ts") != 0 || edit("shell/extensions/jhon.ts") != 2 {
		t.Error("the code map's file is read as a surface, or the brief's is not")
	}
}

// pi's limits: at most 12 decision lines; a code map of at most 4096 bytes.
func TestLaunchBlockLimits(t *testing.T) {
	var many []ldd.Decision
	for i := range 30 {
		many = append(many, ldd.Decision{ID: float64(i + 1), Type: "decision", Title: "Decision"})
	}
	if n := len(strings.Split(decisionsBlock(many), "\n")); n != decisionsLines {
		t.Errorf("%d decision lines", n)
	}
	if decisionsBlock([]ldd.Decision{{ID: 1, Title: "  "}}) != "" {
		t.Error("a decision with no title makes a block")
	}
	fakeLaunchTools(t)
	cwd := launchRepo(t, true, false)
	var long []string
	for i := range 40 {
		long = append(long, fmt.Sprintf(`{"node":{"kind":"function","name":"runAgent","filePath":"shell/lib/agents.ts","startLine":%d,"signature":"(%s)"}}`, i+1, strings.Repeat("x", 180)))
	}
	t.Setenv("FAKE_QUERY", "["+strings.Join(long, ",")+"]")
	t.Setenv("FAKE_CALLERS", "{}")
	block := codeMap("`runAgent`", cwd, codeMapTime)
	if !strings.HasPrefix(block, "## Code map\n") || len(block) > codeMapBytes || strings.Count(block, "\n- ") < 2 {
		t.Errorf("%d bytes: %.80q", len(block), block)
	}
}

// The brief's names: existing files and path:line references, backticked identifiers; at most 8.
func TestLaunchBriefNames(t *testing.T) {
	cwd := launchRepo(t, false, false)
	got := briefNames("See `runAgent()`, `ExplorationBudget.reset`, shell/lib/agents.ts:12, `shell/lib/agents.ts`, gone/missing.ts:3, `a b`, `x`.", cwd)
	want := []briefName{{symbol: "runAgent"}, {symbol: "reset"}, {file: "shell/lib/agents.ts"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("names %+v", got)
	}
	var many []string
	for i := range 20 {
		many = append(many, "`symbol"+string(rune('a'+i))+"`")
	}
	if n := len(briefNames(strings.Join(many, " "), cwd)); n != codeMapNames {
		t.Errorf("%d names", n)
	}
	// A path inside a word or after a slash is not a reference.
	if got := briefNames("x/shell/lib/agents.ts and a.shell/lib/agents.ts", cwd); len(got) != 0 {
		t.Errorf("names %+v", got)
	}
}

// T137 (L1064 KB2, KB3): verify and each Prism facet, launched for a change
// whose record marks a test as turned green by editing only its test, get
// a `## Tests to judge` block after the other blocks; any other agent, and
// a change with nothing to judge, get none.
const judgedRecord = `{"timestamp":"2026-01-01T00:02:00.000Z","complete":true,"scope":"full","executions":[{"project":"b","command":"sh t.sh","status":"pass","exit_code":0}],` +
	`"turned_green":[{"project":"b","test":"tests/test_ids.py::test_ids","file":"b/tests/test_ids.py","red_run":"2026-01-01T00:01:00.000Z","green_run":"2026-01-01T00:02:00.000Z","red_tree":"t1","green_tree":"t2"}]}` + "\n"

const wantTestsToJudge = "\n\n## Tests to judge\n" + ldd.TestsToJudgeIntro + "\n" +
	"- `tests/test_ids.py::test_ids` (b, b/tests/test_ids.py): failed in run 2026-01-01T00:01:00.000Z, passed in run 2026-01-01T00:02:00.000Z"

func TestLaunchOfAJudgeCarriesTheTestsToJudge(t *testing.T) {
	fakeLaunchTools(t)
	t.Setenv("JHON_ENGRAM", "/dev/null/engram")
	cwd := launchRepo(t, false, true)
	launchWrite(t, filepath.Join(cwd, "openspec/changes/c1/tasks.md"), "- [ ] 1.1 a task\n")
	brief := "Verify change c1.\n"
	prompt := func(kind string) string {
		t.Helper()
		code, stdout, stderr := agentCall(t, cwd, kind, brief, "default", nil)
		if code != 0 || stderr != "" {
			t.Fatalf("%s: exit %d: %s", kind, code, stderr)
		}
		if stdout == "" {
			return brief
		}
		in, _ := answerOf(t, stdout)["updatedInput"].(map[string]any)
		p, _ := in["prompt"].(string)
		return p
	}
	if got := prompt("verify"); got != brief {
		t.Errorf("nothing to judge, yet verify's brief is %q", got)
	}
	launchWrite(t, filepath.Join(cwd, "openspec/changes/c1/execution-record.json"), judgedRecord)
	if got := prompt("verify"); got != brief+wantTestsToJudge {
		t.Errorf("verify's brief:\n%q\nwant\n%q", got, brief+wantTestsToJudge)
	}
	if got := prompt("explore"); got != brief {
		t.Errorf("explore judges nothing, yet its brief is %q", got)
	}
	for _, facet := range ldd.TestJudges[1:] {
		if got := launchContext(brief, cwd, facet); got != wantTestsToJudge {
			t.Errorf("%s's launch adds %q", facet, got)
		}
	}
}
