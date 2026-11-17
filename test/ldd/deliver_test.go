// `jhon deliver check` (contracts/ldd.md, "Delivery"): an advisory
// reading of whether the revision about to be delivered is the revision that
// was tested. It is ready when the working tree is clean and HEAD's tree is
// the tree of the change's last passing, complete, undisturbed test run (the
// change's own record files aside). A deploy or a release also names an
// immutable artifact by its digest, which a ready check records.
//
// Every fixture is a temporary git repository; the executor runs `true` or
// `false`.
package ldd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

const (
	deliverChange = "v"
	deliverDir    = "openspec/changes/" + deliverChange
)

var digest = "registry.example/app@sha256:" + strings.Repeat("a", 64)

// deliverProject is a committed root with project `a` whose resolved command
// is command, and an active change `v`.
func deliverProject(t *testing.T, command string) string {
	t.Helper()
	root := support.GitRepo(t, temp(t))
	support.Write(t, root, map[string]string{
		"a/go.mod":               "module example.com/a\n",
		"a/a.go":                 "package a\n",
		"openspec/config.yaml":   "test_command_override:\n  a: \"" + command + "\"\n",
		deliverDir + "/tasks.md": "- [x] done\n",
	})
	commit(t, root, "change")
	return root
}

func commit(t *testing.T, root, message string) {
	t.Helper()
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "--allow-empty", "-m", message)
}

// tested runs the tests through the recorder and commits the record, as a
// change does before delivery.
func tested(t *testing.T, root string) {
	t.Helper()
	jhonGit(t, root, "executions", "run", deliverChange, "--json")
	commit(t, root, "record")
}

type checked struct {
	support.Result
	json map[string]any
}

func check(t *testing.T, root string, args ...string) checked {
	t.Helper()
	r := jhonGit(t, root, append([]string{"deliver", "check", deliverChange, "--json"}, args...)...)
	c := checked{Result: r}
	if strings.TrimSpace(r.Stdout) != "" {
		c.json = object(t, r.Stdout)
	}
	return c
}

func reasons(c checked) string { b, _ := json.Marshal(c.json["reasons"]); return string(b) }

func TestDeliverReady(t *testing.T) {
	t.Parallel()
	root := deliverProject(t, "true")
	tested(t, root)
	r := check(t, root)
	equal(t, r.Code, 0, r.Out())
	equal(t, r.json["ready"], true)
	equal(t, r.json["action"], "merge")
	rec := object(t, support.Read(t, filepath.Join(root, deliverDir, "execution-record.json")))
	equal(t, r.json["tested_tree"], rec["candidate_tree"])
}

func TestDeliverProductionChangeAfterRun(t *testing.T) {
	t.Parallel()
	root := deliverProject(t, "true")
	tested(t, root)
	support.Write(t, root, map[string]string{"a/a.go": "package a\n\nvar X = 1\n"})
	commit(t, root, "later")
	r := check(t, root)
	equal(t, r.Code, 1)
	equal(t, r.json["ready"], false)
	equal(t, anyMatch(r.json["reasons"], `a/a\.go`), true, reasons(r))
}

func TestDeliverUncommittedFile(t *testing.T) {
	t.Parallel()
	root := deliverProject(t, "true")
	tested(t, root)
	support.Write(t, root, map[string]string{"a/new.go": "package a\n"})
	r := check(t, root)
	equal(t, r.Code, 1)
	equal(t, anyMatch(r.json["reasons"], `not clean|uncommitted`), true, reasons(r))
}

func TestDeliverNoFailingOrDirtyRun(t *testing.T) {
	t.Parallel()
	none := deliverProject(t, "true")
	equal(t, check(t, none).Code, 1)
	equal(t, anyMatch(check(t, none).json["reasons"], `no recorded test run`), true)

	failing := deliverProject(t, "false")
	tested(t, failing)
	f := check(t, failing)
	equal(t, f.Code, 1)
	equal(t, anyMatch(f.json["reasons"], `did not pass`), true, reasons(f))

	dirty := deliverProject(t, "touch made")
	tested(t, dirty)
	d := check(t, dirty)
	equal(t, d.Code, 1)
	equal(t, anyMatch(d.json["reasons"], `changed during the last test run`), true, reasons(d))
}

