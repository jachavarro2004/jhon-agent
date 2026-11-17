package guard

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// decision is what the safety guard does with a command's findings
// (hook/safetyguard.go): refuse on a deny, ask on an ask, else allow.
func decision(t *testing.T, fs []Finding) string {
	t.Helper()
	d := "allow"
	for _, f := range fs {
		switch f.Level {
		case "deny":
			d = "refuse"
		case "ask":
			if d == "allow" {
				d = "ask"
			}
		default:
			t.Fatalf("finding level %q is neither deny nor ask: %+v", f.Level, f)
		}
	}
	return d
}

// T53: the safety guard's judging (Classify) never panics, judges one
// command the same way twice, and decides allow, ask, or refuse. Seeds are
// the guard corpus's Bash inputs (test/guards/corpus); the judging runs in
// an empty directory outside any repository.
func FuzzClassify(f *testing.F) {
	files, err := filepath.Glob("../../../test/guards/corpus/*.tsv")
	if err != nil || len(files) == 0 {
		f.Fatalf("no guard corpus: %v", err)
	}
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			f.Fatal(err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			col := strings.Split(line, "\t")
			if strings.HasPrefix(line, "#") || len(col) < 3 || col[0] != "safety-guard" || col[1] != "Bash" {
				continue
			}
			if s, err := strconv.Unquote(col[2]); err == nil {
				f.Add(s)
			}
		}
	}
	dir := f.TempDir()
	f.Fuzz(func(t *testing.T, command string) {
		a := Classify(command, ContextOf(dir))
		b := Classify(command, ContextOf(dir))
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("%q: judged twice differently:\n%+v\n%+v", command, a, b)
		}
		if da, db := decision(t, a), decision(t, b); da != db {
			t.Fatalf("%q: decisions %s, %s", command, da, db)
		}
	})
}
