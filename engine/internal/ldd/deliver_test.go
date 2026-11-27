package ldd

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

func TestListed(t *testing.T) {
	if got := listed([]string{"a", "b"}); got != "a, b" {
		t.Errorf("two: %q", got)
	}
	if got := listed([]string{"1", "2", "3", "4", "5", "6", "7"}); got != "1, 2, 3, 4, 5, and 2 more" {
		t.Errorf("seven: %q", got)
	}
}

func TestDigest(t *testing.T) {
	for s, want := range map[string]bool{
		"sha256:" + strings.Repeat("a", 64):                      true,
		"registry.example/app@sha256:" + strings.Repeat("0", 64): true,
		"app@sha384:" + strings.Repeat("f", 96):                  true,
		"sha512:" + strings.Repeat("1", 128):                     true,
		"registry.example/app:latest":                            false,
		"sha256:" + strings.Repeat("A", 64):                      false,
		"sha256:" + strings.Repeat("a", 63):                      false,
		"a b@sha256:" + strings.Repeat("a", 64):                  false,
		"a@b@sha256:" + strings.Repeat("a", 64):                  false,
		"":                                                       false,
	} {
		if Digest.MatchString(s) != want {
			t.Errorf("Digest(%q) != %v", s, want)
		}
	}
}

func TestCommitArtifact(t *testing.T) {
	for s, want := range map[string]bool{
		strings.Repeat("a", 40):             true,
		"infra@" + strings.Repeat("0", 40):  true,
		strings.Repeat("b", 64):             true,
		strings.Repeat("a", 7):              false,
		strings.Repeat("a", 41):             false,
		strings.Repeat("A", 40):             false,
		"v1.2.3":                            false,
		"main":                              false,
		"sha256:" + strings.Repeat("a", 64): false,
		"a b@" + strings.Repeat("a", 40):    false,
		"":                                  false,
	} {
		if IsCommitArtifact(s) != want {
			t.Errorf("IsCommitArtifact(%q) != %v", s, want)
		}
	}
}

// deliverGit is the trimmed output of a test git command in dir.
func deliverGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

const deliverChange = "v"

var deliverDir = filepath.Join("openspec", "changes", deliverChange)

// deliverRepo is a committed repository with a tested Go file and an active change.
func deliverRepo(t *testing.T) string {
	deliverEnv(t)
	root, _ := filepath.EvalSymlinks(t.TempDir())
	gitT(t, root, "init", "-q")
	write(t, filepath.Join(root, "a", "a.go"), "package a\n")
	write(t, filepath.Join(root, "a", "a_test.go"), "package a\n")
	write(t, filepath.Join(root, deliverDir, "tasks.md"), "- [x] done\n")
	deliverCommit(t, root)
	return root
}

func deliverCommit(t *testing.T, root string) {
	gitT(t, root, "add", "-A")
	gitT(t, root, "commit", "-q", "--allow-empty", "-m", "c")
}

// tested records a run of HEAD with extra fields (JSON members), and commits it.
func deliverTested(t *testing.T, root, status string, extra string) {
	rec := `{"complete": true, "scope": "full", "dirty": false, "working_tree_clean": true, ` +
		`"candidate_commit": "` + deliverGit(t, root, "rev-parse", "HEAD") + `", "candidate_tree": "` + deliverGit(t, root, "rev-parse", "HEAD^{tree}") + `", ` +
		extra + `"executions": [{"project": "a", "status": "` + status + `"}]}`
	write(t, filepath.Join(root, deliverDir, "execution-record.json"), rec)
	deliverCommit(t, root)
}

