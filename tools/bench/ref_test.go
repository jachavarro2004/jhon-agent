package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitOut is git's trimmed output in dir.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir, c.Env = dir, append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// fakeBuild stands in for `go build` of the source at src: its build/jhon
// packs an artifact whose own jhon, on `release install`, writes a bench
// jhon that reports src's HEAD commit and src's VERSION file, as a real
// install of that commit reports it in `jhon version --json`.
func fakeBuild(t *testing.T) func(src string, env []string) error {
	return func(src string, _ []string) error {
		commit := gitOut(t, src, "rev-parse", "HEAD")
		file, _ := os.ReadFile(filepath.Join(src, "VERSION"))
		installed := fmt.Sprintf(`#!/bin/sh
case "$1" in version) echo '{"version":"0.0.0-dev+%s","commit":"%s","file":"%s"}';; esac
`, commit[:7], commit, strings.TrimSpace(string(file)))
		installer := fmt.Sprintf("#!/bin/sh\nmkdir -p \"$HOME/.local/bin\"\ncat > \"$HOME/.local/bin/jhon\" <<'EOI'\n%sEOI\nchmod +x \"$HOME/.local/bin/jhon\"\n", installed)
		packer := fmt.Sprintf(`#!/bin/sh
[ "$1 $2" = "release pack" ] || exit 2
d=$(mktemp -d); trap 'rm -rf "$d"' EXIT; mkdir -p "$d/build"
cat > "$d/build/jhon" <<'EOS'
%sEOS
chmod +x "$d/build/jhon"
tar -czf "$4/jhon-fake.tar.gz" -C "$d" build/jhon
`, installer)
		if err := os.MkdirAll(filepath.Join(src, "build"), 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(src, "build", "jhon"), []byte(packer), 0o755)
	}
}

// `bench prepare --ref <commit>` installs that commit's build, made from a
// clone of the checkout at the commit in scratch; the checkout stays as it is.
// Nothing is left in TMPDIR (T267 S3: the fake packer's mktemp -d folder was).
func TestPrepareRefInstallsThatCommitsBuild(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Cleanup(func() {
		if left, _ := os.ReadDir(tmp); len(left) > 0 {
			t.Errorf("left in TMPDIR: %s", left)
		}
	})
	checkout := t.TempDir()
	gitOut(t, checkout, "init", "-q")
	for _, v := range []string{"old", "new"} {
		if err := os.WriteFile(filepath.Join(checkout, "VERSION"), []byte(v+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitOut(t, checkout, "add", "-A")
		gitOut(t, checkout, "commit", "-q", "-m", v)
	}
	old, head := gitOut(t, checkout, "rev-parse", "HEAD~1"), gitOut(t, checkout, "rev-parse", "HEAD")
	refs := gitOut(t, checkout, "for-each-ref")

	home := filepath.Join(t.TempDir(), "home")
	var out bytes.Buffer
	ri := releaseInstaller{checkout: checkout, ref: old[:9], build: fakeBuild(t)}
	if err := Prepare(home, t.TempDir(), os.Environ(), ri, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	got, err := benchCommit(home)
	if err != nil || got.Commit != old || !strings.Contains(out.String(), "jhon "+old) {
		t.Fatalf("installed %+v (%v), want %s\n%s", got, err, old, out.String())
	}
	b, _ := exec.Command(benchJhon(home), "version", "--json").Output()
	var v map[string]string
	if json.Unmarshal(b, &v) != nil || v["file"] != "old" {
		t.Errorf("the build was not of the old tree: %s", b)
	}
	if gitOut(t, checkout, "rev-parse", "HEAD") != head || gitOut(t, checkout, "status", "--porcelain") != "" || gitOut(t, checkout, "for-each-ref") != refs {
		t.Errorf("the checkout changed")
	}
	if err := (releaseInstaller{checkout: checkout, ref: "no-such-ref", build: fakeBuild(t)}).Install(home, os.Environ(), &out); err == nil ||
		!strings.Contains(err.Error(), "no-such-ref") {
		t.Errorf("a bad ref: %v", err)
	}
}

// `bench run --expect-commit` refuses a bench HOME whose jhon is another
// build, so a candidate run cannot use the baseline's jhon; every run
// records the jhon it ran.
func TestRunRecordsTheBenchJhonAndRefusesAnUnexpectedOne(t *testing.T) {
	f := newFake(t, "0.40")
	if err := os.WriteFile(filepath.Join(f.dir, "version.json"), []byte(`{"version":"0.0.0-dev+752cb38","commit":"752cb382c85af5dd539b69caaff2b1d85b00c20b"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	o, out := options(t, f, "pagination-bug", []string{"jhon"}, 1)
	o.ExpectCommit = "0123456789ab"
	err := Execute(o)
	if err == nil || !strings.Contains(err.Error(), "752cb382c85a") || !strings.Contains(err.Error(), "0123456789ab") || !strings.Contains(err.Error(), "bench prepare --ref 0123456789ab") {
		t.Fatalf("err %v\n%s", err, out)
	}
	if calls := strings.Join(f.calls(t), "\n"); strings.Contains(calls, "jhon start") || fileExists(o.Results) {
		t.Fatalf("it ran after refusing:\n%s", calls)
	}
	o, out = options(t, f, "pagination-bug", []string{"jhon"}, 1)
	o.ExpectCommit = "752cb382"
	if err := Execute(o); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	m := metricsOf(t, o, "pagination-bug-plain-jhon-1")
	if m.JhonCommit != "752cb382c85af5dd539b69caaff2b1d85b00c20b" || m.JhonVersion != "0.0.0-dev+752cb38" {
		t.Errorf("metrics jhon %q %q", m.JhonCommit, m.JhonVersion)
	}
	o.ExpectCommit = "752"
	if err := Execute(o); err == nil || !strings.Contains(err.Error(), "at least 7") {
		t.Errorf("a too-short commit: %v", err)
	}
}
