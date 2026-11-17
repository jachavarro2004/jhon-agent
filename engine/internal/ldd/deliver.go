package ldd

import (
	"encoding/json"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/judgment"
	"github.com/jachavarro2004/jhon-agent/engine/internal/vcs"
)

// This file is `jhon deliver check` (contracts/ldd.md,
// "Delivery"): an advisory reading an agent or the owner runs before
// delivering. Is the revision about to be delivered the revision that was
// tested? It is ready when the working tree is clean and HEAD's tree is the
// tree of the change's last passing test run (`jhon executions run`): a
// complete run, with at least one execution and every one passing, that was
// not disturbed while it ran. A deploy or a release also names the immutable
// artifact it ships, by digest; a mutable tag is refused, since it can name
// another build tomorrow. An infrastructure deploy (CDK, Terraform) has no
// image to digest: it names the full commit it deploys from, which must be
// the tested HEAD. A change with delta specs is ready only when a test
// names each of their scenarios (ScenarioCoverage, as `jhon status` counts).
//
// Identity is the tree, never the commit: a squash or a rebase that yields
// the same tree is the same revision. The change's own record files
// (verify-report.md, the execution record, delivery.json) are written after
// the run and never count as a difference, committed or not; a committed
// Judgment Day record under its judgments/ does not either.
//
// A high-risk change (risk.go) also carries an independent verification
// (verification.go): a verify-report.md stating `verdict: pass`, or a
// Judgment Day record closed APPROVED. A 4R review leaves no record, so it
// cannot stand in for one.

// actions are the deliver_action values, merge first, on first use.
var actions = sync.OnceValue(func() []string { return contract.MustVocabulary("deliver_action", 3) })

// Actions are the delivery actions: merge, deploy, release.
func Actions() []string { return actions() }

// Merge is the default action, the one that ships no artifact.
func Merge() string { return actions()[0] }

// DeliveryFile is the record a ready check writes in the change directory.
const DeliveryFile = "delivery.json"

// Digest is an immutable artifact reference: an optional name, then a content digest.
var Digest = lazyre.New(`^(?:[^\s@]+@)?(?:sha256:[0-9a-f]{64}|sha384:[0-9a-f]{96}|sha512:[0-9a-f]{128})$`)

