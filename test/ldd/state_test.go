// T134 (L1021 H4, H5): at a slice-closing commit, a recorded verify, and an
// archive, the engine writes ONE state record of the change to Engram (`ldd
// state: <change>`, upserted by its topic key): phase, slices done, last
// test run, delivery, the decisions the handoff's search finds, and `Refs:`
// from CodeGraph without line numbers. A failing, slow, or absent Engram
// changes nothing of the transition but one note. The handoff's search
// carries the diff's symbol and file names. Every Engram and CodeGraph here
// is a fake that records its arguments.
package ldd

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

const fakeEngramScript = `#!/bin/sh
d='CALLS'
n=$(ls "$d" | wc -l)
for a in "$@"; do printf '%s\n' "$a"; done > "$d/tmp" && mv "$d/tmp" "$d/$(printf %03d "$n")"
case "$1" in
search)
  case "$*" in
    *"--type decision"*) printf 'Found 1 memories:\n\n[1] #101 (decision) — Send mail through the queue\n    body\n' ;;
    *) printf 'Found 0 memories\n' ;;
  esac ;;
save)
  case "$FAKE_ENGRAM" in
    fail) echo "engram: database is locked" >&2; exit 1 ;;
    slow) exec sleep 10 ;;
  esac
  printf 'Memory saved: #7 "%s" (ldd-state)\n' "$2" ;;
esac
`

// The fake index knows every name as a symbol of a/mailer.go, at a line.
const fakeCodegraphScript = `#!/bin/sh
kind=function
case "$*" in *"--kind file"*) kind=file ;; esac
printf '[{"node":{"kind":"%s","name":"%s","filePath":"a/mailer.go","startLine":7,"endLine":9},"score":1}]' "$kind" "$2"
`

type stateFixture struct {
	root, calls string
	env         []string
}

// stateProject is a project under LDD on branch feat: change c1, slice 1
// closed and slice 2 open, a/mailer.go declaring Send, a CodeGraph index,
// and the fakes on PATH.
func stateProject(t *testing.T, mode string) stateFixture {
	t.Helper()
	root := support.GitRepo(t, temp(t))
	bin := temp(t)
	calls := filepath.Join(bin, "calls")
	if err := os.MkdirAll(calls, 0o755); err != nil {
		t.Fatal(err)
	}
	support.Write(t, bin, map[string]string{"engram": strings.ReplaceAll(fakeEngramScript, "CALLS", calls), "codegraph": fakeCodegraphScript})
	for _, f := range []string{"engram", "codegraph"} {
		if err := os.Chmod(filepath.Join(bin, f), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	support.Write(t, root, map[string]string{
		"openspec/config.yaml":         "test_command_override:\n  a: \"true\"\n",
		"a/go.mod":                     "module example.com/a\n",
		"openspec/changes/c1/tasks.md": "## Slice 1: a\n\n- [ ] 1.1 a\n\n## Slice 2: b\n\n- [ ] 2.1 b\n",
		".gitignore":                   ".codegraph/\n",
		".codegraph/codegraph.db":      "",
	})
	support.Git(t, root, "checkout", "-q", "-B", "main")
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "base")
	support.Git(t, root, "checkout", "-q", "-b", "feat")
	support.Write(t, root, map[string]string{
		"a/mailer.go":                  "package a\n\nfunc Send() int { return 1 }\n",
		"openspec/changes/c1/tasks.md": "## Slice 1: a\n\n- [x] 1.1 a\n\n## Slice 2: b\n\n- [ ] 2.1 b\n",
	})
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "slice 1")
	env := support.Env("JHON_ENGRAM="+filepath.Join(bin, "engram"), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"JHON_SETTINGS="+filepath.Join(bin, "no-settings.toml"), "FAKE_ENGRAM="+mode)
	return stateFixture{root, calls, env}
}

