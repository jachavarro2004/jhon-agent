package audit

import (
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"os"
	"path/filepath"
	"testing"
)

func TestInvocations(t *testing.T) {
	via := func(cmd string) [][2]string {
		var out [][2]string
		for _, x := range Invocations(cmd) {
			out = append(out, [2]string{x.Program + " " + x.Sub, x.Via})
		}
		return out
	}
	eq(t, "wrappers", via("env GIT_TRACE=0 git -C /r push --force; sudo -u x timeout 5 rm -rf /a"), [][2]string{{"git push", "env"}, {"rm /a", "sudo > timeout"}})
	eq(t, "sh -c", via(`bash -lc "git status && npm test"`), [][2]string{{"git status", "bash -c"}, {"npm test", "bash -c"}})
	eq(t, "a path is its own route", via("/usr/bin/git diff a b"), [][2]string{{"git diff", "/usr/bin/git"}})
	eq(t, "redirections dropped", via("cat a.txt 2>&1 > out.log | head -5"), [][2]string{{"cat a.txt", "plain"}, {"head ", "plain"}})
	code := Invocations(`python3 -c 'import os; os.remove("x")'`)
	if len(code) != 1 || code[0].Code == nil || *code[0].Code != `import os; os.remove("x")` {
		t.Errorf("interpreter code = %+v", code)
	}
	eq(t, "heredoc", WithoutHeredocs("cat > f <<'EOF'\ngit push\nEOF\ngit status"), "cat > f <<'EOF'\ngit status")
}

func TestCommandKinds(t *testing.T) {
	for cmd, want := range map[string]string{
		// A push delivers only into the default branch (the owner's decision,
		// docket AY B6); a change branch's push is review.
		"git push origin feat": "", "git push -u origin fix/x": "", "git push": "", "git push origin main": "push",
		"git push origin HEAD:main": "push", "git push -f origin +master": "push", "git switch main && git push": "push",
		"git push --dry-run": "", "git push --dry-run origin main": "", "jhon publish": "push", "~/.local/bin/jhon publish": "push",
		"npx cdk deploy Stack": "deploy", "./deploy.sh prod": "deploy", "kubectl apply -f x": "deploy", "helm upgrade a b": "deploy",
		"docker push img": "deploy", "gh workflow run deploy-prod.yml": "deploy", "gh release create v1": "release", "npm publish": "release",
		"git status": "", "cat > n <<'EOF'\ngit push\nEOF": "",
		// A case pattern is no program (session 49dcc77e: `deploy*` read as a deploy).
		"for f in *; do case $f in compliance*|deploy*|x*) continue;; esac; done": "",
	} {
		eq(t, cmd, DeliveryCommand(cmd), want)
	}
	for cmd, want := range map[string]bool{
		"gh pr merge 12": true, "git checkout main && git merge feat": true, "git checkout main && git merge origin/main": false,
		"git merge-base HEAD origin/main": false, "git merge feat": false,
	} {
		eq(t, cmd, MergesToDefault(cmd), want)
	}
	for cmd, want := range map[string]bool{
		"npm test": true, "npm run test": true, "python3 -m pytest": true, "uv run py.test": true, "go test ./...": true, "node --test t/": true,
		"npm run build": false, "jhon executions run x": false,
	} {
		eq(t, cmd, DirectTest(cmd), want)
	}
	eq(t, "jhon operand", jhonOperand("jhon executions run --timeout 30 my-change", "executions", "run"), "my-change")
	eq(t, "a dry run", jhonOperand("jhon executions run my-change --dry-run", "executions", "run"), "")
	eq(t, "status operand", statusOperand("jhon status --root /r my-change"), "my-change")
}

