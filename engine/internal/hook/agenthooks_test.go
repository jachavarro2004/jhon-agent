package hook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lddswitch"
)

// agentFile is a definition in the shape render/claude.go writes.
const agentFile = `---
name: jhon-orchestrator
description: "Jhon orchestrator"
hooks:
  PreToolUse:
    - matcher: "Edit|Write|NotebookEdit|Bash|SendMessage"
      hooks:
        - type: command
          command: "'/x/jhon' 'hook' 'pre-tool' '--agent' 'jhon-orchestrator' 'safety-guard'"
  Stop:
    - hooks:
        - type: command
          command: "'/x/jhon' 'hook' 'context-notice'"
---
# Orchestrator
hooks:
  PreToolUse:
          command: "'/x/jhon' 'hook' 'nope'"
`

func runAgentHooks(t *testing.T, payload string, part ...string) (int, string, string) {
	t.Helper()
	var out, errs bytes.Buffer
	code := AgentHooks(append([]string{"jhon-orchestrator"}, part...), IO{strings.NewReader(payload), &out, &errs})
	return code, out.String(), errs.String()
}

func TestAgentHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "jhon-orchestrator.md"), []byte(agentFile), 0o644); err != nil {
		t.Fatal(err)
	}
	hooks, err := readAgentHooks(filepath.Join(dir, "jhon-orchestrator.md"))
	if err != nil || len(hooks) != 2 || hooks[0].event != "PreToolUse" || hooks[1].event != "Stop" || hooks[1].matcher != "" {
		t.Fatalf("readAgentHooks = %+v, %v; the body after the frontmatter is not hooks", hooks, err)
	}
	cwd := t.TempDir()
	push := `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git push origin feat/x"},"cwd":"` + cwd + `","permission_mode":"bypassPermissions"%s}`
	// The agent's own call, and a session that lost its agent, both meet its guards.
	for _, caller := range []string{`,"agent_type":"jhon-orchestrator"`, ""} {
		code, out, errs := runAgentHooks(t, strings.Replace(push, "%s", caller, 1))
		if code == 0 && !strings.Contains(out, "push") || !strings.Contains(out+errs, "push") {
			t.Errorf("caller %q: git push was not refused: code %d, out %q, err %q", caller, code, out, errs)
		}
	}
	// An agent Jhon does not render keeps its calls.
	if code, out, errs := runAgentHooks(t, strings.Replace(push, "%s", `,"agent_type":"Explore"`, 1)); code != 0 || out != "" || errs != "" {
		t.Errorf("Explore's call was judged: code %d, out %q, err %q", code, out, errs)
	}
	// A tool the matcher does not take runs nothing.
	if code, out, _ := runAgentHooks(t, `{"hook_event_name":"PreToolUse","tool_name":"Read","agent_type":"jhon-orchestrator"}`); code != 0 || out != "" {
		t.Errorf("Read was judged: code %d, out %q", code, out)
	}
	// SessionStart hands the session its agent's definition, the body after
	// the frontmatter; a session that lost its agent is also told so.
	_, out, _ := runAgentHooks(t, `{"hook_event_name":"SessionStart","source":"clear","cwd":"`+cwd+`"}`, "--part", "1")
	if !strings.Contains(out, "# Orchestrator") || strings.Contains(out, "safety-guard") || !strings.Contains(out, "systemMessage") {
		t.Errorf("a session without its agent did not get its definition and the notice: %q", out)
	}
	_, out, _ = runAgentHooks(t, `{"hook_event_name":"SessionStart","source":"startup","agent_type":"jhon-orchestrator","cwd":"`+cwd+`"}`, "--part", "1")
	if !strings.Contains(out, "# Orchestrator") || strings.Contains(out, "systemMessage") {
		t.Errorf("a session with its agent did not get only its definition: %q", out)
	}
	// A part past the definition's says nothing, and a part runs no guard.
	if _, out, _ = runAgentHooks(t, `{"hook_event_name":"SessionStart","cwd":"`+cwd+`"}`, "--part", "2"); out != "" {
		t.Errorf("part 2 of a one-part definition said %q", out)
	}
	// In a project with openspec/config.yaml, the project's rules are the
	// last part: its delivery and the change's route, read now.
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"commit", "-q", "--allow-empty", "-m", "base"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
	os.MkdirAll(filepath.Join(repo, "openspec"), 0o755)
	os.WriteFile(filepath.Join(repo, "openspec", "config.yaml"), []byte("delivery: change\n"), 0o644)
	_, out, _ = runAgentHooks(t, `{"hook_event_name":"SessionStart","agent_type":"jhon-orchestrator","cwd":"`+repo+`"}`, "--part", "2")
	for _, want := range []string{"Part 2 of 2", "One PR per change", "Route, from the change's diff now: standard", "outrank every memory"} {
		if !strings.Contains(out, want) {
			t.Errorf("the project's rules lack %q: %q", want, out)
		}
	}
	if code, out, errs := runAgentHooks(t, strings.Replace(push, "%s", "", 1), "--part", "1"); code != 0 || out+errs != "" {
		t.Errorf("a SessionStart part judged a tool call: code %d, %q %q", code, out, errs)
	}
}