func deliverRun(t *testing.T, root string) DeliverCheck {
	t.Helper()
	c, err := CheckDelivery(root, deliverChange, "merge", nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func deliverReason(c DeliverCheck, pattern string) bool {
	re := regexp.MustCompile(pattern)
	for _, r := range c.Reasons {
		if re.MatchString(r) {
			return true
		}
	}
	return false
}

func TestCheckDeliveryReady(t *testing.T) {
	root := deliverRepo(t)
	tree := deliverGit(t, root, "rev-parse", "HEAD^{tree}")
	deliverTested(t, root, "pass", "")
	// A verify report and another file of the change's folder written after the run are no difference.
	write(t, filepath.Join(root, deliverDir, "verify-report.md"), "verdict: fail\n")
	write(t, filepath.Join(root, deliverDir, "notes", "x.md"), "draft\n")
	deliverCommit(t, root)
	c := deliverRun(t, root)
	if !c.Ready || len(c.Reasons) != 0 || c.TestedTree == nil || *c.TestedTree != tree || c.VerifiedBy != nil || c.Risk.Tier == High {
		t.Fatalf("ready: %+v", c)
	}
	s, _ := js.Stringify(c)
	if !strings.HasPrefix(s, "{\n  \"ready\": true,\n  \"action\": \"merge\",\n  \"artifact\": null,\n  \"head_commit\"") || !strings.Contains(s, "\"reasons\": [],\n  \"notes\": []\n}") {
		t.Errorf("JSON shape: %s", s)
	}
}

func TestCheckDeliveryNotReady(t *testing.T) {
	root := deliverRepo(t)
	if c := deliverRun(t, root); c.Ready || !deliverReason(c, `^no recorded test run: run jhon executions run v \(openspec/changes/v/execution-record\.json\)$`) || c.TestedTree != nil || c.TestedCommit != nil {
		t.Errorf("no run: %+v", c)
	}
	deliverTested(t, root, "fail", "")
	if c := deliverRun(t, root); !deliverReason(c, `^the last test run did not pass: a$`) {
		t.Errorf("failed run: %+v", c.Reasons)
	}
	deliverTested(t, root, "pass", `"complete": false, "dirty": true, "working_tree_clean": false, "scope": "affected", `)
	c := deliverRun(t, root)
	for _, want := range []string{"did not finish: its record is partial", "changed during the last test run", "started on a working tree that was not clean; order: a passing run (`jhon executions run`), commit the tree it tested, then `jhon deliver check`"} {
		if !deliverReason(c, regexp.QuoteMeta(want)) {
			t.Errorf("%q missing: %+v", want, c.Reasons)
		}
	}
	if len(c.Notes) != 1 || c.Notes[0] != "the last test run had scope affected: only the affected projects ran" {
		t.Errorf("scope note: %+v", c.Notes)
	}

	deliverTested(t, root, "pass", "")
	write(t, filepath.Join(root, "a", "a.go"), "package a\n\nvar X = 1\n")
	if c := deliverRun(t, root); !deliverReason(c, `^the working tree is not clean.*; order: a passing run \(`+"`jhon executions run`"+`\), commit the tree it tested, then `+"`jhon deliver check`"+`$`) {
		t.Errorf("uncommitted: %+v", c.Reasons)
	}
	deliverCommit(t, root)
	if c := deliverRun(t, root); !deliverReason(c, `^HEAD's tree is not the tested tree: 1 path\(s\) differ \(a/a\.go\); test this revision; order: a passing run \(`+"`jhon executions run`"+`\), commit the tree it tested, then `+"`jhon deliver check`"+`$`) {
		t.Errorf("later commit: %+v", c.Reasons)
	}

	deliverTested(t, root, "pass", `"candidate_tree": "`+strings.Repeat("0", 40)+`", `)
	if c := deliverRun(t, root); !deliverReason(c, `^the tested tree 0{40} does not resolve in this repository$`) {
		t.Errorf("unknown tree: %+v", c.Reasons)
	}
	write(t, filepath.Join(root, deliverDir, "execution-record.json"), `{"candidate_tree": "unknown", "executions": []}`)
	deliverCommit(t, root)
	c = deliverRun(t, root)
	if !deliverReason(c, `^the last test run did not pass: it ran no project; run `+"`jhon executions run v`"+` \(the full run\), then `+"`jhon deliver check`"+`$`) || !deliverReason(c, `^the last test run recorded no tree: it ran outside a git repository$`) {
		t.Errorf("no tree, no project: %+v", c.Reasons)
	}
}

func TestCheckDeliveryOutsideARepository(t *testing.T) {
	deliverEnv(t)
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, deliverDir), 0o755)
	c := deliverRun(t, root)
	if c.HeadTree != "unknown" || !deliverReason(c, `^HEAD's tree cannot be read`) || c.Risk.Tier != High ||
		!deliverReason(c, `^the change is high risk \(the diff cannot be read: not a git repository\) and has no independent verification`) {
		t.Errorf("outside a repository: %+v", c)
	}
}

