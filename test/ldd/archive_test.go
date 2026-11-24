// Tests for `jhon archive`: the mechanical move of a change directory into
// the archive (the archive step, step 4).
//
// The method is decided before anything moves: a change directory holding a
// tracked file moves with `git mv`, one holding none moves with `mv`. A failed
// `git mv` is reported with git's own message and never falls back. Every
// project is a temporary git repository with one commit, built outside any
// other repository, and the command runs with no inherited GIT_* variable.
package ldd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

const (
	archiveChange = "add-login"
	archiveDate   = "2026-09-23"
)

var (
	archiveSource = filepath.Join("openspec", "changes", archiveChange)
	archiveDest   = filepath.Join("openspec", "changes", "archive", archiveDate+"-"+archiveChange)
	archiveFiles  = map[string]string{
		"proposal.md":        "# Proposal\n",
		"tasks.md":           "- [x] 1.1 done\n",
		"specs/auth/spec.md": "# Auth\n",
	}
)

// archiveProject is a git repository with one commit and the change
// directory written, untracked, its RED before GREEN on record (T223 S2).
func archiveProject(t *testing.T) string {
	t.Helper()
	root := bareArchiveProject(t)
	recordRedGreen(t, filepath.Join(root, archiveSource), true)
	return root
}

// bareArchiveProject is archiveProject with no test run on record.
func bareArchiveProject(t *testing.T) string {
	t.Helper()
	root := support.GitRepo(t, temp(t))
	support.Write(t, filepath.Join(root, archiveSource), archiveFiles)
	return root
}

// recordRedGreen writes the change directory dir's execution record: a
// last run that passed, after an earlier one that failed when red (its RED
// before GREEN), else with no earlier run.
func recordRedGreen(t *testing.T, dir string, red bool) {
	t.Helper()
	support.Write(t, dir, map[string]string{"execution-record.json": redGreenRecord(red)})
}

// redGreenRecord is the content recordRedGreen writes.
func redGreenRecord(red bool) string {
	history := `[]`
	if red {
		history = `[{"timestamp":"2026-09-20T10:00:00.000Z","passed":false}]`
	}
	return `{"timestamp":"2026-09-20T10:01:00.000Z","complete":true,"scope":"full",` +
		`"executions":[{"project":".","command":"true","status":"pass","exit_code":0}],"history":` + history + "}\n"
}

func archiveMove(t *testing.T, root string, args ...string) support.Result {
	t.Helper()
	return support.Jhon(t, root, support.Env(), append([]string{"archive"}, args...)...)
}

// assertArchived checks that the archived files carry exactly the source
// content.
func assertArchived(t *testing.T, root string) {
	t.Helper()
	equal(t, exists(filepath.Join(root, archiveSource)), false, "the source directory is gone")
	for rel, content := range archiveFiles {
		equal(t, support.Read(t, filepath.Join(root, archiveDest, rel)), content, rel)
	}
}

func commitSource(t *testing.T, root string, paths ...string) {
	t.Helper()
	support.Git(t, root, append([]string{"add"}, paths...)...)
	support.Git(t, root, "commit", "-q", "-m", "change")
}

func TestArchiveTrackedUsesGitMv(t *testing.T) {
	t.Parallel()
	root := archiveProject(t)
	commitSource(t, root, archiveSource)
	r := archiveMove(t, root, archiveChange, archiveDate)
	equal(t, r.Code, 0, r.Out())
	match(t, r.Stdout, `(?m)^method: git-mv$`)
	assertArchived(t, root)
	match(t, support.Git(t, root, "ls-files", "--", archiveDest), `proposal\.md`)
	equal(t, support.Git(t, root, "ls-files", "--", archiveSource), "")
}

func TestArchiveUntrackedUsesMv(t *testing.T) {
	t.Parallel()
	root := archiveProject(t)
	r := archiveMove(t, root, archiveChange, archiveDate)
	equal(t, r.Code, 0, r.Out())
	match(t, r.Stdout, `(?m)^method: mv$`)
	noMatch(t, r.Stderr, `fatal`)
	assertArchived(t, root)
	equal(t, support.Git(t, root, "ls-files", "--", archiveDest), "")
}

