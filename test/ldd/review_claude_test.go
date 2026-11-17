package ldd

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// The native 4R's Claude Code variant (T118d, L704, L706, L1150): `jhon
// review run` runs each lens prompt `start` gives through `claude -p`, all
// lenses at once, then the refuter and the validator the same way, then
// `record`; any call that fails records nothing and names the lens agents.
// A fake `claude` stands in: it records its argv and stdin and answers what
// the test put in its directory. The real claude never runs.

// claudeArgs is the exact argv of one review call on model: no settings
// load, and --effort only when the profile names one.
func claudeArgs(model, effort string) []string {
	args := []string{"-p", "--tools", "", "--disallowedTools", "mcp__*", "--output-format", "json", "--max-turns", "1", "--no-session-persistence", "--setting-sources", ""}
	if effort != "" {
		args = append(args, "--effort", effort)
	}
	return append(args, "--model", model)
}

// The models the fake tells apart, one per routing key.
const claudeProfile = `{"active":"p","profiles":{"p":{"agents":{` +
	`"review-risk":{"claude-code":"m-risk","pi":"prov/pi-risk@low"},"review-reliability":{"claude-code":"m-rel@high"},` +
	`"review-refuter":{"claude-code":"m-ref@low"},"review-validator":{"claude-code":"m-val"}}}}}`