// commitArtifact is an infrastructure deploy's artifact: an optional name,
// then a full commit sha (SHA-1 or SHA-256), never a tag or a short sha.
var commitArtifact = lazyre.New(`^(?:[^\s@]+@)?(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// IsCommitArtifact is whether s names a full commit, as an infrastructure deploy's artifact.
func IsCommitArtifact(s string) bool { return commitArtifact.MatchString(s) }

// artifactCommit is the commit sha a commit artifact names.
func artifactCommit(s string) string { return s[strings.LastIndex(s, "@")+1:] }

// gitOK is whether git, run in root with args, exits 0.
func gitOK(root string, args ...string) (string, bool) {
	c := exec.Command("git", append([]string{"-C", root}, args...)...)
	c.Env = project.GitReadEnv()
	out, err := c.Output()
	return strings.TrimSpace(string(out)), err == nil
}

// DeliverRisk is the part of a change's Risk a check reports.
type DeliverRisk struct {
	Tier Level  `json:"tier"`
	Base string `json:"base"`
}

// DeliverCheck is what a check saw, and why it is not ready. The pointers are
// JSON null when absent: no artifact, no tested run, no independent
// verification (or a tier below high).
type DeliverCheck struct {
	Ready        bool        `json:"ready"`
	Action       string      `json:"action"`
	Artifact     *string     `json:"artifact"`
	HeadCommit   string      `json:"head_commit"`
	HeadTree     string      `json:"head_tree"`
	TestedTree   *string     `json:"tested_tree"`
	TestedCommit *string     `json:"tested_commit"`
	Risk         DeliverRisk `json:"risk"`
	VerifiedBy   *string     `json:"verified_by"`
	Reasons      []string    `json:"reasons"`
	Notes        []string    `json:"notes"`
}

// listed is the first five paths, and how many more there are.
func listed(paths []string) string {
	head := paths
	if len(head) > 5 {
		head = head[:5]
	}
	s := strings.Join(head, ", ")
	if len(paths) > 5 {
		s += fmt.Sprintf(", and %d more", len(paths)-5)
	}
	return s
}

var sha = lazyre.New(`^[0-9a-f]{40,64}$`)

func isSha(s string) bool { return sha.MatchString(s) }

// orNull is s, or nil for "": an empty value prints as null.
func orNull(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// deliverOrder names the steps a delivery takes, for the reasons a step was skipped.
const deliverOrder = "; order: commit, then `jhon executions run` on the clean tree, then `jhon deliver check`"

// CheckDelivery is whether the revision at HEAD in root is the change's last
// tested revision, and why not. artifact is nil when none is named. The
// error is a record, a risk config, or a judgments/ that exists and cannot be
// read.
func CheckDelivery(root, change, action string, artifact *string) (DeliverCheck, error) {
	exempt := execution.ChangeRecordPaths(root, change)
	now := vcs.CandidateOf(root, exempt)
	run, err := execution.ReadLastRun(root, change)
	if err != nil {
		return DeliverCheck{}, err
	}
	reasons, notes := []string{}, []string{}
	if now.Tree == vcs.CandidateUnknown() {
		reasons = append(reasons, "HEAD's tree cannot be read: delivery needs a git repository with a commit")
	} else if now.Clean != true {
		reasons = append(reasons, "the working tree is not clean (commit or remove the uncommitted changes)"+deliverOrder)
	}
	if run == nil {
		reasons = append(reasons, fmt.Sprintf("no recorded test run: run jhon executions run %s (%s)", change, execution.RecordShown(root, change)))
	} else {
		if !run.Complete {
			reasons = append(reasons, "the last test run did not finish: its record is partial")
		} else if !run.Passed {
			// An affected run of nothing is what wrote an empty record:
			// only the full run clears it.
			why := fmt.Sprintf(": it ran no project; run `jhon executions run %s` (the full run), then `jhon deliver check`", change)
			if len(run.Failed) > 0 {
				why = ": " + strings.Join(run.Failed, ", ")
			}
			reasons = append(reasons, "the last test run did not pass"+why)
		}
		if run.Dirty {
			reasons = append(reasons, "the candidate changed during the last test run: what was tested is not one revision; run the tests again")
		}
		// outsideRecord is the paths that differ between two trees, but for
		// the change's records (Judgment Day's under it included), written
		// after the run; ok is false when a tree does not resolve.
		outsideRecord := func(a, b string) ([]string, bool) {
			diff, ok := vcs.TreeDiff(root, a, b)
			judged := layout.For(root).Artifact(change, "judgments") + "/"
			outside := []string{}
			for _, p := range diff {
				if !exempt[p] && !strings.HasPrefix(p, judged) && !strings.Contains(p, "/"+judged) {
					outside = append(outside, p)
				}
			}
			return outside, ok
		}
		// A run on uncommitted work tested the working tree it recorded: once
		// that tree is committed, HEAD is the tested revision, and the run
		// that recorded GREEN is the delivery evidence, with no re-run (T146).
		// Only a run that passed, finished, and saw its working tree unchanged
		// to its end (not Dirty) is that evidence (T146d).
		committed := false
		if run.Passed && run.Complete && !run.Clean && !run.Dirty && now.Clean == true && isSha(run.WorkingTree) && isSha(now.Tree) {
			if outside, ok := outsideRecord(run.WorkingTree, now.Tree); ok && len(outside) == 0 {
				committed = true
				run.Tree = run.WorkingTree
				notes = append(notes, "the last test run tested this tree before its commit: its run is the delivery evidence, with no re-run")
			}
		}
		if !run.Clean && !committed {
			reasons = append(reasons, "the last test run started on a working tree that was not clean"+deliverOrder)
		}
		if committed {
		} else if isSha(run.Tree) && isSha(now.Tree) && run.Tree != now.Tree {
			outside, ok := outsideRecord(run.Tree, now.Tree)
			if !ok {
				reasons = append(reasons, fmt.Sprintf("the tested tree %s does not resolve in this repository", run.Tree))
			} else if len(outside) > 0 {
				reasons = append(reasons, fmt.Sprintf("HEAD's tree is not the tested tree: %d path(s) differ (%s); test this revision", len(outside), listed(outside))+deliverOrder)
			}
		} else if !isSha(run.Tree) {
			reasons = append(reasons, "the last test run recorded no tree: it ran outside a git repository")
		}
		// A unit's run tested what changed since the last passing run, not the
		// change. One of nothing has the full run named above, not this.
		if run.Unit && !run.Empty() {
			reasons = append(reasons, fmt.Sprintf("the last test run was a unit's (`--affected` with no base), which runs only what changed since the change's last passing run: run `jhon executions run %s --affected <base>` for the whole change, then `jhon deliver check`", change))
		}
		if run.Scope != execution.Full {
			notes = append(notes, fmt.Sprintf("the last test run had scope %s: only the affected projects ran", run.Scope))
		}
	}
	// A deploy naming a commit deploys from that commit: it is the tested HEAD.
	if action != Merge() && artifact != nil && IsCommitArtifact(*artifact) {
		c := artifactCommit(*artifact)
		if _, ok := gitOK(root, "cat-file", "-e", c+"^{commit}"); !ok {
			reasons = append(reasons, fmt.Sprintf("the artifact commit %s does not exist in this repository", c))
		} else if c != now.Commit {
			reasons = append(reasons, fmt.Sprintf("the artifact commit %s is not HEAD (%s): deploy from the tested HEAD and name it", c, now.Commit))
		} else if out, ok := gitOK(root, "branch", "-r", "--contains", c); ok && out == "" {
			notes = append(notes, fmt.Sprintf("the artifact commit %s is on no remote-tracking branch: push it so the deploy can be traced", c))
		}
	}
	risk, err := RiskOf(root, nil)
	if err != nil {
		return DeliverCheck{}, err
	}
	// The tier only rises across the change's slices: a slice whose own diff
	// reads lower keeps the highest tier an earlier delivery recorded.
	earlier := deliveredTier(root, change)
	if rank(earlier) > rank(risk.Tier) {
		notes = append(notes, fmt.Sprintf("jhon risk reads %s on this slice's diff, but an earlier delivery of this change was %s: the tier only rises, so this delivery keeps %s's process", risk.Tier, earlier, earlier))
		risk.Tier = earlier
		risk.Reasons = append(risk.Reasons, Reason{earlier, "", "an earlier delivery of this change was " + string(earlier)})
	}
	var verified *string
	if risk.Tier == High {
		v, err := IndependentVerification(root, change, judgment.VerdictOf)
		if err != nil {
			return DeliverCheck{}, err
		}
		if v != nil {
			verified = orNull(v.By)
		}
		var high []string
		for _, r := range risk.Reasons {
			if r.Level != High {
				continue
			}
			if r.File != "" {
				high = append(high, r.File+": "+r.Why)
			} else {
				high = append(high, r.Why)
			}
		}
		folder := filepath.Join(execution.ChangesOf(root), change)
		if st, err := os.Stat(filepath.Join(root, folder)); verified == nil && (err != nil || !st.IsDir()) {
			// A change with no folder: risk high needs tasks.md, and so one.
			reasons = append(reasons, fmt.Sprintf("the change is high risk (%s) and has no folder: risk high needs its tasks.md, so write it under %s/ and record there a verify-report.md stating verdict: pass (the verify agent), or a Judgment Day under %s/ closed APPROVED",
				listed(high), folder, filepath.Join(folder, "judgments")))
		} else if verified != nil {
			notes = append(notes, "high risk, independently verified by "+*verified)
		} else {
			reasons = append(reasons, fmt.Sprintf("the change is high risk (%s) and has no independent verification: a verify-report.md stating verdict: pass (the verify agent), or a Judgment Day record under %s/ closed APPROVED",
				listed(high), filepath.Join(execution.ChangesOf(root), change, "judgments")))
		}
	}
	// Each scenario of the change's delta specs is named by a test, as status counts them.
	lay := layout.For(root)
	specFileList, err := specFiles(lay, filepath.Join(root, execution.ChangesOf(root), change))
	if err != nil {
		return DeliverCheck{}, err
	}
	if len(specFileList) > 0 {
		specs := make([]string, len(specFileList))
		for i, f := range specFileList {
			specs[i] = f[1]
		}
		coverage, err := ScenarioCoverage(lay, specs, testFiles(root))
		if err != nil {
			return DeliverCheck{}, err
		}
		if coverage.Covered < coverage.Total {
			quoted := make([]string, len(coverage.Uncovered))
			for i, n := range coverage.Uncovered {
				quoted[i] = strconv.Quote(n)
			}
			reasons = append(reasons, fmt.Sprintf("scenarios named by a test: %d/%d; not yet: %s: a test names the scenario it proves (its test name, or a `scenario: <name>` comment)", coverage.Covered, coverage.Total, strings.Join(quoted, ", ")))
		}
	}
	// delivery: change delivers the change in one merge, after its last slice.
	if action == Merge() && DeliveryUnit(root) == "change" {
		var src *string
		if b, err := os.ReadFile(filepath.Join(root, execution.ChangesOf(root), change, lay.Tasks())); err == nil {
			text := string(b)
			src = &text
		}
		t := ProgressOf(lay, src)
		outlined := t.Outlined()
		if t.Pending > 0 {
			reasons = append(reasons, fmt.Sprintf("delivery: change in openspec/config.yaml merges the change once, after its last slice; %d task(s) of tasks.md are unchecked (%s): commit each slice on the change's branch and merge when every slice is done", t.Pending, listed(t.Unchecked)))
		}
		if len(outlined) > 0 {
			reasons = append(reasons, fmt.Sprintf("delivery: change in openspec/config.yaml merges the change once, after its last slice; %d slice(s) of tasks.md are outlined with no tasks yet (%s): commit each slice on the change's branch and merge when every slice is done", len(outlined), listed(outlined)))
		}
		if t.Pending == 0 && len(outlined) == 0 {
			notes = append(notes, "delivery: change: merge with rebase (gh pr merge <n> --rebase --delete-branch), so each slice stays its own commit on the default branch")
		}
	}
	var testedTree, testedCommit *string
	if run != nil {
		testedTree, testedCommit = orNull(run.Tree), orNull(run.Commit)
	}
	return DeliverCheck{
		Ready: len(reasons) == 0, Action: action, Artifact: artifact,
		HeadCommit: now.Commit, HeadTree: now.Tree,
		TestedTree: testedTree, TestedCommit: testedCommit,
		Risk: DeliverRisk{risk.Tier, risk.Base}, VerifiedBy: verified,
		Reasons: reasons, Notes: notes,
	}, nil
}

// Delivery is the record a ready check leaves in the change's delivery.json.
type Delivery struct {
	Timestamp    string  `json:"timestamp"`
	Action       string  `json:"action"`
	Artifact     *string `json:"artifact"`
	HeadCommit   string  `json:"head_commit"`
	TestedTree   *string `json:"tested_tree"`
	TestedCommit *string `json:"tested_commit"`
	Risk         Level   `json:"risk"`
	VerifiedBy   *string `json:"verified_by"`
}

// DeliveryOf is the record of a ready check c, made at timestamp.
func DeliveryOf(c DeliverCheck, timestamp string) Delivery {
	return Delivery{timestamp, c.Action, c.Artifact, c.HeadCommit, c.TestedTree, c.TestedCommit, c.Risk.Tier, c.VerifiedBy}
}

// WriteDelivery writes d to the change's delivery.json, replacing the
// previous one atomically: a reader sees the previous record or this one.
func WriteDelivery(root, change string, d Delivery) error {
	s, err := js.Stringify(d)
	if err != nil {
		return err
	}
	abs := execution.ChangeFile(root, change, DeliveryFile)
	if err := os.MkdirAll(filepath.Dir(abs), 0o777); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp-%d", abs, os.Getpid())
	if err := os.WriteFile(tmp, []byte(s+"\n"), 0o666); err != nil {
		return err
	}
	return os.Rename(tmp, abs)
}

// DeliveryUnit is what one delivery of a change carries, from `delivery:` in
// the project's openspec/config.yaml: "change" for the whole change in one
// merge, after its last slice; "slice" (the default) for each slice merged
// on its own.
func DeliveryUnit(root string) string {
	b, err := project.ReadLddConfig(root)
	if err == nil && contract.Scalar(b, "delivery") == "change" {
		return "change"
	}
	return "slice"
}

// rank is a tier's place in Levels, -1 for anything else.
func rank(l Level) int { return slices.Index(Levels, l) }

// deliveredTier is the tier the change's last ready delivery recorded, ""
// when it has none: delivery.json keeps the highest tier delivered so far.
func deliveredTier(root, change string) Level {
	b, err := os.ReadFile(execution.ChangeFile(root, change, DeliveryFile))
	if err != nil {
		return ""
	}
	var d struct {
		Risk Level `json:"risk"`
	}
	if json.Unmarshal(b, &d) != nil || rank(d.Risk) < 0 {
		return ""
	}
	return d.Risk
}