func TestArchivePartiallyTracked(t *testing.T) {
	t.Parallel()
	root := archiveProject(t)
	commitSource(t, root, filepath.Join(archiveSource, "proposal.md"))
	r := archiveMove(t, root, archiveChange, archiveDate)
	equal(t, r.Code, 0, r.Out())
	match(t, r.Stdout, `(?m)^method: git-mv$`)
	assertArchived(t, root)
}

func TestArchiveFailedGitMvNeverFallsBack(t *testing.T) {
	t.Parallel()
	root := archiveProject(t)
	commitSource(t, root, archiveSource)
	// A stale index lock makes git refuse to write the index.
	support.Write(t, root, map[string]string{".git/index.lock": ""})
	r := archiveMove(t, root, archiveChange, archiveDate)
	if r.Code == 0 {
		t.Errorf("exit 0: %s", r.Out())
	}
	match(t, r.Stderr, `fatal: Unable to create '.*index\.lock': File exists`)
	noMatch(t, r.Stdout, `(?m)^method: mv$`)
	equal(t, exists(filepath.Join(root, archiveDest)), false, "no fallback moved the directory")
	for rel, content := range archiveFiles {
		equal(t, support.Read(t, filepath.Join(root, archiveSource, rel)), content, rel)
	}
}

func TestArchiveDestinationCollision(t *testing.T) {
	t.Parallel()
	root := archiveProject(t)
	support.Write(t, filepath.Join(root, archiveDest), map[string]string{"keep.md": "keep\n"})
	r := archiveMove(t, root, archiveChange, archiveDate)
	equal(t, r.Code, 1, r.Out())
	match(t, r.Stderr, `archive destination collision`)
	equal(t, support.Read(t, filepath.Join(root, archiveDest, "keep.md")), "keep\n")
	equal(t, support.Read(t, filepath.Join(root, archiveSource, "proposal.md")), archiveFiles["proposal.md"])
}

func TestArchiveRefusesBadArguments(t *testing.T) {
	t.Parallel()
	root := archiveProject(t)
	for _, args := range [][]string{{}, {archiveChange}, {archiveChange, "23-09-2026"}, {"../x", archiveDate}, {"archive", archiveDate}} {
		r := archiveMove(t, root, args...)
		equal(t, r.Code, 2, fmt.Sprintf("%q: %s", args, r.Out()))
		match(t, r.Stderr, `usage:`)
	}
	equal(t, exists(filepath.Join(root, archiveSource, "proposal.md")), true)
}

// Docket AJ: tdd-evidence.md, the apply's append-only TDD evidence, lives in
// the change directory, so the archive move carries it with the change,
// byte-identical, whichever method moves it.
func TestArchiveCarriesTDDEvidence(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		method  string
		tracked bool
	}{{"git mv", true}, {"mv", false}} {
		t.Run("tdd-evidence.md moves with the change, byte-identical ("+c.method+")", func(t *testing.T) {
			t.Parallel()
			root := archiveProject(t)
			evidence := "# TDD Evidence — add-login\n\n## Run 1 — 2026-09-20T10:00:00Z\nsession: unknown\ncommit: unknown\n"
			support.Write(t, filepath.Join(root, archiveSource), map[string]string{"tdd-evidence.md": evidence})
			if c.tracked {
				commitSource(t, root, archiveSource)
			}
			r := archiveMove(t, root, archiveChange, archiveDate)
			equal(t, r.Code, 0, r.Out())
			equal(t, support.Read(t, filepath.Join(root, archiveDest, "tdd-evidence.md")), evidence)
			_, err := os.Stat(filepath.Join(root, archiveSource, "tdd-evidence.md"))
			equal(t, os.IsNotExist(err), true, "the source evidence is gone")
		})
	}
}

// T223 S1: the archive reference names where `jhon status --json` reports
// a change's RED before GREEN, and that path holds it in the real JSON:
// true once the change's RED before GREEN is on record, false with a GREEN
// alone.
func TestArchiveDocNamesTheRealRedBeforeGreenField(t *testing.T) {
	t.Parallel()
	doc := support.Read(t, support.Path("prose", "skills", "ldd", "references", "archive.md"))
	m := regexp.MustCompile("\\(`([\\w.]+)` in `jhon status --json`\\)").FindStringSubmatch(strings.Join(strings.Fields(doc), " "))
	if m == nil {
		t.Fatalf("archive.md names no field of `jhon status --json`:\n%s", doc)
	}
	at := func(s map[string]any) any {
		var v any = s
		for _, k := range strings.Split(m[1], ".") {
			o, ok := v.(map[string]any)
			if !ok {
				return nil
			}
			v = o[k]
		}
		return v
	}
	root := archiveProject(t)
	equal(t, at(statusJSON(t, root, archiveChange)), true, m[1]+" with its RED before GREEN on record")
	green := bareArchiveProject(t)
	recordRedGreen(t, filepath.Join(green, archiveSource), false)
	equal(t, at(statusJSON(t, green, archiveChange)), false, m[1]+" with a GREEN alone")
}