func TestRunVerdictAndReaders(t *testing.T) {
	check := func(statuses []string, clean, dirty, complete bool, passed bool, why string) {
		t.Helper()
		p, w := RunVerdict(statuses, clean, dirty, complete)
		eq(t, "verdict", [2]any{p, w}, [2]any{passed, why})
	}
	check([]string{"pass"}, true, true, true, false, "its tests passed, but the tree changed during the run")
	check([]string{"pass", "timeout"}, true, false, true, false, "1 of 2 project(s) timeout")
	check([]string{"pass"}, true, false, false, false, "it did not complete")
	check([]string{"pass"}, true, false, true, true, "")
	check(nil, true, false, true, false, "it ran no project")

	x := ReadExecutions(`{"executions":[{"status":"pass"},{"exit_code":0}],"working_tree_clean":true,"candidate_commit":"c","candidate_tree":"t"}`)
	eq(t, "json executions", *x, Executions{Passed: true, Commit: "c", Tree: "t", Statuses: []string{"pass", "pass"}})
	x = ReadExecutions(executionsOut("c", "t", []string{"pass", "fail"}))
	eq(t, "human executions", *x, Executions{Why: "1 of 2 project(s) fail", Commit: "c", Tree: "t", Statuses: []string{"pass", "fail"}})
	if ReadExecutions("nothing") != nil {
		t.Error("no executions in plain text")
	}
	c := ReadDeliverCheck("deploy  ready  artifact a@sha256:1\nhead    h  tree t\ntested  h  tree u\nrisk    high")
	eq(t, "deliver check", [5]any{c.Action, *c.Artifact, c.Commit, c.TestedTree, c.Risked}, [5]any{"deploy", "a@sha256:1", "h", "u", true})
	if ReadDeliverCheck(`{"ready":false}`) != nil {
		t.Error("a check that is not ready")
	}
	eq(t, "risk tier", [2]string{riskTier("risk: high (base main)"), riskTier(`{"tier":"medium"}`)}, [2]string{"high", "medium"})
}

func TestRefusalReading(t *testing.T) {
	r := ToolResult{IsError: true, Text: hookText("inspect-guard", "prism-safety", "shell syntax is not allowed here\nallowed forms: git diff, git show")}
	guard, _, ask, ok := SafetyRefusal(r, "prism-safety")
	eq(t, "inspect-guard on a reviewer", [3]any{guard, ask, ok}, [3]any{"inspect-guard", false, true})
	if _, _, _, ok := SafetyRefusal(r, "jhon-builder"); ok {
		t.Error("inspect-guard binds reviewers only")
	}
	_, _, ask, _ = SafetyRefusal(ToolResult{IsError: true, Text: safetyAsk("git push", "push")}, "general")
	eq(t, "an asked refusal", ask, true)
	eq(t, "forms", DeclaredForms(r.Text), [][]string{{"git", "diff"}, {"git", "show"}})
	eq(t, "a denial", IsRefusal(ToolResult{IsError: true, Text: "x"}, "user-rejected"), true)
	eq(t, "an operational failure", IsRefusal(ToolResult{IsError: true, Text: "exit 1"}, ""), false)
	eq(t, "mentions", [3]bool{mentions("refused git push --force.", "--force"), mentions("pushed", "push"), mentions("(push)", "push")}, [3]bool{true, false, true})
	eq(t, "sentences", splitSentences("Run a. Then b;  c\nd"), []string{"Run a.", "Then b;", "c", "d"})
	_, ok = asksFor("Do not git push --force; open a PR.", effect{program: "git", operands: []string{"push"}, options: []string{"--force"}})
	eq(t, "a negated ask", ok, false)
}

func TestLaunchReading(t *testing.T) {
	block := "```jhon-context\n{\"change\": \"c1\", \"phase\": \"apply\", \"project_root\": \"/r/x/\"}\n```\nApply it."
	eq(t, "change", changeOf(block), "c1")
	eq(t, "phase", phaseOf(block, "jhon-builder"), "apply")
	eq(t, "a null change", changeOf("```jhon-context\n{\"change\": null}\n```\nopenspec/changes/c2/"), "")
	eq(t, "a named path", changeOf("Apply openspec/changes/c2/tasks.md and openspec/changes/archive/x."), "c2")
	eq(t, "a change line", changeOf("- change: `c3`\n"), "c3")
	eq(t, "verify", phaseOf("", "jhon-checker"), "verify")
	eq(t, "no phase", phaseOf("", "jhon-builder"), "")
	a := &Subagent{LaunchPrompt: block}
	eq(t, "root", rootOf(a), "/r/x")
	eq(t, "named paths", namedPaths("```jhon-context\n{\"x\": \"a/b.ts\"}\n```\nEdit `/r/src/a.ts` and ./docs/ and src/b.ts.", "/r"), []string{"src/a.ts", "docs/", "src/b.ts"})
	eq(t, "normalize", [4]string{normalize("a//b/../c/"), normalize(""), normalizeRoot("/a/b/"), absolute("x/../y", "/r")}, [4]string{"a/c/", ".", "/a/b", "/r/y"})
}

