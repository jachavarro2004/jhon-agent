package vcs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// T268 (L1762): a tool's own data (CodeGraph's index, jhon-studio's output,
// Engram's data) is never project input; a read-only look at the working
// state writes nothing into the repository; and an untracked tree too big
// to hash in a guard's time is named, not hashed.

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// toolFiles is what CodeGraph, jhon-studio and Engram leave in a project,
// at its root and in a sub-project.
var toolFiles = map[string]string{
	".codegraph/codegraph.db":         "index",
	".codegraph/.gitignore":           "*.tmp\n",
	"sub/.codegraph-wsl/codegraph.db": "index",
	".jhon/studio/tour/brief.md":      "# tour\n",
	".engram/engram.db":               "memories",
}

func headTree(t *testing.T, root string) string {
	t.Helper()
	return strings.TrimSpace(git(root, "rev-parse", "HEAD^{tree}").out)
}

func TestToolDataIsNoCandidateInput(t *testing.T) {
	root := repo(t, false)
	writeFiles(t, root, map[string]string{"a.txt": "a\n"})
	gitT(t, root, "add", "-A")
	gitT(t, root, "commit", "-q", "-m", "a")
	writeFiles(t, root, toolFiles)
	if c := CandidateOf(root, nil); c.Clean != true {
		t.Errorf("candidate with tool data only untracked: clean %v, want true", c.Clean)
	}
	if got, want := WorkingTree(root, nil), headTree(t, root); got != want {
		t.Errorf("working tree with tool data only untracked: %s, want HEAD's %s", got, want)
	}
	// Beside a real edit, the edit alone counts.
	writeFiles(t, root, map[string]string{"b.txt": "b\n"})
	if c := CandidateOf(root, nil); c.Clean != false {
		t.Errorf("candidate with b.txt untracked: clean %v, want false", c.Clean)
	}
	tree := WorkingTree(root, nil)
	if paths, ok := TreeDiff(root, headTree(t, root), tree); !ok || !reflect.DeepEqual(paths, []string{"b.txt"}) {
		t.Errorf("tree diff beside tool data: %q %v, want [b.txt]", paths, ok)
	}
}

// objects is every file under root's object directory.
func objects(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(filepath.Join(root, ".git", "objects"), func(p string, e os.DirEntry, err error) error {
		// git's own lock files come and go: a commit starts `git maintenance
		// run --auto` detached, which holds objects/maintenance.lock a moment.
		if err == nil && !e.IsDir() && !strings.HasSuffix(p, ".lock") {
			out = append(out, p)
		}
		return nil
	})
	slices.Sort(out)
	return out
}

