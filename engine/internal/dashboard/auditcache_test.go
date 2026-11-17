package dashboard

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The audits kept (S14, L31): each with what it read besides the session's
// transcripts, never an audit-error, the newest auditCacheEntries at most,
// and no temporary file left behind by a write that died.

// noDeps reads no input besides the session.
func noDeps(string) string { return "" }

func TestAuditCacheMissesWhenItsInputsMove(t *testing.T) {
	c := AuditCache{Dir: t.TempDir()}
	c.Put("s.jsonl", "--json", "k", "t /r/openspec/changes/c\n", "size 1", "out\n", 1)
	deps := func(want string) func(string) string {
		return func(spec string) string {
			if spec != "t /r/openspec/changes/c\n" {
				t.Errorf("the spec kept = %q", spec)
			}
			return want
		}
	}
	out, code, ok := c.Get("s.jsonl", "--json", "k", deps("size 1"))
	if !ok || out != "out\n" || code != 1 {
		t.Errorf("the same inputs = %q %d %v", out, code, ok)
	}
	if _, _, ok := c.Get("s.jsonl", "--json", "k", deps("size 2")); ok {
		t.Errorf("an input moved and the cache hit")
	}
	if _, _, ok := c.Get("s.jsonl", "--json", "k2", deps("size 1")); ok {
		t.Errorf("another key hit")
	}
}

func TestAuditCacheKeepsNoAuditError(t *testing.T) {
	c := AuditCache{Dir: t.TempDir()}
	c.Put("s.jsonl", "--json", "k", "", "", "error\n", 2)
	if _, _, ok := c.Get("s.jsonl", "--json", "k", noDeps); ok {
		t.Errorf("an audit-error was kept")
	}
	if files, _ := os.ReadDir(c.Dir); len(files) != 0 {
		t.Errorf("files kept: %v", files)
	}
}

func TestAuditCacheRemovesStaleTemporaryFiles(t *testing.T) {
	c := AuditCache{Dir: t.TempDir()}
	stale, fresh := filepath.Join(c.Dir, ".tmp-stale"), filepath.Join(c.Dir, ".tmp-fresh")
	for _, f := range []string{stale, fresh} {
		if err := os.WriteFile(f, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	c.Put("s.jsonl", "--json", "k", "", "", "out\n", 0)
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("a temporary file older than a minute stays: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("a temporary file being written was removed: %v", err)
	}
}

func TestAuditCacheKeepsTheNewestEntries(t *testing.T) {
	c := AuditCache{Dir: t.TempDir()}
	at := time.Now().Add(-time.Hour)
	for i := range auditCacheEntries + 5 {
		target := fmt.Sprintf("s%d.jsonl", i)
		c.Put(target, "--json", "k", "", "", "out\n", 0)
		when := at.Add(time.Duration(i) * time.Second)
		if err := os.Chtimes(c.file(target, "--json"), when, when); err != nil {
			t.Fatal(err)
		}
	}
	c.Put("last.jsonl", "--json", "k", "", "", "out\n", 0)
	files, _ := os.ReadDir(c.Dir)
	if len(files) != auditCacheEntries {
		t.Fatalf("entries kept = %d, want %d", len(files), auditCacheEntries)
	}
	for i, target := range []string{"last.jsonl", fmt.Sprintf("s%d.jsonl", auditCacheEntries+4), "s6.jsonl"} {
		if _, _, ok := c.Get(target, "--json", "k", noDeps); !ok {
			t.Errorf("%d: the newer %s was dropped", i, target)
		}
	}
	for _, target := range []string{"s0.jsonl", "s5.jsonl"} {
		if _, _, ok := c.Get(target, "--json", "k", noDeps); ok {
			t.Errorf("the oldest %s is still kept", target)
		}
	}
}