func TestDeliverArtifactDigest(t *testing.T) {
	t.Parallel()
	root := deliverProject(t, "true")
	tested(t, root)
	for _, action := range []string{"deploy", "release"} {
		equal(t, check(t, root, "--action", action).Code, 2, action+" without an artifact")
		equal(t, check(t, root, "--action", action, "--artifact", "registry.example/app:latest").Code, 2, action+" with a mutable tag")
	}
	equal(t, exists(filepath.Join(root, deliverDir, "delivery.json")), false, "a refusal writes nothing")
	r := check(t, root, "--action", "deploy", "--artifact", digest)
	equal(t, r.Code, 0, r.Out())
	rec := object(t, support.Read(t, filepath.Join(root, deliverDir, "delivery.json")))
	equal(t, rec["action"], "deploy")
	equal(t, rec["artifact"], digest)
	equal(t, rec["tested_tree"], r.json["tested_tree"])
	equal(t, check(t, root, "--action", "deploy", "--artifact", digest).Code, 0, "the delivery record never makes the tree unclean")
	m := check(t, root, "--action", "merge", "--artifact", digest)
	equal(t, m.Code, 0, "a merge may name a digest, as before: "+m.Out())
	equal(t, object(t, support.Read(t, filepath.Join(root, deliverDir, "delivery.json")))["artifact"], digest)
}

func TestDeliverRetiredSubcommands(t *testing.T) {
	t.Parallel()
	root := deliverProject(t, "true")
	for _, sub := range []string{"override", "archive-check"} {
		equal(t, jhonGit(t, root, "deliver", sub, deliverChange).Code, 2, sub)
	}
}

// orchestrator.md "Risk": a high-risk change is delivered only with an
// independent verification on record.
func TestDeliverHighRiskNeedsVerification(t *testing.T) {
	t.Parallel()
	root := deliverProject(t, "true")
	support.Git(t, root, "checkout", "-q", "-b", "feat")
	support.Write(t, root, map[string]string{"a/a.go": "package a\n\nvar Limit = 10\n"})
	commit(t, root, "code without a test")
	tested(t, root)
	r := check(t, root)
	equal(t, r.Code, 1, r.Out())
	equal(t, r.json["risk"].(map[string]any)["tier"], "high")
	equal(t, anyMatch(r.json["reasons"], `high risk \(a/a\.go: .*\) and has no independent verification`), true, reasons(r))

	support.Write(t, root, map[string]string{deliverDir + "/verify-report.md": "# Verify\n\nverdict: fail\n"})
	commit(t, root, "failed verify")
	equal(t, check(t, root).Code, 1, "a failing verdict verifies nothing")

	support.Write(t, root, map[string]string{deliverDir + "/verify-report.md": "# Verify\n\n**Verdict**: PASS WITH WARNINGS\n"})
	commit(t, root, "verify")
	v := check(t, root)
	equal(t, v.Code, 0, v.Out())
	equal(t, v.json["verified_by"], "verify-report.md (verdict: pass)")

	// A Judgment Day record closed APPROVED stands in for the report.
	if err := os.Remove(filepath.Join(root, deliverDir, "verify-report.md")); err != nil {
		t.Fatal(err)
	}
	fix := support.Fixture("judgment", "defect-close")
	if err := os.Mkdir(filepath.Join(root, deliverDir, "judgments"), 0o777); err != nil {
		t.Fatal(err)
	}
	rec := filepath.Join(root, deliverDir, "judgments", "2026-10-24-limit.md")
	entries, err := os.ReadDir(fix)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	for _, f := range names {
		a := jhonGit(t, root, "judgment", "append", rec, filepath.Join(fix, f))
		equal(t, a.Code, 0, a.Stderr)
	}
	commit(t, root, "judgment")
	j := check(t, root)
	equal(t, j.Code, 0, j.Out())
	equal(t, j.json["verified_by"], "judgments/2026-10-24-limit.md (APPROVED)")
}

func TestDeliverBelowHighRisk(t *testing.T) {
	t.Parallel()
	root := deliverProject(t, "true")
	tested(t, root)
	r := check(t, root)
	equal(t, r.Code, 0, r.Out())
	if r.json["risk"].(map[string]any)["tier"] == "high" {
		t.Error("tier is high")
	}
	equal(t, r.json["verified_by"], nil)
}

