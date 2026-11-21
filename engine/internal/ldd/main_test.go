package ldd

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain runs the status fixtures from a copy outside this checkout: they
// are plain folders under test/testdata, so git walked up to the repository
// that holds them and its branch name selected a fixture change (a branch
// named like "fresh" picked the cycle fixture's change, T198). Jhon's git
// reads drop inherited GIT_* variables, so a ceiling cannot stop the walk.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "jhon-ldd-status-")
	if err != nil {
		panic(err)
	}
	copied := filepath.Join(dir, "status")
	if err := os.CopyFS(copied, os.DirFS(statusFixtures)); err != nil {
		panic(err)
	}
	statusFixtures, cycle = copied, filepath.Join(copied, "cycle")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