func TestParts(t *testing.T) {
	text := "# Top\nintro\n## A\n" + strings.Repeat("a\n", 40) + "## B\n" + strings.Repeat("b\n", 40) + "## C\nc\n"
	parts := Parts(text, 100)
	if strings.Join(parts, "") != text {
		t.Fatalf("Parts lost text: %q", parts)
	}
	for i, p := range parts {
		if i > 0 && !strings.HasPrefix(p, "## ") {
			t.Errorf("part %d does not open on a heading: %q", i+1, p[:min(len(p), 20)])
		}
		if len(p) > 100 {
			t.Errorf("part %d is %d bytes", i+1, len(p))
		}
	}
	if many := Parts(strings.Repeat("## x\n"+strings.Repeat("y", 90)+"\n", 20), 100); len(many) != SessionParts || strings.Join(many, "") != strings.Repeat("## x\n"+strings.Repeat("y", 90)+"\n", 20) {
		t.Errorf("a long text took %d parts, want %d with nothing lost", len(many), SessionParts)
	}
}

func TestShellWords(t *testing.T) {
	words, ok := shellWords(`'/a b/jhon' 'hook' 'it'\''s' bare`)
	if !ok || strings.Join(words, "|") != "/a b/jhon|hook|it's|bare" {
		t.Fatalf("shellWords = %q, %v", words, ok)
	}
	if _, ok := shellWords(`'open`); ok {
		t.Fatal("an unclosed quote was read")
	}
}

// T31 D: pi's main session gets Claude Code's binding lead and the
// project's rules (`jhon hook session-brief <agent>`, payload {"cwd"}), only
// in a project that uses LDD; elsewhere the line that says LDD is inactive
// (T64).
func TestSessionBrief(t *testing.T) {
	brief := func(cwd string) (int, string, string) {
		var out, errs bytes.Buffer
		code := SessionBrief([]string{"jhon-orchestrator"}, IO{strings.NewReader(`{"cwd":"` + cwd + `"}`), &out, &errs})
		return code, out.String(), errs.String()
	}
	t.Setenv("JHON_ROOT", fakeRoot(t, true))
	inactive := marshal(map[string]string{"inactive": ldd.InactiveLineFor(os.Getenv("JHON_ROOT"))})
	plain := t.TempDir()
	if code, out, errs := brief(plain); code != 0 || out != inactive || errs != "" {
		t.Errorf("outside an LDD project: code %d, out %q, err %q", code, out, errs)
	}
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"commit", "-q", "--allow-empty", "-m", "base"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
	if code, out, _ := brief(repo); code != 0 || out != inactive {
		t.Errorf("a repository without openspec/: code %d, out %q", code, out)
	}
	os.MkdirAll(filepath.Join(repo, "openspec"), 0o755)
	os.WriteFile(filepath.Join(repo, "openspec", "config.yaml"), []byte("delivery: change\n"), 0o644)
	code, out, errs := brief(repo)
	var got struct{ Lead, Brief string }
	if code != 0 || errs != "" || json.Unmarshal([]byte(out), &got) != nil {
		t.Fatalf("in an LDD project: code %d, out %q, err %q", code, out, errs)
	}
	if want := "Jhon runs this session as the jhon-orchestrator agent. Its definition is binding: it says how this session works, whatever the session's own system prompt says."; got.Lead != want {
		t.Errorf("lead = %q, want %q", got.Lead, want)
	}
	if !strings.HasPrefix(fmt.Sprintf(definitionLead, "jhon-orchestrator", 1, 2), got.Lead+" Part 1 of 2") {
		t.Errorf("the lead is not Claude Code's: %q", got.Lead)
	}
	for _, want := range []string{"## This project's rules", "One PR per change", "Route, from the change's diff now: standard", "outrank every memory"} {
		if !strings.Contains(got.Brief, want) {
			t.Errorf("the brief lacks %q: %q", want, got.Brief)
		}
	}
	if code := SessionBrief(nil, IO{strings.NewReader("{}"), &bytes.Buffer{}, &bytes.Buffer{}}); code != 2 {
		t.Errorf("no agent: code %d, want 2", code)
	}
	if Handlers["session-brief"] == nil {
		t.Error("session-brief is not a hook")
	}
}

