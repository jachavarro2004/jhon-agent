package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// tree is every path under dir, to prove a command wrote nothing there.
func tree(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	if err := filepath.Walk(dir, func(p string, _ os.FileInfo, err error) error {
		out = append(out, p)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// --plan prints the matrix the owner chose (L482, L483) and an estimate
// from the bench's own records, read only, and runs nothing.
func TestCertifyPlanPrintsTheMatrixAndItsEstimate(t *testing.T) {
	f := newFake(t)
	results := t.TempDir()
	// One opus batch: 4 runs of 1,000 weighted tokens and 60 s each, the
	// weekly utilization 40 % → 42 %: 0.0005 points per weighted token.
	for i, u := range []float64{0.40, 0.41, 0.42, -1} {
		m := Metrics{Task: "late-fee", Variant: "plain", Condition: []string{"jhon", "pi-jhon"}[i%2], Repeat: i + 1, Model: "opus", InputTokens: 1000, WallMs: 60000}
		if u >= 0 {
			m.Utilization = f64(u)
		}
		putRuns(t, filepath.Join(results, "20261110T000000Z"), m)
	}
	before := tree(t, results)
	code, out, errOut := certify(t, nil, "--plan", "--results-root", results, "--baseline-ref", "752cb382", "--candidate-ref", "abcdef12")
	if code != 0 {
		t.Fatalf("exit %d\n%s%s", code, out, errOut)
	}
	for _, s := range []string{
		"short tasks: account-import, contacts-migration, late-fee, pagination-bug, search-accents, shipping-rules",
		"long tasks: invoice-slices, slow-suite, two-modules, workspace-budgets",
		"conditions: jhon, pi-jhon", "variants: plain, ldd, ldd-native, ldd-ask", "repeats: 3",
		"baseline of the installed build: 120 runs\n  $ go run ./tools/bench prepare --ref 752cb382\n  $ go run ./tools/bench login-check\n" +
			"  $ go run ./tools/bench run --conditions jhon,pi-jhon --variants plain,ldd --repeats 3 --model opus --expect-commit 752cb382 --results " + filepath.Join(results, "baseline-752cb382") + "\n",
		"≈ 60 quota points", "≈ 2h0m0s",
		"candidate: 240 runs\n  $ go run ./tools/bench prepare --ref abcdef12\n  $ go run ./tools/bench login-check\n" +
			"  $ go run ./tools/bench run --conditions jhon,pi-jhon --variants plain,ldd,ldd-native,ldd-ask --repeats 3 --model opus --expect-commit abcdef12 --results " + filepath.Join(results, "candidate-abcdef12") + "\n",
		"≈ 120 quota points", "≈ 4h0m0s",
		"$ go run ./tools/bench certify --baseline " + filepath.Join(results, "baseline-752cb382") + " --candidate " + filepath.Join(results, "candidate-abcdef12") + " --gates <checkout>",
		"method:", "1 batch", "20261110T000000Z",
		"runs nothing",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("plan lacks %q:\n%s", s, out)
		}
	}
	if after := tree(t, results); !slices.Equal(before, after) {
		t.Errorf("the plan wrote under the results: %v", after)
	}
	if calls := f.calls(t); len(calls) != 0 {
		t.Errorf("the plan ran %v", calls)
	}
}

func TestCertifyPlanWithNoRecordsSaysSo(t *testing.T) {
	code, out, _ := certify(t, nil, "--plan", "--results-root", filepath.Join(t.TempDir(), "absent"), "--baseline-ref", "752cb382", "--candidate-ref", "abcdef12")
	if code != 0 || !strings.Contains(out, "candidate: 240 runs") || !strings.Contains(out, "no estimate: no runs with a weekly reading under") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}
