package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeGuard is the fake reference build: it refuses a command holding
// refuse-me or data/*.json, asks for ask-me or $OLDPWD, breaks on boom and
// passes the rest, keeping each argv, payload and environment under its HOME.
func fakeGuard(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", "certify", "replay", "fake-guard"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "certify", "replay", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// ccRefusal is a Claude Code stream whose Bash call of command a guard
// refused, at the top level or (agent set) in a sub-agent of that type.
func ccRefusal(command, agent string) []byte {
	enc := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	var lines []string
	parent := any(nil)
	if agent != "" {
		lines = append(lines, enc(map[string]any{"type": "assistant", "parent_tool_use_id": nil, "message": map[string]any{"role": "assistant",
			"content": []any{map[string]any{"type": "tool_use", "id": "toolu_agent", "name": "Agent", "input": map[string]any{"subagent_type": agent, "prompt": "x"}}}}}))
		parent = "toolu_agent"
	}
	lines = append(lines,
		enc(map[string]any{"type": "assistant", "parent_tool_use_id": parent, "message": map[string]any{"role": "assistant",
			"content": []any{map[string]any{"type": "tool_use", "id": "toolu_bash", "name": "Bash", "input": map[string]any{"command": command}}}}}),
		enc(map[string]any{"type": "user", "parent_tool_use_id": parent, "message": map[string]any{"role": "user",
			"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu_bash", "is_error": true,
				"content": "PreToolUse:Bash hook error: [/bench/jhon hook pre-tool]: safety-guard: refused: x"}}}}))
	return []byte(strings.Join(lines, "\n") + "\n")
}

