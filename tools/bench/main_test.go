package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunDryRunDefaultsToTheBenchRootAndAllConditions(t *testing.T) {
	newFake(t)
	scratch := t.TempDir()
	t.Setenv("HOME", scratch)
	var out, errOut bytes.Buffer
	code := cli([]string{"run", "--tasks", "pagination-bug", "--repeats", "2", "--model", "haiku", "--dry-run"}, &out, &errOut, time.Date(2026, 11, 9, 8, 30, 0, 0, time.UTC))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	root := filepath.Join(scratch, ".local", "share", "jhon-bench")
	for _, want := range []string{"plan: 10 runs", "budget: 10 points", filepath.Join(root, "results", "20261109T083000Z"), "HOME=" + filepath.Join(root, "home"),
		"pagination-bug-plain-jhon-1", "pagination-bug-plain-clean-2", "pagination-bug-plain-pi-jhon-2", "--model haiku"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("dry run lacks %q:\n%s", want, out.String())
		}
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Errorf("the dry run created %s", root)
	}
}

func TestRunRefusesAMissingBenchHomeAndBadFlags(t *testing.T) {
	newFake(t)
	home := filepath.Join(t.TempDir(), "absent")
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"run", "--home", home, "--results", t.TempDir()}, "run `bench prepare` first"},
		{[]string{"run", "--conditions", "jhon,other", "--dry-run"}, `unknown condition "other"`},
		{[]string{"run", "--tasks", "absent", "--dry-run"}, "task absent"},
		{[]string{"other"}, "usage: bench"},
	} {
		var out, errOut bytes.Buffer
		if code := cli(c.args, &out, &errOut, time.Now()); code == 0 || !strings.Contains(errOut.String(), c.want) {
			t.Errorf("%v: exit %d, stderr %q", c.args, code, errOut.String())
		}
	}
}

func TestReportCommandPrintsTheTable(t *testing.T) {
	dir := t.TempDir()
	writeMetrics(t, dir, Metrics{Task: "shipping-rules", Condition: "clean", Repeat: 1, Success: true, Turns: 4})
	var out, errOut bytes.Buffer
	if code := cli([]string{"report", dir}, &out, &errOut, time.Now()); code != 0 || !strings.Contains(out.String(), "shipping-rules  plain    clean") {
		t.Fatalf("exit %d:\n%s%s", code, out.String(), errOut.String())
	}
}