func TestCheckDeliveryHighRisk(t *testing.T) {
	root := deliverRepo(t)
	gitT(t, root, "checkout", "-q", "-b", "feat")
	write(t, filepath.Join(root, "a", "a.go"), "package a\n\nvar Limit = 10\n")
	deliverCommit(t, root)
	deliverTested(t, root, "pass", "")
	c := deliverRun(t, root)
	if c.Ready || !deliverReason(c, `^the change is high risk \(a/a\.go: code changed and no test touched\) and has no independent verification: a verify-report\.md stating verdict: pass \(the jhon-checker agent\)$`) {
		t.Errorf("unverified: %+v", c.Reasons)
	}
	write(t, filepath.Join(root, deliverDir, "verify-report.md"), "**Verdict**: PASS\n")
	deliverCommit(t, root)
	c = deliverRun(t, root)
	if !c.Ready || c.VerifiedBy == nil || *c.VerifiedBy != "verify-report.md (verdict: pass)" || len(c.Notes) != 1 || c.Notes[0] != "high risk, independently verified by verify-report.md (verdict: pass)" {
		t.Errorf("verified: %+v", c)
	}
}

func TestWriteDelivery(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, deliverDir), 0o755)
	art, tree := "sha256:"+strings.Repeat("a", 64), strings.Repeat("b", 40)
	c := DeliverCheck{Ready: true, Action: "deploy", Artifact: &art, HeadCommit: "c", TestedTree: &tree, Risk: DeliverRisk{Medium, "main"}}
	if err := WriteDelivery(root, deliverChange, DeliveryOf(c, "2026-10-24T00:00:00.000Z")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(root, deliverDir, DeliveryFile))
	want := "{\n  \"timestamp\": \"2026-10-24T00:00:00.000Z\",\n  \"action\": \"deploy\",\n  \"artifact\": \"" + art + "\",\n  \"head_commit\": \"c\",\n" +
		"  \"tested_tree\": \"" + tree + "\",\n  \"tested_commit\": null,\n  \"risk\": \"medium\",\n  \"verified_by\": null\n}\n"
	if string(b) != want {
		t.Errorf("delivery.json:\n%s", b)
	}
	if entries, _ := os.ReadDir(filepath.Join(root, deliverDir)); len(entries) != 1 {
		t.Errorf("a temporary file is left: %v", entries)
	}
}

// deliverEnv points the vocabulary at this checkout.
func deliverEnv(t *testing.T) {
	root, _ := filepath.Abs(filepath.Join("..", "..", ".."))
	t.Setenv("JHON_ROOT", root)
}

// The tier only rises across a change's slices (contracts/ldd.md,
// "Slices"): a slice whose own diff reads lower keeps the process of
// the highest tier an earlier delivery of the change recorded.
func TestCheckDeliveryKeepsTheHighestTierAnEarlierSliceDelivered(t *testing.T) {
	root := deliverRepo(t)
	write(t, filepath.Join(root, deliverDir, DeliveryFile), `{"timestamp": "2026-10-25T00:00:00Z", "action": "merge", "risk": "high"}`+"\n")
	deliverTested(t, root, "pass", "")
	c := deliverRun(t, root)
	if c.Ready || c.Risk.Tier != High || !deliverReason(c, `high risk \(an earlier delivery of this change was high\) and has no independent verification`) {
		t.Fatalf("an earlier high slice keeps the change high: %+v", c)
	}
	if !slices.ContainsFunc(c.Notes, func(n string) bool { return strings.Contains(n, "the tier only rises") }) {
		t.Errorf("the note says why: %q", c.Notes)
	}
	write(t, filepath.Join(root, deliverDir, "verify-report.md"), "verdict: pass\n")
	deliverCommit(t, root)
	if c := deliverRun(t, root); !c.Ready || c.VerifiedBy == nil || c.Risk.Tier != High {
		t.Errorf("verified, it is ready, and still high: %+v", c)
	}
	if d := DeliveryOf(deliverRun(t, root), "now"); d.Risk != High {
		t.Errorf("the record keeps the highest tier for the next slice: %+v", d)
	}
}

