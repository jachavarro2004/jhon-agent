package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeJhon is a script answering what guarddiff asks a jhon binary: the
// matchers, `mode`, and a hook call. With refuse set, safety-guard refuses
// every Bash command holding "rm -rf"; every other call is allowed.
func fakeJhon(t *testing.T, dir, name string, refuse bool) string {
	t.Helper()
	judge := `cat >/dev/null; exit 0`
	if refuse {
		judge = `if [ "$5" = safety-guard ] && grep -q 'rm -rf'; then echo 'safety-guard: refused: it deletes' >&2; exit 2; fi; exit 0`
	}
	script := `#!/bin/sh
case "$1 $2 $3" in
"hook pre-tool --matchers") printf 'safety-guard\tBash\nvcs-guard\tWrite\nframework-guard\tWrite|Bash\nsecrets-guard\tRead\nscope-guard\tWrite|Bash\nroute-guard\tWrite|Bash\n'; exit 0 ;;
"mode "*) exit 0 ;;
esac
` + judge + "\n"
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func runDiff(t *testing.T, allow string) (int, string, string) {
	t.Helper()
	dir := t.TempDir()
	lines := filepath.Join(dir, "lines")
	files := map[string]string{
		"lines/set.tsv":                        "# place\tfixture\tcaller\ttool\tinput\tsource\nfolder\tnone\ttop-default\tBash\t\"rm -rf build\"\tt\nfolder\tnone\ttop-default\tBash\t\"ls\"\tt\n",
		"lines/trees.txt":                      "-- all proj/README --\nhi\n",
		"allow.tsv":                            allow,
		"root/prose/contracts/vocabulary.yaml": "vcs_dirs: [.git, .hg]\n",
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old, new := fakeJhon(t, dir, "old", false), fakeJhon(t, dir, "new", true)
	root := filepath.Join(dir, "root")
	var out, errs bytes.Buffer
	code := run([]string{"--old", old, "--new", new, "--old-root", root, "--new-root", root, "--lines", lines,
		"--allow", filepath.Join(dir, "allow.tsv"), "--scratch", dir}, &out, &errs)
	return code, out.String(), errs.String()
}

func TestChangedVerdictOffTheAllowListFails(t *testing.T) {
	code, out, errs := runDiff(t, "# id\tguard\told\tnew\twhy\n")
	if code != 1 {
		t.Fatalf("exit %d, want 1: %s%s", code, out, errs)
	}
	want := "set.tsv:2@folder/top-default\tsafety-guard\tallow -> refuse\tNOT ALLOWED\tsafety-guard: refused: it deletes\n"
	if !strings.Contains(out, want) {
		t.Errorf("no changed verdict line %q in:\n%s", want, out)
	}
	if !strings.Contains(out, "guarddiff: 2 lines, 1 changed verdict (0 allowed, 1 not on the allow list)\n") {
		t.Errorf("no count in:\n%s", out)
	}
}

func TestChangedVerdictOnTheAllowListPasses(t *testing.T) {
	code, out, errs := runDiff(t, "set.tsv:2@folder/top-default\tsafety-guard\tallow\trefuse\tT0: rm is judged now\n")
	if code != 0 {
		t.Fatalf("exit %d, want 0: %s%s", code, out, errs)
	}
	if !strings.Contains(out, "set.tsv:2@folder/top-default\tsafety-guard\tallow -> refuse\tallowed\t") ||
		!strings.Contains(out, "guarddiff: 2 lines, 1 changed verdict (1 allowed, 0 not on the allow list)\n") {
		t.Errorf("output:\n%s", out)
	}
}

// The checked-in set reads, and its fuzz file is the generator's output.
func TestCheckedInSet(t *testing.T) {
	dir := filepath.Join("..", "..", "test", "guards", "corpus")
	lines, err := loadLines(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) < 4000 {
		t.Errorf("%d lines, want the corpus, the matrix, the rows and the fuzz set", len(lines))
	}
	got, err := os.ReadFile(filepath.Join(dir, "fuzz.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != fuzzTSV() {
		t.Error("test/guards/corpus/fuzz.tsv is not the generator's output: run go run ./tools/guarddiff gen-fuzz")
	}
}