// A change whose delta specs name a scenario no test names is not ready, and
// says which; once a test names it, the same change is ready.
func TestDeliverUnnamedScenario(t *testing.T) {
	t.Parallel()
	root := deliverProject(t, "true")
	support.Write(t, root, map[string]string{
		deliverDir + "/specs/login/spec.md": "## ADDED Requirements\n### Requirement: Login\nThe system SHALL lock.\n#### Scenario: Locks after five tries\n- WHEN y\n- THEN locked\n",
	})
	commit(t, root, "spec")
	tested(t, root)
	r := check(t, root)
	equal(t, r.Code, 1, r.Out())
	equal(t, r.json["ready"], false)
	equal(t, anyMatch(r.json["reasons"], `"Locks after five tries"`), true, reasons(r))

	support.Write(t, root, map[string]string{"a/a_test.go": "package a\n\n// scenario: Locks after five tries\n"})
	commit(t, root, "test")
	tested(t, root)
	r = check(t, root)
	equal(t, r.Code, 0, r.Out())
	equal(t, r.json["ready"], true, reasons(r))
}

// An infrastructure deploy names the full commit it deploys from, which must
// be the tested HEAD; a release still needs a content digest.
func TestDeliverDeployNamesItsCommit(t *testing.T) {
	t.Parallel()
	root := deliverProject(t, "true")
	first := support.Git(t, root, "rev-parse", "HEAD")
	tested(t, root)
	head := strings.TrimSpace(support.Git(t, root, "rev-parse", "HEAD"))
	record := filepath.Join(root, deliverDir, "delivery.json")

	refused := "jhon deliver: '%s' is not an immutable artifact identifier: give its content digest (<name@>sha256:<hex>), or for an infrastructure deploy the full commit it deploys from; never a tag or a short sha\n"
	for _, a := range []string{head[:7], "v1.2.3"} {
		r := check(t, root, "--action", "deploy", "--artifact", a)
		equal(t, r.Code, 2, a)
		equal(t, r.Stdout, "")
		equal(t, r.Stderr, strings.ReplaceAll(refused, "%s", a))
	}
	rel := check(t, root, "--action", "release", "--artifact", head)
	equal(t, rel.Code, 2, "a release names a content digest")
	equal(t, rel.Stdout, "")
	equal(t, rel.Stderr, "jhon deliver: a release names its artifact by its content digest (<name@>sha256:<hex>); a commit names only an infrastructure deploy\n")
	merge := check(t, root, "--action", "merge", "--artifact", head)
	equal(t, merge.Code, 2, "a merge names no commit")
	equal(t, merge.Stdout, "")
	equal(t, merge.Stderr, "jhon deliver: a merge names no commit artifact; a commit names only an infrastructure deploy\n")
	named := check(t, root, "--action", "merge", "--artifact", "infra@"+head)
	equal(t, named.Code, 2, "a merge names no commit, named or bare")
	equal(t, named.Stdout, "")
	equal(t, named.Stderr, "jhon deliver: a merge names no commit artifact; a commit names only an infrastructure deploy\n")
	equal(t, exists(record), false, "a refusal writes nothing")

	ghost := check(t, root, "--action", "deploy", "--artifact", strings.Repeat("1", 40))
	equal(t, ghost.Code, 1, ghost.Out())
	equal(t, anyMatch(ghost.json["reasons"], `does not exist in this repository`), true, reasons(ghost))
	equal(t, exists(record), false, "a check that is not ready writes nothing")

	old := check(t, root, "--action", "deploy", "--artifact", strings.TrimSpace(first))
	equal(t, old.Code, 1, old.Out())
	equal(t, old.json["ready"], false)
	equal(t, anyMatch(old.json["reasons"], `is not HEAD`), true, reasons(old))
	equal(t, exists(record), false, "a check that is not ready writes nothing")

	r := check(t, root, "--action", "deploy", "--artifact", "infra@"+head)
	equal(t, r.Code, 0, r.Out())
	equal(t, anyMatch(r.json["notes"], `on no remote-tracking branch`), true, r.Out())
	rec := object(t, support.Read(t, record))
	equal(t, rec["action"], "deploy")
	equal(t, rec["artifact"], "infra@"+head)

	bare := check(t, root, "--action", "deploy", "--artifact", head)
	equal(t, bare.Code, 0, bare.Out())
	equal(t, object(t, support.Read(t, record))["artifact"], head, "the bare commit is recorded as given")
}