func TestReadingWritesNothingIntoTheRepository(t *testing.T) {
	root := repo(t, true)
	writeFiles(t, root, map[string]string{"a.txt": "a\n", "d/b.txt": "b\n"})
	writeFiles(t, root, toolFiles)
	before := objects(t, root)
	r, err := OpenReading(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	scratch := r.dir
	if c := r.Candidate(nil); c.Clean != false || c.Tree != headTree(t, root) {
		t.Errorf("reading's candidate: %+v", c)
	}
	tree, err := r.WorkingTree(nil)
	if err != nil || len(tree) != 40 {
		t.Fatalf("reading's working tree: %q, %v", tree, err)
	}
	if paths, ok := r.TreeDiff(headTree(t, root), tree); !ok || !reflect.DeepEqual(paths, []string{"a.txt", "d/b.txt"}) {
		t.Errorf("reading's tree diff: %q %v", paths, ok)
	}
	r.Close()
	if got := objects(t, root); !reflect.DeepEqual(got, before) {
		t.Errorf("a reading wrote into the repository's objects: %d files before, %d after", len(before), len(got))
	}
	if _, err := os.Stat(scratch); !os.IsNotExist(err) {
		t.Errorf("the reading's private folder %s is still there after Close: %v", scratch, err)
	}
	// The same bytes give the same tree as the one a test run records.
	if want := WorkingTree(root, nil); tree != want {
		t.Errorf("reading's working tree %s, want WorkingTree's %s", tree, want)
	}
}

func TestReadingRefusesAnUntrackedTreeOverItsBudget(t *testing.T) {
	defer func(b Budget) { untrackedBudget = b }(untrackedBudget)
	untrackedBudget = Budget{Files: 4, Bytes: 1 << 20}
	root := repo(t, true)
	writeFiles(t, root, map[string]string{"a.txt": "a\n", "out-1/x": "1", "out-1/y": "2", "out-2/z/w": "3", "src/k.go": "package k\n"})
	gitT(t, root, "add", "src/k.go")
	gitT(t, root, "commit", "-q", "-m", "src")
	writeFiles(t, root, map[string]string{"src/gen/1": "1", "src/gen/2": "2"})
	writeFiles(t, root, toolFiles)
	r, err := OpenReading(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	tree, err := r.WorkingTree(nil)
	var large *LargeError
	if tree != "" || !errors.As(err, &large) {
		t.Fatalf("over budget: tree %q, error %v, want a LargeError", tree, err)
	}
	// Entries by size, then name; tool data is never counted.
	if large.Files != 6 || large.Bytes != 7 || !reflect.DeepEqual(large.Entries, []string{"a.txt", "out-1/", "src/gen/", "out-2/"}) ||
		!reflect.DeepEqual(large.Folders(), []string{"out-1/", "src/gen/", "out-2/"}) {
		t.Errorf("large: %+v", large)
	}
	want := "the working tree holds 6 untracked files (7 B) git does not ignore, over the 4 files a check hashes in its time: a.txt, out-1/, src/gen/, out-2/; when they are build or test output, add them to .gitignore (out-1/, src/gen/, out-2/), else commit them"
	if err.Error() != want {
		t.Errorf("message:\n%s\nwant\n%s", err.Error(), want)
	}
	// Within the budget, the same tree is read.
	untrackedBudget = Budget{Files: 6, Bytes: 1 << 20}
	if tree, err := r.WorkingTree(nil); err != nil || tree != WorkingTree(root, nil) {
		t.Errorf("within budget: %q, %v", tree, err)
	}
	untrackedBudget = Budget{Files: 100, Bytes: 5}
	if _, err := r.WorkingTree(nil); !errors.As(err, &large) || !strings.Contains(err.Error(), "(7 B) git does not ignore, over the 5 B a check hashes in its time") {
		t.Errorf("over the byte budget: %v", err)
	}
}

func TestReadingStopsWithItsContext(t *testing.T) {
	root := repo(t, true)
	writeFiles(t, root, map[string]string{"a.txt": "a\n"})
	ctx, cancel := context.WithCancel(context.Background())
	r, err := OpenReading(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	cancel()
	if tree, err := r.WorkingTree(nil); tree != "" || !errors.Is(err, context.Canceled) {
		t.Errorf("a canceled reading: tree %q, error %v, want context.Canceled", tree, err)
	}
	// Its folder goes with its context, Close or not: a guard past its
	// deadline exits without waiting for it.
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(r.dir); os.IsNotExist(err) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("a canceled reading's folder %s is still there", r.dir)
}

// A reading's folder a killed process left behind is swept by the next
// reading: a jhon-reading-* folder in the temporary folder, this user's,
// older than 10 minutes. A newer one, another name, and a link stay.
func TestOpenReadingSweepsAKilledReadingsFolder(t *testing.T) {
	tmp := dir(t)
	t.Setenv("TMPDIR", tmp)
	old, fresh, other := filepath.Join(tmp, "jhon-reading-1"), filepath.Join(tmp, "jhon-reading-2"), filepath.Join(tmp, "jhon-index-1")
	for _, d := range []string{old, fresh, other} {
		writeFiles(t, d, map[string]string{"objects/ab/cd": "x"})
	}
	link := filepath.Join(tmp, "jhon-reading-3")
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}
	then := time.Now().Add(-11 * time.Minute)
	for _, d := range []string{old, other} {
		if err := os.Chtimes(d, then, then); err != nil {
			t.Fatal(err)
		}
	}
	r, err := OpenReading(context.Background(), repo(t, true))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("the 11-minute-old reading folder is still there: %v", err)
	}
	for _, d := range []string{fresh, other, link} {
		if _, err := os.Lstat(d); err != nil {
			t.Errorf("%s was swept: %v", d, err)
		}
	}
}