// fakeClaude writes a `claude` into a fresh directory: each call appends its
// model to order, writes its argv (one per line) to <model>.args and its
// stdin to <model>.stdin; a lens call waits until both lenses have arrived
// (10 s at most, else exit 3), so lenses run one after the other fail. Then
// exit.<model> makes it exit with that code, sleep.<model> makes it hang,
// else it prints answer.<model>.
func fakeClaude(t *testing.T) (dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake claude is a POSIX shell script")
	}
	dir = temp(t)
	script := `#!/bin/sh
log='` + dir + `'
model=""; prev=""
for a in "$@"; do [ "$prev" = "--model" ] && model="$a"; prev="$a"; done
printf '%s\n' "$@" > "$log/$model.args"
cat > "$log/$model.stdin"
echo "$model" >> "$log/order"
case "$model" in m-risk|m-rel)
  : > "$log/arrived.$model"
  i=0
  while [ ! -f "$log/arrived.m-risk" ] || [ ! -f "$log/arrived.m-rel" ]; do
    i=$((i+1)); [ $i -gt 200 ] && { echo "barrier: the other lens never came" >&2; exit 3; }
    sleep 0.05
  done ;;
esac
[ -f "$log/exit.$model" ] && { echo "API Error: overloaded" >&2; exit "$(cat "$log/exit.$model")"; }
[ -f "$log/sleep.$model" ] && exec sleep 30
cat "$log/answer.$model"
`
	support.Write(t, dir, map[string]string{"claude": script})
	if err := os.Chmod(filepath.Join(dir, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// envelope is claude -p's --output-format json answer whose result is text.
func envelope(t *testing.T, text string) string {
	t.Helper()
	return mustJSON(t, map[string]any{"type": "result", "subtype": "success", "is_error": false, "duration_ms": 1200, "duration_api_ms": 1100,
		"num_turns": 1, "result": text, "session_id": "00000000-0000-4000-8000-000000000000", "total_cost_usd": 0.01,
		"usage": map[string]any{"input_tokens": 10, "output_tokens": 20}})
}

// claudeAnswers puts the canned answers for subject in dir: risk finds
// nothing, reliability one WARNING that stands and is unconfirmed. It returns
// the record's input the pi path sends for the same answers.
func claudeAnswers(t *testing.T, dir, subject string) string {
	t.Helper()
	finding := map[string]any{"location": "src/app.py:2", "severity": "WARNING", "claim": "sum of nothing is 0", "proof_refs": []string{"src/app.py:2"}}
	risk, rel := lensJSON(subject, "risk"), lensJSON(subject, "reliability", finding)
	refuter := role(subject, "refuter", map[string]any{"finding_id": "reliability-1", "outcome": "stands", "reason": "src/app.py:2"})
	validator := role(subject, "validator", map[string]any{"finding_id": "reliability-1", "outcome": "unconfirmed", "reason": "no caller passes nothing"})
	support.Write(t, dir, map[string]string{
		"answer.m-risk": envelope(t, "Reviewed.\n"+mustJSON(t, risk)),
		"answer.m-rel":  envelope(t, "```json\n"+mustJSON(t, rel)+"\n```"),
		"answer.m-ref":  envelope(t, mustJSON(t, refuter)),
		"answer.m-val":  envelope(t, mustJSON(t, validator)),
	})
	return mustJSON(t, map[string]any{"lenses": []any{risk, rel}, "refuter": refuter, "validator": validator})
}

func withPath(env []string, dir string) []string {
	return append(env, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func lines(t *testing.T, file string) []string {
	t.Helper()
	return strings.Split(strings.TrimSuffix(support.Read(t, file), "\n"), "\n")
}

func TestReviewRunClaude(t *testing.T) {
	t.Parallel()
	root := nativeRepo(t)
	fake := fakeClaude(t)
	env := withPath(nativeEnv(t, claudeProfile), fake)
	start := startReview(t, root, env)
	subject := start["subject"].(string)
	piInput := claudeAnswers(t, fake, subject)

	r := reviewRun(t, root, env, "", "run")
	equal(t, r.Code, 0, r.Stderr)
	equal(t, r.Stderr, "", "nothing on stderr")
	match(t, r.Stdout, `^4R native review \(claude -p\): verdict pass · lenses risk, reliability · record openspec/changes/c1/review-4r\.md\n`)
	match(t, r.Stdout, `(?m)^correction set 0 · for verify 0 · follow-ups 0 · information 1 · refuted 0 · unavailable 0$`)

	// Exact flags, the profile's Claude Code model and effort per routing
	// key (no --effort when the profile names none), and each prompt on
	// stdin as start gives it.
	prompts := map[string]string{}
	for _, l := range start["lenses"].([]any) {
		m := l.(map[string]any)
		prompts[m["routing_key"].(string)] = m["prompt"].(string)
	}
	effort := map[string]string{"m-risk": "", "m-rel": "high", "m-ref": "low", "m-val": ""}
	for model, key := range map[string]string{"m-risk": "review-risk", "m-rel": "review-reliability"} {
		equal(t, lines(t, filepath.Join(fake, model+".args")), claudeArgs(model, effort[model]), key+"'s argv")
		equal(t, support.Read(t, filepath.Join(fake, model+".stdin")), prompts[key], key+"'s prompt on stdin")
	}
	for _, model := range []string{"m-ref", "m-val"} {
		equal(t, lines(t, filepath.Join(fake, model+".args")), claudeArgs(model, effort[model]), model+"'s argv")
	}
	match(t, support.Read(t, filepath.Join(fake, "m-ref.stdin")), `"role": "refuter"`, "the refuter's prompt")
	match(t, support.Read(t, filepath.Join(fake, "m-ref.stdin")), `"id": "reliability-1"`)
	match(t, support.Read(t, filepath.Join(fake, "m-val.stdin")), `"role": "validator"`, "the validator's prompt")

	// The lenses met at the fake's barrier (in parallel), then the refuter,
	// then the validator.
	order := lines(t, filepath.Join(fake, "order"))
	equal(t, len(order), 4, order)
	equal(t, sortedCopy(order[:2]), []string{"m-rel", "m-risk"}, "the lenses first")
	equal(t, order[2:], []string{"m-ref", "m-val"}, "then the refuter, then the validator")

	// The record is the one the pi path writes for the same answers.
	file := filepath.Join(root, "openspec/changes/c1/review-4r.md")
	got := support.Read(t, file)
	match(t, got, "(?m)^subject: "+subject+"$")
	match(t, got, "(?m)^lenses: risk, reliability$")
	pi := reviewRun(t, root, env, piInput, "record", "--subject", subject)
	equal(t, pi.Code, 0, pi.Stderr)
	equal(t, got, support.Read(t, file), "the same record as the pi path's")
	equal(t, reviews(t, root)["4r"]["done"], true, "the record holds the 4R")
}

func sortedCopy(xs []string) []string {
	out := slices.Clone(xs)
	slices.Sort(out)
	return out
}

// Without a finding there is nothing to refute or validate: only the lens
// calls run.
func TestReviewRunClaudeNoFindings(t *testing.T) {
	t.Parallel()
	root := nativeRepo(t)
	fake := fakeClaude(t)
	env := withPath(nativeEnv(t, claudeProfile), fake)
	subject := startReview(t, root, env)["subject"].(string)
	support.Write(t, fake, map[string]string{
		"answer.m-risk": envelope(t, mustJSON(t, lensJSON(subject, "risk"))),
		"answer.m-rel":  envelope(t, mustJSON(t, lensJSON(subject, "reliability"))),
	})
	r := reviewRun(t, root, env, "", "run")
	equal(t, r.Code, 0, r.Stderr)
	equal(t, sortedCopy(lines(t, filepath.Join(fake, "order"))), []string{"m-rel", "m-risk"}, "no refuter or validator call")
	match(t, support.Read(t, filepath.Join(root, "openspec/changes/c1/review-4r.md")), "(?m)^verdict: pass$")
}

// Any failed call gives one line that says why and names the lens agents,
// exits 1 and records nothing (K2).
func TestReviewRunClaudeFailures(t *testing.T) {
	t.Parallel()
	const fallback = `; nothing recorded\. Fallback: launch the review-risk and review-reliability agents, then the review-refuter and review-validator agents, as the 4R contract's fallback says\n$`
	type tc struct {
		name, why string
		args      []string
		set       func(t *testing.T, fake, subject string)
		path      func(t *testing.T, fake string) string
	}
	cases := []tc{
		{name: "no claude on PATH", why: `claude is not on PATH`, path: func(t *testing.T, fake string) string {
			git, err := exec.LookPath("git")
			if err != nil {
				t.Fatal(err)
			}
			bare := temp(t)
			if err := os.Symlink(git, filepath.Join(bare, "git")); err != nil {
				t.Fatal(err)
			}
			return "PATH=" + bare
		}},
		{name: "a non-zero exit", why: `the reliability lens call failed: claude exited 7: API Error: overloaded`, set: func(t *testing.T, fake, _ string) {
			support.Write(t, fake, map[string]string{"exit.m-rel": "7"})
		}},
		{name: "unparsable JSON", why: `the risk lens call failed: claude's output is not its JSON result`, set: func(t *testing.T, fake, _ string) {
			support.Write(t, fake, map[string]string{"answer.m-risk": "not json\n"})
		}},
		{name: "an error result", why: `the risk lens call failed: claude reported an error \(error_max_turns\)`, set: func(t *testing.T, fake, _ string) {
			support.Write(t, fake, map[string]string{"answer.m-risk": `{"type":"result","subtype":"error_max_turns","is_error":true,"num_turns":1}`})
		}},
		{name: "an answer for another subject", why: `the risk lens call failed: its answer is not one line of JSON for the risk lens and this subject`, set: func(t *testing.T, fake, _ string) {
			support.Write(t, fake, map[string]string{"answer.m-risk": envelope(t, mustJSON(t, lensJSON(strings.Repeat("1", 64), "risk")))})
		}},
		{name: "a timeout", why: `the risk lens call failed: claude did not answer within 1s`, args: []string{"--timeout", "1"}, set: func(t *testing.T, fake, _ string) {
			support.Write(t, fake, map[string]string{"sleep.m-risk": ""})
		}},
		{name: "the refuter fails", why: `the refuter call failed: claude exited 1: API Error: overloaded`, set: func(t *testing.T, fake, _ string) {
			support.Write(t, fake, map[string]string{"exit.m-ref": "1"})
		}},
		{name: "the validator's answer is no JSON", why: `the validator call failed: its answer is not one line of JSON with results`, set: func(t *testing.T, fake, _ string) {
			support.Write(t, fake, map[string]string{"answer.m-val": envelope(t, "I agree.")})
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := nativeRepo(t)
			fake := fakeClaude(t)
			env := withPath(nativeEnv(t, claudeProfile), fake)
			subject := startReview(t, root, env)["subject"].(string)
			claudeAnswers(t, fake, subject)
			if c.set != nil {
				c.set(t, fake, subject)
			}
			if c.path != nil {
				env = append(env, c.path(t, fake))
			}
			r := reviewRun(t, root, env, "", append([]string{"run"}, c.args...)...)
			equal(t, r.Code, 1, c.name+": "+r.Stderr)
			equal(t, r.Stdout, "", c.name+": nothing on stdout")
			match(t, r.Stderr, `^jhon review run: `+c.why+fallback, c.name)
			equal(t, strings.Count(r.Stderr, "\n"), 1, c.name+": one line")
			if exists(filepath.Join(root, "openspec/changes/c1/review-4r.md")) {
				t.Errorf("%s: a failed run records nothing", c.name)
			}
		})
	}
}

// JHON_REVIEW_NATIVE=off runs no claude and names the lens agents, as the
// shell's switch does.
func TestReviewRunClaudeOff(t *testing.T) {
	t.Parallel()
	root := nativeRepo(t)
	fake := fakeClaude(t)
	env := append(withPath(nativeEnv(t, claudeProfile), fake), "JHON_REVIEW_NATIVE=off")
	r := reviewRun(t, root, env, "", "run")
	equal(t, r.Code, 1)
	match(t, r.Stderr, `^jhon review run: the native review is off \(JHON_REVIEW_NATIVE=off\); nothing recorded\. Fallback: launch the review-risk and review-reliability agents`)
	if exists(filepath.Join(fake, "order")) {
		t.Error("no claude call when the native review is off")
	}
}

// A usage error exits 2.
func TestReviewRunUsage(t *testing.T) {
	t.Parallel()
	root := nativeRepo(t)
	for _, args := range [][]string{{"run", "--bogus"}, {"run", "--timeout", "0"}, {"run", "--timeout", "x"}, {"run", "--subject", "a"}} {
		r := reviewRun(t, root, nativeEnv(t, ""), "", args...)
		equal(t, r.Code, 2, args)
		match(t, r.Stderr, `jhon review run \[--base <ref>\] \[--change <name>\] \[--timeout <seconds>\]`, args)
	}
}