// In a client project each slice's PR cost a CI run of about 30 minutes and an image
// build: with `delivery: change`, a change merges once, after its last
// slice, with rebase so each slice stays its own commit.
func TestCheckDeliveryOfAWholeChange(t *testing.T) {
	root := deliverRepo(t)
	write(t, filepath.Join(root, deliverDir, "tasks.md"), "## Slice 1: a\n- [x] 1.1 a\n## Slice 2: b\n- [ ] 2.1 Plan the slice\n")
	deliverCommit(t, root)
	deliverTested(t, root, "pass", "")
	if c := deliverRun(t, root); !c.Ready {
		t.Fatalf("by default each slice merges on its own: %+v", c)
	}
	write(t, filepath.Join(root, "openspec", "config.yaml"), "delivery: change\n")
	deliverCommit(t, root)
	deliverTested(t, root, "pass", "")
	c := deliverRun(t, root)
	if c.Ready || !deliverReason(c, `delivery: change .* 1 task\(s\) of tasks.md are unchecked \(2.1 Plan the slice\)`) {
		t.Fatalf("a slice left: not ready: %+v", c)
	}
	if c, _ := CheckDelivery(root, deliverChange, "deploy", nil); !c.Ready {
		t.Errorf("only a merge waits for the whole change: %+v", c)
	}
	write(t, filepath.Join(root, deliverDir, "tasks.md"), "## Slice 1: a\n- [x] 1.1 a\n## Slice 2: b\n- [x] 2.1 b\n")
	deliverCommit(t, root)
	deliverTested(t, root, "pass", "")
	c = deliverRun(t, root)
	if !c.Ready || !slices.ContainsFunc(c.Notes, func(n string) bool { return strings.Contains(n, "--rebase --delete-branch") }) {
		t.Fatalf("every slice done: ready, merged with rebase: %+v", c)
	}
}

// L842 A1: with `delivery: change`, an outlined slice with no tasks yet is
// a slice left: the change is not ready to merge.
func TestCheckDeliveryWaitsForOutlinedSlices(t *testing.T) {
	root := deliverRepo(t)
	write(t, filepath.Join(root, "openspec", "config.yaml"), "delivery: change\n")
	write(t, filepath.Join(root, deliverDir, "tasks.md"), "## Slice 1: a\n- [x] 1.1 a\n## Slice 2: b\nDelivers: y\n## Slice 3: c\nDelivers: z\n")
	deliverCommit(t, root)
	deliverTested(t, root, "pass", "")
	c := deliverRun(t, root)
	if c.Ready || !deliverReason(c, `^delivery: change in openspec/config\.yaml merges the change once, after its last slice; 2 slice\(s\) of tasks\.md are outlined with no tasks yet \(Slice 2: b, Slice 3: c\): commit each slice on the change's branch and merge when every slice is done$`) {
		t.Fatalf("outlined slices left: not ready: %+v", c)
	}
	if c, _ := CheckDelivery(root, deliverChange, "deploy", nil); !c.Ready {
		t.Errorf("only a merge waits for the whole change: %+v", c)
	}
}

func TestCheckDeliveryNamesAFailingCodeCheck(t *testing.T) {
	root := deliverRepo(t)
	rec := `{"complete": true, "scope": "full", "dirty": false, "working_tree_clean": true, ` +
		`"candidate_commit": "` + deliverGit(t, root, "rev-parse", "HEAD") + `", "candidate_tree": "` + deliverGit(t, root, "rev-parse", "HEAD^{tree}") + `", ` +
		`"executions": [{"project": "a", "status": "pass"}, {"project": "a", "kind": "lint", "status": "fail"}]}`
	write(t, filepath.Join(root, deliverDir, "execution-record.json"), rec)
	deliverCommit(t, root)
	if c := deliverRun(t, root); c.Ready || !deliverReason(c, `^the last test run did not pass: a \(lint\)$`) {
		t.Errorf("a failing lint is not delivered: %+v", c)
	}
}

// jhon-studio's working output (.jhon/studio/, T8g) is no difference from
// the tested tree: a video rendered and committed after the run leaves the
// delivery ready; a source file changed beside it is still the one named.
func TestCheckDeliveryLeavesStudioOutputOut(t *testing.T) {
	root := deliverRepo(t)
	tree := deliverGit(t, root, "rev-parse", "HEAD^{tree}")
	deliverTested(t, root, "pass", "")
	write(t, filepath.Join(root, ".jhon", "studio", "tour", "out", "tour.mp4"), "MP4\n")
	write(t, filepath.Join(root, ".jhon", "studio", "tour", "state.json"), "{}\n")
	deliverCommit(t, root)
	if c := deliverRun(t, root); !c.Ready || len(c.Reasons) != 0 || c.TestedTree == nil || *c.TestedTree != tree {
		t.Fatalf("studio output committed after the run: %+v", c)
	}
	write(t, filepath.Join(root, "a", "a.go"), "package a\n\nvar X = 1\n")
	write(t, filepath.Join(root, ".jhon", "studio", "tour", "state.json"), "{\"step\":\"render\"}\n")
	deliverCommit(t, root)
	if c := deliverRun(t, root); c.Ready || !deliverReason(c, `^HEAD's tree is not the tested tree: 1 path\(s\) differ \(a/a\.go\); test this revision`) {
		t.Errorf("a source change beside studio output: %+v", c.Reasons)
	}
}
