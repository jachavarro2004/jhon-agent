package audit

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// What a round reads of a session (contracts/ldd.md, "Rounds"; S12): per
// change, its slices and the owner's messages per slice, the planning
// rewrites after its first verify by that name, and a test run of the
// uncommitted tree its next commit committed as a passing run of that commit.

// changeValue is one change of a report's outcomes.
func changeValue(t *testing.T, r *Result, change string) *js.Object {
	t.Helper()
	for _, c := range js.Prop(js.Prop(r.Report(), "outcomes"), "changes").([]any) {
		if js.Prop(c, "change") == any(change) {
			return c.(*js.Object)
		}
	}
	t.Fatalf("no change %s in the report", change)
	return nil
}

func TestReportStatesSlicesAndOwnerMessagesPerSlice(t *testing.T) {
	root := t.TempDir()
	tasks := filepath.Join(root, "openspec", "changes", chg, "tasks.md")
	must(t, os.MkdirAll(filepath.Dir(tasks), 0o755))
	must(t, os.WriteFile(tasks, []byte("## Slice 1: a\n\n- [x] 1.1 a\n\n## Slice 2: b\n\n- [ ] 2.1 b\n"), 0o644))
	at := func(s step) step { s.root = root; return s }
	r := auditOf(t, user("go"), at(delegate("d01", "worker", chg)), user("and the other one"), at(delegate("d02", "worker", chg)))
	c := changeValue(t, r, chg)
	eq(t, "slices", js.Prop(c, "slices"), any(2.0))
	eq(t, "owner messages per slice", js.Prop(c, "owner_messages_per_slice"), any(0.5))
	out := Human(r, "s.jsonl", false)
	if !strings.Contains(out, "\n  ldd  "+chg+"  slices 2  owner messages 1  per slice 0.5\n") {
		t.Errorf("the human output states no slices:\n%s", out)
	}

	none := auditOf(t, user("go"), delegate("d01", "worker", chg))
	c = changeValue(t, none, chg)
	eq(t, "no tasks.md: slices", js.Prop(c, "slices"), any(0.0))
	eq(t, "no tasks.md: per slice", js.Prop(c, "owner_messages_per_slice"), nil)
	if out := Human(none, "s.jsonl", false); !strings.Contains(out, "\n  ldd  "+chg+"  slices 0  owner messages 0  per slice -\n") {
		t.Errorf("the human output states no slices:\n%s", out)
	}
}

func TestPerSliceIsOwnerMessagesOverSlices(t *testing.T) {
	eq(t, "3 messages, 2 slices", ChangeLDD{Slices: 2, OwnerMessages: 3}.PerSlice(), 1.5)
	eq(t, "no slices", math.IsNaN(ChangeLDD{OwnerMessages: 3}.PerSlice()), true)
}

func TestReportNamesPostVerifyRewrites(t *testing.T) {
	dir := testRoot + "/openspec/changes/" + chg
	tool := func(name string, input *js.Object) step { return step{kind: "tool", tool: name, input: input} }
	verify := step{kind: "delegate", id: "o02", typ: "verify", change: chg, noBlock: true, prompt: "Verify openspec/changes/" + chg + "/ against its specs", result: "**Status**: success\nverdict: fail"}
	r := auditOf(t, user("go"), step{kind: "delegate", id: "o01", typ: "worker", phase: "apply", change: chg}, verify,
		tool("Edit", obj("file_path", dir+"/design.md", "old_string", "cache", "new_string", "no cache")))
	c := changeValue(t, r, chg)
	eq(t, "post_verify_rewrites", js.Prop(c, "post_verify_rewrites"), any(1.0))
	eq(t, "no replans key", slices.Contains(c.Keys(), "replans"), false)
}

// git runs git in dir and returns its trimmed output.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	c.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "GIT_") })
	out, err := c.Output()
	must(t, err)
	return strings.TrimSpace(string(out))
}

func TestARecordOfTheTreeTheNextCommitCommitsPasses(t *testing.T) {
	repo, err := filepath.EvalSymlinks(t.TempDir())
	must(t, err)
	gitT(t, repo, "init", "-q")
	must(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o644))
	gitT(t, repo, "add", "-A")
	gitT(t, repo, "commit", "-qm", "base")
	base, baseTree := gitOut(t, repo, "rev-parse", "HEAD"), gitOut(t, repo, "rev-parse", "HEAD^{tree}")
	// The slice is tested uncommitted, then committed as it was tested.
	must(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n\nvar X = 1\n"), 0o644))
	gitT(t, repo, "add", "-A")
	gitT(t, repo, "commit", "-qm", "slice 1")
	next, nextTree := gitOut(t, repo, "rev-parse", "HEAD"), gitOut(t, repo, "rev-parse", "HEAD^{tree}")
	dir := filepath.Join(repo, "openspec", "changes", chg)
	must(t, os.MkdirAll(dir, 0o755))
	record := func(workingTree string) {
		must(t, os.WriteFile(filepath.Join(dir, "execution-record.json"), []byte(`{"schema":"jhon.execution-record/v2","timestamp":"2026-09-30T09:00:00.000Z","complete":true,"candidate_commit":"`+base+
			`","candidate_tree":"`+baseTree+`","working_tree_clean":false,"working_tree":"`+workingTree+`","dirty":false,"scope":"full","executions":[{"project":".","command":"go test ./...","cwd":".","exit_code":0,"status":"pass","duration_ms":1000}]}`), 0o644))
	}
	d := delegate("d01", "general", chg, pushOf(next))
	d.root = repo

	record(nextTree)
	r := auditOf(t, user("go"), d)
	eq(t, "the delivery of the tested tree", of(r.Findings, "delivered-untested"), []Finding(nil))
	x := outcomeOf(r, chg)
	if x == nil || x.Record == nil || !x.Record.Passed || x.Record.Why != "" {
		t.Fatalf("the record = %+v", x)
	}

	// A tree no later commit committed stays a run of uncommitted changes.
	record(strings.Repeat("f", 40))
	r = auditOf(t, user("go"), d)
	if f := of(r.Findings, "delivered-untested"); len(f) != 1 || !strings.Contains(f[0].Message, "not a commit's") {
		t.Errorf("an uncommitted tree = %+v", f)
	}
	if x := outcomeOf(r, chg); x.Record == nil || x.Record.Passed || !x.Record.TestsPassed {
		t.Errorf("the record = %+v", x.Record)
	}
}