// T223 S2: `jhon archive` refuses a change with no RED before GREEN on
// record, the fact `jhon status --json` reports as tests.red_before_green:
// exit 1, nothing on stdout, nothing moved, and a message that names what
// is missing, how to record it, and that no route archives without it.
func TestArchiveRefusesWithoutRedBeforeGreen(t *testing.T) {
	t.Parallel()
	want := refusal(archiveChange, "openspec")
	for name, setup := range map[string]func(t *testing.T, dir string){
		"no run on record": func(t *testing.T, dir string) {},
		"a GREEN alone":    func(t *testing.T, dir string) { recordRedGreen(t, dir, false) },
		"a RED alone": func(t *testing.T, dir string) {
			support.Write(t, dir, map[string]string{"execution-record.json": `{"timestamp":"2026-09-20T10:00:00.000Z","complete":true,"scope":"full",` +
				`"executions":[{"project":".","command":"false","status":"fail","exit_code":1}],"history":[]}` + "\n"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := bareArchiveProject(t)
			setup(t, filepath.Join(root, archiveSource))
			if s := statusJSON(t, root, archiveChange); s["tests"] != nil {
				equal(t, s["tests"].(map[string]any)["red_before_green"], false, "status reports the same fact")
			}
			r := archiveMove(t, root, archiveChange, archiveDate)
			equal(t, r.Code, 1, r.Out())
			equal(t, r.Stdout, "", "nothing on stdout")
			equal(t, r.Stderr, want)
			equal(t, exists(filepath.Join(root, archiveDest)), false, "nothing was moved")
			equal(t, exists(filepath.Join(root, "openspec", "changes", "archive")), false, "no archive directory")
			for rel, content := range archiveFiles {
				equal(t, support.Read(t, filepath.Join(root, archiveSource, rel)), content, rel)
			}
		})
	}
	// PRESERVE: a usage error and a missing source are reported as before,
	// ahead of the record.
	root := bareArchiveProject(t)
	r := archiveMove(t, root, archiveChange)
	equal(t, r.Code, 2, r.Out())
	match(t, r.Stderr, `^usage: jhon archive <change-name> <YYYY-MM-DD>`)
	r = archiveMove(t, root, "ghost", archiveDate)
	equal(t, r.Code, 1, r.Out())
	equal(t, r.Stderr, "archive source "+filepath.Join("openspec", "changes", "ghost")+" is not a directory; nothing was moved.\n")
}

// PRESERVE (T223 S2): with its RED before GREEN on record, a change is
// archived exactly as before: the method, the readback, and every file,
// the execution record with them.
func TestArchiveWithRedBeforeGreenMovesAsBefore(t *testing.T) {
	t.Parallel()
	root := archiveProject(t)
	record := support.Read(t, filepath.Join(root, archiveSource, "execution-record.json"))
	r := archiveMove(t, root, archiveChange, archiveDate)
	equal(t, r.Code, 0, r.Out())
	match(t, r.Stdout, `(?m)^method: mv\nreadback: diff -r <snapshot> `+regexp.QuoteMeta(archiveDest)+`\nreadback: empty$`)
	assertArchived(t, root)
	equal(t, support.Read(t, filepath.Join(root, archiveDest, "execution-record.json")), record)
}

// refusal is `jhon archive`'s refusal of change with no RED before GREEN
// on record, in a layout whose directory is dir.
func refusal(change, dir string) string {
	return "archive refused: " + change + " has no RED before GREEN on record (`tests.red_before_green` in `jhon status " + change + " --json` is not true); nothing was moved. " +
		"Record it: run `jhon executions run " + change + " --affected <base>` while its new test fails (RED), a unit's run (`--affected`) between, and `--affected <base>` again once it passes (GREEN), " +
		"where <base> is the commit the slice started from (with none, a full `jhon executions run " + change + "`); then archive again. " +
		"Only a change that alters no behaviour archives without it, while a file outside " + dir + "/ differs from the default branch (before its merge): " +
		"the standard and the verified route both keep the RED and GREEN runs of a change that alters behaviour (contracts/ldd.md, \"The record\").\n"
}

// T223 S2 (L4, A3): a change whose route records no tests by design, one
// that alters no behaviour, archives with no RED before GREEN, while its
// diff against the default branch is not empty; once that diff is empty
// (after its merge) it is refused, as a change that alters behaviour is.
// `jhon status` agrees: archive where archive takes it, apply where not.
func TestArchiveExemptsAChangeThatAltersNoBehaviour(t *testing.T) {
	t.Parallel()
	project := func(t *testing.T, files map[string]string) (root, def string) {
		t.Helper()
		root = support.GitRepo(t, temp(t))
		def = strings.TrimSpace(support.Git(t, root, "rev-parse", "--abbrev-ref", "HEAD"))
		support.Write(t, root, map[string]string{"src/app.py": "X = 1\n", "tests/test_app.py": "def test_x():\n    pass\n"})
		commit(t, root, "base")
		support.Git(t, root, "checkout", "-q", "-b", "feat/c1")
		support.Write(t, root, map[string]string{"openspec/changes/c1/tasks.md": "## Intent\nDelivers: the guide\n\n- [x] 1.1 guide\n"})
		support.Write(t, root, files)
		return root, def
	}
	refused := func(t *testing.T, root, what string) {
		t.Helper()
		s := statusJSON(t, root, "c1")
		equal(t, s["next"] == "archive", false, what, s["next"], s["notes"])
		if s["next"] == "apply" {
			equal(t, anyMatch(s["notes"], "^no RED before GREEN on record for c1: "), true, what, s["notes"])
		}
		r := archiveMove(t, root, "c1", archiveDate)
		equal(t, r.Code, 1, what, r.Out())
		equal(t, r.Stdout, "", what)
		equal(t, r.Stderr, refusal("c1", "openspec"), what)
		equal(t, exists(filepath.Join(root, "openspec/changes/c1/tasks.md")), true, what, "nothing was moved")
	}

	// A docs-only change with a diff: no RED needed, said in one line.
	docs, _ := project(t, map[string]string{"docs/guide.md": "# Guide\n"})
	s := statusJSON(t, docs, "c1")
	equal(t, s["route"].(map[string]any)["behaviour"], false, s["route"])
	equal(t, s["next"], "archive", s["notes"])
	equal(t, anyMatch(s["notes"], "RED before GREEN"), false, s["notes"])
	r := archiveMove(t, docs, "c1", archiveDate)
	equal(t, r.Code, 0, r.Out())
	equal(t, r.Stderr, "")
	match(t, r.Stdout, `^no RED before GREEN needed: c1 alters no behaviour \(every file outside openspec/ that differs from the default branch is a test or a document\)\nmethod: mv\n`)
	equal(t, exists(filepath.Join(docs, "openspec/changes/archive", archiveDate+"-c1", "tasks.md")), true, "archived")

	// The same change once merged: its diff is empty, no exemption.
	merged, def := project(t, map[string]string{"docs/guide.md": "# Guide\n"})
	commit(t, merged, "c1")
	support.Git(t, merged, "branch", "-f", def, "HEAD")
	refused(t, merged, "the docs-only change after its merge")

	// A change that alters behaviour, its diff not empty.
	code, _ := project(t, map[string]string{"src/app.py": "X = 2\n"})
	refused(t, code, "a behaviour change")
}

// T223 (verify L5): an umbrella is split, never applied: its status names
// no RED before GREEN to record.
func TestUmbrellaStatusOwesNoRedBeforeGreenNote(t *testing.T) {
	t.Parallel()
	root := support.GitRepo(t, temp(t))
	support.Git(t, root, "checkout", "-q", "-b", "feat/u")
	support.Write(t, root, map[string]string{"openspec/changes/u/exploration.md": "# e\n"})
	s := statusJSON(t, root, "u")
	equal(t, s["next"], "start-change", s["notes"])
	equal(t, anyMatch(s["notes"], "^no RED before GREEN on record"), false, s["notes"])
	equal(t, s["command"] == nil || s["command"] == "", true, s["command"])
}