// calls are the fake Engram's invocations, each its arguments, in order.
func (f stateFixture) engramCalls(t *testing.T) [][]string {
	t.Helper()
	entries, err := os.ReadDir(f.calls)
	if err != nil {
		t.Fatal(err)
	}
	var out [][]string
	for _, e := range entries {
		if e.Name() == "tmp" {
			continue
		}
		out = append(out, strings.Split(strings.TrimSuffix(support.Read(t, filepath.Join(f.calls, e.Name())), "\n"), "\n"))
	}
	return out
}

// saves are the fake Engram's `save` calls.
func (f stateFixture) saves(t *testing.T) [][]string {
	var out [][]string
	for _, c := range f.engramCalls(t) {
		if c[0] == "save" {
			out = append(out, c)
		}
	}
	return out
}

func (f stateFixture) jhon(t *testing.T, args ...string) support.Result {
	t.Helper()
	return support.Jhon(t, f.root, f.env, args...)
}

// oneSave asserts the state record's shape and returns its content.
func oneSave(t *testing.T, save []string) string {
	t.Helper()
	// save <title> <content> --type ldd-state --topic ldd-state/c1
	tail := strings.Join(save[len(save)-4:], " ")
	equal(t, save[1], "ldd state: c1", "the record's title")
	equal(t, tail, "--type ldd-state --topic ldd-state/c1", "upserted by the change's topic key")
	content := strings.Join(save[2:len(save)-4], "\n")
	match(t, content, `(?m)^decisions:\n- #101 \(decision\) Send mail through the queue$`, content)
	match(t, content, `(?m)^Refs:\nfile mailer\.go @ a/mailer\.go\nfunction Send @ a/mailer\.go$`, content)
	noMatch(t, content, `@ \S+:\d|startLine|\b7\b`, "refs carry no line numbers")
	return content
}

func TestArchiveWritesTheChangeState(t *testing.T) {
	t.Parallel()
	f := stateProject(t, "")
	recordRedGreen(t, filepath.Join(f.root, "openspec/changes/c1"), true)
	r := f.jhon(t, "archive", "c1", "2026-11-16")
	equal(t, r.Code, 0, r.Stderr)
	equal(t, r.Stderr, "", "a written state adds no note")
	saves := f.saves(t)
	equal(t, len(saves), 1, "one record")
	if len(saves) != 1 {
		t.FailNow()
	}
	content := oneSave(t, saves[0])
	match(t, content, `(?m)^phase: archive\nslices: 1 of 2 done\nlast test run: passed 2026-09-20T10:01:00\.000Z\ndelivery: archived to openspec/changes/archive/2026-11-16-c1$`, content)
}

func TestVerifyWritesAndUpdatesTheChangeState(t *testing.T) {
	t.Parallel()
	f := stateProject(t, "")
	equal(t, f.jhon(t, "executions", "run", "c1").Code, 0, "the change's tests run")
	r := f.jhon(t, "executions", "verify", "c1", "--verdict", "fail", "--note", "first")
	equal(t, r.Code, 0, r.Stderr)
	equal(t, r.Stderr, "", "a written state adds no note")
	r = f.jhon(t, "executions", "verify", "c1", "--verdict", "fail", "--note", "second")
	equal(t, r.Code, 0, r.Stderr)
	saves := f.saves(t)
	equal(t, len(saves), 2, "one save per verify")
	if len(saves) != 2 {
		t.FailNow()
	}
	for _, s := range saves {
		content := oneSave(t, s)
		match(t, content, `(?m)^phase: verify\nslices: 1 of 2 done\nlast test run: passed \S+, light verify fail\ndelivery: light verify fail of tree [0-9a-f]+$`, content)
	}
	equal(t, saves[0][1], saves[1][1], "the second verify updates the same record, by title")
	equal(t, saves[0][len(saves[0])-1], saves[1][len(saves[1])-1], "and by topic key, which engram save --topic upserts")
}