// putStream gives a fixture run its stream.jsonl and, when repo, its repo/.
func putStream(t *testing.T, dir, run string, stream []byte, repo bool) string {
	t.Helper()
	d := filepath.Join(dir, "runs", run)
	if err := os.WriteFile(filepath.Join(d, "stream.jsonl"), stream, 0o644); err != nil {
		t.Fatal(err)
	}
	if repo {
		if err := os.MkdirAll(filepath.Join(d, "repo"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

// refusedOnce is a fixture edit: the first plain run of cond has one refusal
// (jhon) or one declined dialog (pi-jhon).
func refusedOnce(cond string) func(m *Metrics) {
	return func(m *Metrics) {
		if m.Variant != "plain" || m.Condition != cond || m.Repeat != 1 {
			return
		}
		if cond == "pi-jhon" {
			m.UIRequestsDeclined = 1
		} else {
			m.Refusals = ip(1)
		}
	}
}

// readCert is out's certify.json.
func readCert(t *testing.T, out string) Certification {
	t.Helper()
	var c Certification
	b, err := os.ReadFile(filepath.Join(out, "certify.json"))
	if err != nil || json.Unmarshal(b, &c) != nil {
		t.Fatalf("certify.json: %v %s", err, b)
	}
	return c
}

// payloads are the payloads the fake reference received.
func payloads(t *testing.T, out string) []map[string]any {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(out, "replay", "home", "payloads.jsonl"))
	return records(b)
}

// The real Claude Code refusal (candidate-15806fe7 contacts-migration plain
// jhon 1, `cat data/*.json` in a read-only list) and the real pi guard
// dialog (candidate-aac6c9aa invoice-slices ldd-native pi-jhon 1, $OLDPWD)
// read as one event each, with the command the agent ran.
func TestGuardEventsReadRealStreams(t *testing.T) {
	cc := guardEvents(readTestdata(t, "claude-refusal.jsonl"))
	want := "git status --short && git log --oneline | head -5 && ls -R | grep -v '^\\.git' | head -50 && cat *.py; cat data/*.json; ls tests 2>/dev/null"
	if len(cc) != 1 || cc[0].Kind != "refusal" || cc[0].Command != want || cc[0].Agent != "" {
		t.Fatalf("claude events %+v", cc)
	}
	pi := guardEvents(readTestdata(t, "pi-dialog.jsonl"))
	if len(pi) != 1 || pi[0].Kind != "dialog" || !strings.HasPrefix(pi[0].Command, "jhon executions run invoice-csv-export") ||
		!strings.Contains(pi[0].Command, "R=$OLDPWD; python3 $R/cli.py export") || pi[0].Agent != "" {
		t.Fatalf("pi events %+v", pi)
	}
	if w := guardEvents(ccRefusal("ls", "worker")); len(w) != 1 || w[0].Agent != "worker" {
		t.Fatalf("worker events %+v", w)
	}
}

// A refusal the reference build makes too is today's behaviour: excluded,
// and listed as baseline-shared with its cell, run, command and verdict.
func TestCertifyReplayExcludesARefusalTheReferenceShares(t *testing.T) {
	base, cand := fixture(t, 1, refusedOnce("jhon"))
	run := putStream(t, cand, "invoice-slices-plain-jhon-1", readTestdata(t, "claude-refusal.jsonl"), true)
	if code, out, _ := certify(t, nil, "--baseline", base, "--candidate", cand, "--out", t.TempDir()); code != 1 || !strings.HasPrefix(line(out, "refusals and declined dialogs"), "FAIL") {
		t.Fatalf("without --replay-guard: exit %d\n%s", code, out)
	}
	t.Setenv("CERTIFY_REPLAY_LEAK", "outside")
	dir := t.TempDir()
	code, out, errOut := certify(t, nil, "--baseline", base, "--candidate", cand, "--out", dir, "--replay-guard", fakeGuard(t))
	if code != 0 || !strings.HasPrefix(line(out, "refusals and declined dialogs"), "PASS") {
		t.Fatalf("exit %d\n%s%s", code, out, errOut)
	}
	for _, s := range []string{"replay: reference guard " + fakeGuard(t), "baseline-shared (excluded): 1",
		"candidate invoice-slices plain jhon invoice-slices-plain-jhon-1: refusal, reference refused: git status --short", "cat data/*.json"} {
		if !strings.Contains(out, s) {
			t.Errorf("output lacks %q:\n%s", s, out)
		}
	}
	c := readCert(t, dir)
	if c.Replay == nil || len(c.Replay.BaselineShared) != 1 || len(c.Replay.Counted) != 0 {
		t.Fatalf("replay %+v", c.Replay)
	}
	if s := c.Replay.BaselineShared[0]; s.Side != "candidate" || s.Cell != "invoice-slices plain jhon" || s.Run != "invoice-slices-plain-jhon-1" ||
		s.Kind != "refusal" || s.Reference != "refused" || !strings.Contains(s.Command, "cat data/*.json") {
		t.Errorf("shared %+v", s)
	}
	ps := payloads(t, dir)
	if len(ps) != 1 || str(ps[0]["cwd"]) != filepath.Join(run, "repo") || str(ps[0]["permission_mode"]) != "default" ||
		str(ps[0]["tool_name"]) != "Bash" || !strings.Contains(str(at(ps[0], "tool_input", "command")), "cat data/*.json") || ps[0]["agent_id"] != nil {
		t.Errorf("payloads %v", ps)
	}
	argv, _ := os.ReadFile(filepath.Join(dir, "replay", "home", "argv.txt"))
	if strings.TrimSpace(string(argv)) != "hook pre-tool --agent * safety-guard vcs-guard .git secrets-guard route-guard" {
		t.Errorf("argv %q", argv)
	}
	env, _ := os.ReadFile(filepath.Join(dir, "replay", "home", "env.txt"))
	if strings.Contains(string(env), "CERTIFY_REPLAY_LEAK") || !strings.Contains(string(env), "HOME="+filepath.Join(dir, "replay", "home")) ||
		!strings.Contains(string(env), "TMPDIR="+filepath.Join(dir, "replay", "tmp")) {
		t.Errorf("the replay's env is not isolated:\n%s", env)
	}
}

// pi's declined guard dialog is replayed the same way: the reference asks.
func TestCertifyReplayExcludesADialogTheReferenceShares(t *testing.T) {
	base, cand := fixture(t, 1, refusedOnce("pi-jhon"))
	run := putStream(t, cand, "invoice-slices-plain-pi-jhon-1", readTestdata(t, "pi-dialog.jsonl"), true)
	dir := t.TempDir()
	code, out, errOut := certify(t, nil, "--baseline", base, "--candidate", cand, "--out", dir, "--replay-guard", fakeGuard(t))
	if code != 0 || !strings.Contains(out, "candidate invoice-slices plain pi-jhon invoice-slices-plain-pi-jhon-1: dialog, reference asked: jhon executions run") {
		t.Fatalf("exit %d\n%s%s", code, out, errOut)
	}
	ps := payloads(t, dir)
	if len(ps) != 1 || str(ps[0]["cwd"]) != filepath.Join(run, "repo") || !strings.Contains(str(at(ps[0], "tool_input", "command")), "$OLDPWD") || ps[0]["agent_id"] != nil {
		t.Errorf("payloads %v", ps)
	}
}

// A refusal the reference does not make, or one the replay cannot judge,
// counts exactly as today: unknown is never excluded.
func TestCertifyReplayCountsWhatTheReferenceDoesNotShare(t *testing.T) {
	for _, c := range []struct {
		name, verdict string
		stream        []byte
		repo          bool
		refusals      int
	}{
		{"reference passes", "reference passed", ccRefusal("echo pass-me", ""), true, 1},
		{"reference breaks", "replay error: exit 1: fake: broken", ccRefusal("echo boom", ""), true, 1},
		{"no repo", "replay error: no repo", ccRefusal("echo refuse-me", ""), false, 1},
		{"unreadable stream", "replay error: ", []byte("not json\n"), true, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			base, cand := fixture(t, 1, func(m *Metrics) {
				if m.Variant == "plain" && m.Condition == "jhon" && m.Repeat == 1 {
					m.Refusals = ip(c.refusals)
				}
			})
			putStream(t, cand, "invoice-slices-plain-jhon-1", c.stream, c.repo)
			dir := t.TempDir()
			code, out, _ := certify(t, nil, "--baseline", base, "--candidate", cand, "--out", dir, "--replay-guard", fakeGuard(t))
			if code != 1 || !strings.HasPrefix(line(out, "refusals and declined dialogs"), "FAIL") || !strings.Contains(out, "baseline-shared (excluded): 0") {
				t.Fatalf("exit %d\n%s", code, out)
			}
			r := readCert(t, dir).Replay
			if r == nil || len(r.BaselineShared) != 0 || len(r.Counted) != c.refusals || !strings.HasPrefix(r.Counted[0].Reference, strings.TrimPrefix(c.verdict, "reference ")) {
				t.Fatalf("replay %+v", r)
			}
			if !strings.Contains(out, c.verdict) {
				t.Errorf("output lacks %q:\n%s", c.verdict, out)
			}
		})
	}
}

// A sub-agent's refusal goes as a sub-agent's payload; metrics.json does
// not count it, so nothing is excluded for it.
func TestCertifyReplaySendsASubAgentsShape(t *testing.T) {
	base, cand := fixture(t, 1, nil)
	putStream(t, cand, "invoice-slices-plain-jhon-1", ccRefusal("echo refuse-me", "worker"), true)
	dir := t.TempDir()
	code, out, _ := certify(t, nil, "--baseline", base, "--candidate", cand, "--out", dir, "--replay-guard", fakeGuard(t))
	if code != 0 || !strings.Contains(out, "refusal (worker, not counted in metrics.json), reference refused") {
		t.Fatalf("exit %d\n%s", code, out)
	}
	ps := payloads(t, dir)
	if len(ps) != 1 || str(ps[0]["agent_type"]) != "worker" || str(ps[0]["agent_id"]) == "" || str(ps[0]["permission_mode"]) != "default" {
		t.Errorf("payloads %v", ps)
	}
}

// The baseline's refusals go through the same reference: the comparison
// is symmetric, and the output says whether every one is shared.
func TestCertifyReplayIsSymmetric(t *testing.T) {
	base, cand := fixture(t, 1, refusedOnce("jhon"))
	putStream(t, cand, "invoice-slices-plain-jhon-1", ccRefusal("echo refuse-me", ""), true)
	putRuns(t, base, func() Metrics { m := cellRun("plain", "jhon", 1, 1); m.Refusals = ip(1); return m }())
	putStream(t, base, "invoice-slices-plain-jhon-1", ccRefusal("echo ask-me", ""), true)
	dir := t.TempDir()
	code, out, _ := certify(t, nil, "--baseline", base, "--candidate", cand, "--out", dir, "--replay-guard", fakeGuard(t))
	if code != 0 || !strings.Contains(out, "baseline invoice-slices plain jhon invoice-slices-plain-jhon-1: refusal, reference asked: echo ask-me") ||
		!strings.Contains(out, "baseline: 1 of 1 refusals and dialogs shared") {
		t.Fatalf("exit %d\n%s", code, out)
	}
	putStream(t, base, "invoice-slices-plain-jhon-1", ccRefusal("echo pass-me", ""), true)
	_, out, _ = certify(t, nil, "--baseline", base, "--candidate", cand, "--out", t.TempDir(), "--replay-guard", fakeGuard(t))
	if !strings.Contains(out, "baseline: 0 of 1 refusals and dialogs shared") {
		t.Fatalf("%s", out)
	}
}

// Without --replay-guard, certify's output and certify.json are what they
// were before the replay existed (pinned on the real report).
func TestCertifyWithoutReplayIsUnchanged(t *testing.T) {
	real := filepath.Join("testdata", "certify", "real")
	dir := t.TempDir()
	_, out, _ := certify(t, nil, "--baseline", real, "--candidate", real, "--out", dir)
	want, _ := os.ReadFile(filepath.Join(real, "certify.golden.txt"))
	if got := strings.ReplaceAll(out, dir, "<out>"); got != string(want) {
		t.Errorf("output changed:\n%s", got)
	}
	gotJSON, _ := os.ReadFile(filepath.Join(dir, "certify.json"))
	wantJSON, _ := os.ReadFile(filepath.Join(real, "certify.golden.json"))
	if !slices.Equal(gotJSON, wantJSON) {
		t.Errorf("certify.json changed:\n%s", gotJSON)
	}
}