func TestMeasurementsReading(t *testing.T) {
	eq(t, "cat", ShellReads("cat", []string{"a", "-"}), []ShellRead{{"a", true}})
	eq(t, "sed -n", ShellReads("sed", []string{"-n", "1,5p", "f"}), []ShellRead{{"f", false}})
	eq(t, "sed -i", len(ShellReads("sed", []string{"-n", "-i", "s/a/b/p", "f"})), 0)
	eq(t, "bat -r", ShellReads("bat", []string{"-r", "1:10", "f"}), []ShellRead{{"f", false}})
	eq(t, "a pipe", len(ShellReads("head", []string{"-n", "5"})), 0)
	first, last, ok := ShownRange("     1\ta\n     2\tb\n     4\td")
	eq(t, "range", [3]any{first, last, ok}, [3]any{1.0, 2.0, true})
	eq(t, "pipes", [3]bool{pipes("a | b"), pipes("a || b"), pipes("echo 'a|b'")}, [3]bool{true, false, false})
	for name, want := range map[string]string{"Read": "full_read_bytes", "Grep": "search_bytes", "mcp__codegraph__x": "codegraph_bytes", "mcp__plugin_engram_engram__x": "engram_bytes", "mcp__other__x": "other_bytes"} {
		eq(t, name, BytesSource(ToolUse{Name: name, Input: obj()}), want)
	}
	eq(t, "durations", [4]string{Duration(29_400), Duration(90_000), Duration(5_400_000), Duration(9_000_000)}, [4]string{"29s", "2m", "1.5h", "2.5h"})
	eq(t, "case folding", ci(`\bno se usa\b|ó`), `\b[nN][oO] [sS][eE] [uU][sS][aA]\b|[óÓ]`)
	eq(t, "a JS i flag folds no ſ", stoppedShort.MatchString("MAX TURNS"), true)
	eq(t, "short", short("a\n\tb  c", 120), "a b c")
	eq(t, "short cuts", short("abcdef", 4), "abc…")
}

func TestResolveTarget(t *testing.T) {
	projects := t.TempDir()
	dir := filepath.Join(projects, "-work-p")
	must(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, "0123abcd-0000-4000-8000-000000000001.jsonl")
	must(t, os.WriteFile(file, []byte("{}\n"), 0o644))
	eq(t, "an id", ResolveTarget("0123abcd-0000-4000-8000-000000000001", "/", projects), file)
	eq(t, "a prefix", ResolveTarget("0123abcd", "/", projects), file)
	eq(t, "a short prefix", ResolveTarget("0123a", "/", projects), "")
	eq(t, "a path", ResolveTarget("-work-p/0123abcd-0000-4000-8000-000000000001.jsonl", projects, projects), file)
	eq(t, "a missing path", ResolveTarget("/nope.jsonl", "/", projects), "")

	projects = t.TempDir()
	for _, f := range []string{"p1/abcdef12-0000-4000-8000-000000000001.jsonl", "p2/abcdef99-0000-4000-8000-000000000002.jsonl"} {
		must(t, os.MkdirAll(filepath.Join(projects, filepath.Dir(f)), 0o755))
		must(t, os.WriteFile(filepath.Join(projects, f), nil, 0o644))
	}
	p1 := filepath.Join(projects, "p1", "abcdef12-0000-4000-8000-000000000001.jsonl")
	eq(t, "a unique prefix", ResolveTarget("abcdef12", "/", projects), p1)
	eq(t, "the full id", ResolveTarget("abcdef12-0000-4000-8000-000000000001", "/", projects), p1)
	eq(t, "an ambiguous prefix", ResolveTarget("abcdef", "/", projects), "")
	eq(t, "too short", ResolveTarget("abcde", "/", projects), "")
}

// A path in what a call writes (a file's text, a heredoc) names no change the
// session works on; a path the call works on, or a command's operand, does.
func TestNamedByReadsPathsNotBodies(t *testing.T) {
	fixture := "def test_x():\n    assert pick(\"openspec/changes/x/tasks.md\")\n"
	cases := []struct {
		name  string
		input *js.Object
		want  string
	}{
		{"Write", obj("file_path", "/r/tests/test_pick.py", "content", fixture), ""},
		{"Edit", obj("file_path", "/r/openspec/changes/real/tasks.md", "old_string", "openspec/changes/x/", "new_string", "y"), "real"},
		{"Bash", obj("command", "cat > tests/test_pick.py <<'EOF'\n"+fixture+"pass\nEOF\npytest tests"), ""},
		{"Bash", obj("command", "cat <<EOF > /r/openspec/changes/real/notes.md\nopenspec/changes/x/\nEOF"), "real"},
		{"Bash", obj("command", "jhon status real --json"), "real"},
	}
	for _, c := range cases {
		if got := namedBy(ToolUse{Name: c.name, Input: c.input}); got != c.want {
			t.Errorf("%s %s: named %q, want %q", c.name, js.StringifyValueCompact(c.input), got, c.want)
		}
	}
}