func TestASliceCommitWritesTheChangeStateOnceItLands(t *testing.T) {
	t.Parallel()
	f := stateProject(t, "")
	support.Write(t, f.root, map[string]string{
		"a/mailer.go":                  "package a\n\nfunc Send() int { return 2 }\n",
		"openspec/changes/c1/tasks.md": "## Slice 1: a\n\n- [x] 1.1 a\n\n## Slice 2: b\n\n- [x] 2.1 b\n",
	})
	r := support.Hook(t, f.env, "route-guard", map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": "git add -A && git commit -m 'slice 2'"},
		"cwd": f.root, "permission_mode": "default"})
	equal(t, r.Code, 0, r.Stderr)
	equal(t, r.Stderr, "", "nothing on stderr")
	time.Sleep(500 * time.Millisecond)
	equal(t, len(f.saves(t)), 0, "nothing is written before the commit lands")
	support.Git(t, f.root, "add", "-A")
	support.Git(t, f.root, "commit", "-q", "-m", "slice 2")
	head := strings.TrimSpace(support.Git(t, f.root, "rev-parse", "HEAD"))
	var saves [][]string
	for end := time.Now().Add(15 * time.Second); len(saves) == 0 && time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		saves = f.saves(t)
	}
	equal(t, len(saves), 1, "one record once the commit lands")
	if len(saves) != 1 {
		t.FailNow()
	}
	content := oneSave(t, saves[0])
	match(t, content, `(?m)^phase: apply\nslices: 2 of 2 done\nlast test run: none on record\ndelivery: Slice 2: b committed at `+head[:12]+`$`, content)
}

func TestAFailingSlowOrAbsentEngramLeavesTheTransitionAlone(t *testing.T) {
	t.Parallel()
	archive := func(mode, engram string) (support.Result, time.Duration) {
		f := stateProject(t, mode)
		recordRedGreen(t, filepath.Join(f.root, "openspec/changes/c1"), true)
		if engram != "" {
			f.env = append(f.env, "JHON_ENGRAM="+engram)
		}
		start := time.Now()
		return f.jhon(t, "archive", "c1", "2026-11-16"), time.Since(start)
	}
	want, _ := archive("", support.NoEngram)
	equal(t, want.Code, 0, want.Stderr)
	equal(t, want.Stderr, "", "absent: no note")
	for _, c := range []struct{ mode, note string }{
		{"fail", "jhon: the state of c1 was not saved to Engram (engram: database is locked); the archive stands\n"},
		{"slow", "jhon: the state of c1 was not saved to Engram (no answer within 3s); the archive stands\n"},
	} {
		r, took := archive(c.mode, "")
		equal(t, r.Code, want.Code, c.mode)
		equal(t, r.Stdout, want.Stdout, c.mode+": the same output")
		equal(t, r.Stderr, c.note, c.mode+": one note")
		if took > 6*time.Second {
			t.Errorf("%s: the archive took %s", c.mode, took)
		}
	}
}

func TestTheHandoffSearchCarriesTheDiffsNames(t *testing.T) {
	t.Parallel()
	f := stateProject(t, "")
	r := f.jhon(t, "context", "c1", "--phase", "none", "--handoff")
	equal(t, r.Code, 0, r.Stderr)
	var searches []string
	for _, c := range f.engramCalls(t) {
		if c[0] == "search" {
			searches = append(searches, c[1])
		}
	}
	equal(t, len(searches), 2, "the two types")
	if len(searches) != 2 {
		t.FailNow()
	}
	for _, q := range searches {
		words := strings.Fields(q)
		for _, w := range []string{"c1", "Send", "mailer.go"} {
			if !slices.Contains(words, w) {
				t.Errorf("the search %q lacks %q", q, w)
			}
		}
	}
	if !regexp.MustCompile(`"decisions":\[\{"id":101`).MatchString(r.Stdout) {
		t.Errorf("decisions: %s", r.Stdout)
	}
}
