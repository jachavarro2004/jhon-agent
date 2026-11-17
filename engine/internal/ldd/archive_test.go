package ldd

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestDiffTrees(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	for _, root := range []string{a, b} {
		os.MkdirAll(filepath.Join(root, "specs", "auth"), 0o777)
		os.WriteFile(filepath.Join(root, "proposal.md"), []byte("# P\n"), 0o644)
		os.WriteFile(filepath.Join(root, "specs", "auth", "spec.md"), []byte("# A\n"), 0o644)
	}
	if d := diffTrees(a, b); len(d) != 0 {
		t.Fatalf("equal trees differ: %q", d)
	}
	os.WriteFile(filepath.Join(b, "specs", "auth", "spec.md"), []byte("# changed\n"), 0o644)
	os.WriteFile(filepath.Join(a, "tasks.md"), []byte("- [ ] x\n"), 0o644)
	d := diffTrees(a, b)
	want := []string{"Files " + filepath.Join(a, "specs", "auth", "spec.md") + " and " + filepath.Join(b, "specs", "auth", "spec.md") + " differ", "Only in " + a + ": tasks.md"}
	if !slices.Equal(d, want) {
		t.Errorf("diffTrees = %q, want %q", d, want)
	}
}

func TestCopyTreeKeepsContentModeAndLinks(t *testing.T) {
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "copy")
	os.MkdirAll(filepath.Join(src, "d"), 0o777)
	os.WriteFile(filepath.Join(src, "d", "run.sh"), []byte("#!/bin/sh\n"), 0o755)
	os.Symlink("d/run.sh", filepath.Join(src, "link"))
	if err := copyTree(src, dst); err != nil {
		t.Fatal(err)
	}
	if d := diffTrees(src, dst); len(d) != 0 {
		t.Errorf("the copy differs: %q", d)
	}
	if fi, _ := os.Stat(filepath.Join(dst, "d", "run.sh")); fi == nil || fi.Mode().Perm() != 0o755 {
		t.Errorf("mode not kept: %v", fi)
	}
}