// T42b (L403): when jhon start's launch already made the agent's definition
// the system prompt (JHON_AGENT_LOADED names the agent, set only on a
// print-mode launch, where --agent loads it) and the session starts fresh,
// SessionStart hands no body part; the owner's shell note and the project's
// rules, which the system prompt lacks, still come in part 1. Resume, /clear,
// compaction, a session that lost its agent, and a start without the signal
// get the parts as before.
func TestAgentHooksBodyOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/usr/bin/fish")
	dir := filepath.Join(home, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := agentFile + strings.Repeat("\n## Section\n"+strings.Repeat("rule line\n", 900), 2)
	if err := os.WriteFile(filepath.Join(dir, "jhon-orchestrator.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"commit", "-q", "--allow-empty", "-m", "base"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
	os.MkdirAll(filepath.Join(repo, "openspec"), 0o755)
	os.WriteFile(filepath.Join(repo, "openspec", "config.yaml"), []byte("delivery: change\n"), 0o644)
	session := func(source, agentType string) []string {
		t.Helper()
		caller := ""
		if agentType != "" {
			caller = `,"agent_type":"` + agentType + `"`
		}
		var outs []string
		for part := 1; part <= SessionParts; part++ {
			code, out, errs := runAgentHooks(t, `{"hook_event_name":"SessionStart","source":"`+source+`"`+caller+`,"cwd":"`+repo+`"}`, "--part", fmt.Sprint(part))
			if code != 0 || errs != "" {
				t.Fatalf("%s part %d: code %d, %q", source, part, code, errs)
			}
			outs = append(outs, out)
		}
		return outs
	}
	hasBody := func(outs []string) bool { return strings.Contains(strings.Join(outs, ""), "# Orchestrator") }

	t.Run("startup with the signal: no body part, the shell note and the project's rules kept", func(t *testing.T) {
		t.Setenv("JHON_AGENT_LOADED", "jhon-orchestrator")
		outs := session("startup", "jhon-orchestrator")
		if hasBody(outs) || strings.Contains(strings.Join(outs, ""), "rule line") {
			t.Errorf("the body came again: %q", outs)
		}
		for _, want := range []string{"The owner's shell is fish", "One PR per change", "jhon-orchestrator agent"} {
			if !strings.Contains(outs[0], want) {
				t.Errorf("part 1 lacks %q: %q", want, outs[0])
			}
		}
		if outs[1]+outs[2]+outs[3] != "" {
			t.Errorf("parts 2-4 said %q", outs[1:])
		}
	})

	t.Run("startup without the signal: the parts as today", func(t *testing.T) {
		t.Setenv("JHON_AGENT_LOADED", "")
		outs := session("startup", "jhon-orchestrator")
		if !hasBody(outs) || !strings.Contains(outs[0], "Part 1 of") {
			t.Errorf("no body: %q", outs)
		}
	})

	t.Run("resume, clear and compact with the signal: the parts as today", func(t *testing.T) {
		t.Setenv("JHON_AGENT_LOADED", "jhon-orchestrator")
		for _, source := range []string{"resume", "clear", "compact"} {
			if outs := session(source, "jhon-orchestrator"); !hasBody(outs) || !strings.Contains(outs[0], "Part 1 of") {
				t.Errorf("%s: no body: %q", source, outs)
			}
		}
	})

	t.Run("a session that lost its agent, or the signal naming another, gets the parts", func(t *testing.T) {
		t.Setenv("JHON_AGENT_LOADED", "jhon-orchestrator")
		if outs := session("startup", ""); !hasBody(outs) {
			t.Errorf("lost agent: no body: %q", outs)
		}
		t.Setenv("JHON_AGENT_LOADED", "other")
		if outs := session("startup", "jhon-orchestrator"); !hasBody(outs) {
			t.Errorf("another agent's signal: no body: %q", outs)
		}
	})
}

// T64 A (L501), T72 (L555): Claude Code's session start says LDD's state
// as pi's prompt does: where LDD is not set up yet the one line
// ldd.InactiveFor, the same text session-brief hands pi; a project with a
// layout gets its rules and no such line.
func TestSessionStartSaysLddState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("SHELL", "/bin/bash")
	dir := filepath.Join(home, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "jhon-orchestrator.md"), []byte(agentFile), 0o644); err != nil {
		t.Fatal(err)
	}
	plain, active := gitRepo(t, false), gitRepo(t, true)
	// L534: the line names the init steps of the install the hook runs
	// from by their absolute path, so no agent expands ~ to a home it
	// guesses; with no init.md there it keeps the ~ text.
	root := fakeRoot(t, true)
	t.Setenv("JHON_ROOT", root)
	inactive := setUpLine(filepath.Join(root, initRel))
	if got := ldd.InactiveLineFor(root); got != inactive {
		t.Errorf("InactiveLineFor(root) = %q", got)
	}
	for _, signal := range []string{"", "jhon-orchestrator", "no init.md"} {
		t.Run("signal "+signal, func(t *testing.T) {
			line := inactive
			if signal == "no init.md" {
				signal, line = "", fallbackLine
				t.Setenv("JHON_ROOT", fakeRoot(t, false))
			}
			t.Setenv("JHON_AGENT_LOADED", signal)
			got := sessionContext(t, plain)
			if strings.Count(got, line) != 1 || strings.Contains(got, "This project's rules") {
				t.Errorf("plain: the session start does not say once that LDD is not set up: %q", got)
			}
			if want := marshal(map[string]string{"inactive": line}); sessionBrief(t, plain) != want {
				t.Errorf("plain: session-brief = %q, want %q", sessionBrief(t, plain), want)
			}
			got = sessionContext(t, active)
			if strings.Contains(got, "LDD is not set up") || !strings.Contains(got, "## This project's rules") {
				t.Errorf("active: %q", got)
			}
		})
	}
}

