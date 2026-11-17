package support

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// The process bound is JHON_TEST_PROCS when a positive number, else a
// quarter of the CPUs, at least 2 (L677).
func TestProcLimit(t *testing.T) {
	for _, c := range []struct {
		env  string
		cpus int
		want int
	}{{"", 16, 4}, {"", 4, 2}, {"", 1, 2}, {"6", 16, 6}, {" 1 ", 16, 1}, {"0", 16, 4}, {"-3", 16, 4}, {"many", 32, 8}} {
		if got := procLimit(c.env, c.cpus); got != c.want {
			t.Errorf("procLimit(%q, %d) = %d, want %d", c.env, c.cpus, got, c.want)
		}
	}
}

// Run starts no more than cap(procs) processes at once: twice as many
// sleeps as slots take at least two rounds.
func TestRunBoundsConcurrentProcesses(t *testing.T) {
	const nap = 300 * time.Millisecond
	n := 2 * cap(procs)
	start := time.Now()
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r := Run(t, Cmd{Args: []string{"sleep", "0.3"}}); r.Code != 0 {
				t.Errorf("sleep: %+v", r)
			}
		}()
	}
	wg.Wait()
	if d := time.Since(start); d < 2*nap {
		t.Errorf("%d sleeps of %v with %d slots took %v, want at least %v", n, nap, cap(procs), d, 2*nap)
	}
}

// A change to a vendored module under third_party/ is a change of the
// binary's source: the build key changes with it.
func TestTheBinaryKeyCoversThirdParty(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("engine/main.go", "package main\n")
	write("go.mod", "module x\n")
	write("third_party/bubbletea/tea.go", "package tea\n")
	before := inputsHash(root)
	write("third_party/bubbletea/tea.go", "package tea // changed\n")
	if after := inputsHash(root); after == before {
		t.Fatalf("the key %s did not change with third_party/", after)
	}
}