// gitRepo is a repository with one commit, and an openspec/ config when openspec.
func gitRepo(t *testing.T, openspec bool) string {
	t.Helper()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"commit", "-q", "--allow-empty", "-m", "base"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
	if openspec {
		os.MkdirAll(filepath.Join(repo, "openspec"), 0o755)
		os.WriteFile(filepath.Join(repo, "openspec", "config.yaml"), []byte("delivery: change\n"), 0o644)
	}
	return repo
}

// sessionContext is every part Claude Code's session start hands a session in cwd.
func sessionContext(t *testing.T, cwd string) string {
	t.Helper()
	var all []string
	for part := 1; part <= SessionParts; part++ {
		code, out, errs := runAgentHooks(t, `{"hook_event_name":"SessionStart","source":"startup","agent_type":"jhon-orchestrator","cwd":"`+cwd+`"}`, "--part", fmt.Sprint(part))
		if code != 0 || errs != "" {
			t.Fatalf("part %d: code %d, %q", part, code, errs)
		}
		if out == "" {
			continue
		}
		var got struct {
			HookSpecificOutput struct{ AdditionalContext string } `json:"hookSpecificOutput"`
		}
		if json.Unmarshal([]byte(out), &got) != nil {
			t.Fatalf("part %d: %q", part, out)
		}
		all = append(all, got.HookSpecificOutput.AdditionalContext)
	}
	return strings.Join(all, "\n")
}

// sessionBrief is what pi's main session in cwd is handed.
func sessionBrief(t *testing.T, cwd string) string {
	t.Helper()
	var out bytes.Buffer
	if code := SessionBrief([]string{"jhon-orchestrator"}, IO{strings.NewReader(`{"cwd":"` + cwd + `"}`), &out, &bytes.Buffer{}}); code != 0 {
		t.Fatalf("session-brief: code %d", code)
	}
	return out.String()
}

// initRel is the init steps under an install's root; fallbackLine is the
// line where the running install has none, naming them as the runtime prose
// does, under ~/.local/share/jhon/current.
const initRel = "prose/skills/ldd/references/init.md"

var fallbackLine = setUpLine("~/.local/share/jhon/current/" + initRel)

// setUpLine is the line a session is told where LDD is not set up yet
// (T72, L555, L557, L559), naming the init steps at steps.
func setUpLine(steps string) string {
	return "LDD is not set up in this project yet (`jhon status`); read-only work sets up nothing." +
		" At your first code change, set it up by following `" + steps + "`: ask how to deliver and test by your ask tool only (else the recommended values, never asked in prose)," +
		" say in one line what you set up, then work under LDD on the change's own branch. Until it is set up, a commit or a PR is refused."
}

// fakeRoot is an install root with the tree's contracts, holding init.md
// when withInit.
func fakeRoot(t *testing.T, withInit bool) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, initRel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(os.Getenv("JHON_ROOT"), "prose", "contracts"), filepath.Join(root, "prose", "contracts")); err != nil {
		t.Fatal(err)
	}
	if withInit {
		if err := os.WriteFile(filepath.Join(root, initRel), []byte("# init\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestInactiveLineNamesTheInitSteps (T64, L506, L534): a session with no
// Skill tool cannot run /jhon-ldd init, so the line names the file whose
// steps it reads instead: the tree's own, by its absolute path; a root
// without it, or none, gets the ~ text, never a path that does not exist.
func TestInactiveLineNamesTheInitSteps(t *testing.T) {
	tree, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	line := ldd.InactiveLineFor(tree)
	if !strings.Contains(line, "`"+filepath.Join(tree, initRel)+"`") || strings.Contains(line, "~") || strings.Contains(line, "/jhon-ldd") {
		t.Errorf("InactiveLineFor(tree) = %q", line)
	}
	for _, root := range []string{"", t.TempDir()} {
		if got := ldd.InactiveLineFor(root); got != fallbackLine {
			t.Errorf("InactiveLineFor(%q) = %q", root, got)
		}
	}
	// Every byte is in every prompt of a project without LDD (L538, L555);
	// 436 holds the ask-tool-only rule (T74, L572).
	if len(fallbackLine) > 436 {
		t.Errorf("the ~ line is %d bytes", len(fallbackLine))
	}
}

// TestOldSwitchOffToldOnce (T72, L555): `jhon ldd off` is gone; a project
// its record still says off is treated as on, its rules bind, and its
// sessions are told so once, on Claude Code and pi alike.
func TestOldSwitchOffToldOnce(t *testing.T) {
	t.Setenv("JHON_ROOT", fakeRoot(t, true))
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("SHELL", "/bin/bash")
	t.Setenv("JHON_AGENT_LOADED", "")
	dir := filepath.Join(home, ".claude", "agents")
	os.MkdirAll(dir, 0o755)
	if err := os.WriteFile(filepath.Join(dir, "jhon-orchestrator.md"), []byte(agentFile), 0o644); err != nil {
		t.Fatal(err)
	}
	const notice = "LDD is always on with Jhon now; the old switch-off no longer applies."
	off := func(t *testing.T, openspec bool) string {
		repo := gitRepo(t, openspec)
		os.MkdirAll(lddswitch.Dir(), 0o700)
		b, _ := json.Marshal(map[string]string{"ldd": "off", "root": repo, "since": "2026-10-01T00:00:00Z"})
		if err := os.WriteFile(lddswitch.File(repo), b, 0o600); err != nil {
			t.Fatal(err)
		}
		return repo
	}
	// Claude Code: the rules, and the notice once, then never again.
	repo := off(t, true)
	got := sessionContext(t, repo)
	if !strings.Contains(got, "## This project's rules") || strings.Count(got, notice) != 1 {
		t.Errorf("session start: %q", got)
	}
	if got := sessionContext(t, repo); strings.Contains(got, notice) || !strings.Contains(got, "## This project's rules") {
		t.Errorf("session start, again: %q", got)
	}
	if got := sessionBrief(t, repo); strings.Contains(got, notice) {
		t.Errorf("session-brief after the session start told it: %q", got)
	}
	// pi: the same, in the brief.
	repo = off(t, true)
	var b struct{ Lead, Brief string }
	if json.Unmarshal([]byte(sessionBrief(t, repo)), &b) != nil || !strings.Contains(b.Brief, "## This project's rules") || strings.Count(b.Brief, notice) != 1 {
		t.Errorf("session-brief: %+v", b)
	}
	if got := sessionBrief(t, repo); strings.Contains(got, notice) {
		t.Errorf("session-brief, again: %q", got)
	}
	// A project with no layout is told it after the set-up line, once.
	plain := off(t, false)
	want := marshal(map[string]string{"inactive": ldd.InactiveLineFor(os.Getenv("JHON_ROOT")) + " " + notice})
	if got := sessionBrief(t, plain); got != want {
		t.Errorf("session-brief, no layout = %q, want %q", got, want)
	}
	if got, want := ldd.InactiveFor(plain), ldd.InactiveLineFor(os.Getenv("JHON_ROOT")); got != want {
		t.Errorf("InactiveFor(no layout) = %q", got)
	}
}
