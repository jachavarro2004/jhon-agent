package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const renderOutput = `=== RUN   TestFixedContextBytes
    fixedcontext_test.go:23: pi: bundle 9000 + orchestrator 9500 = 18500 (fix/t145b 32661, 56.6%)
    fixedcontext_test.go:23: claude-code: bundle 9100 + orchestrator 9600 = 18700 (fix/t145b 32879, 56.9%)
--- PASS: TestFixedContextBytes (0.10s)
=== RUN   TestLeanTextFitsBothRuntimes
    lean_test.go:55: claude-code: lean 4980 bytes, full 30000 bytes
    lean_test.go:55: pi: lean 4990 bytes, full 30100 bytes
    lean_test.go:69: claude-code: definition body 5100 bytes; pi: jhon-lean.md 5000 bytes
--- PASS: TestLeanTextFitsBothRuntimes (0.20s)
`

const fixedSrc = "\t\tif limit := before * 70 / 100; b+o > limit {\n"
const leanSrc = "const leanLimit = 5120\n"

func TestParseBudgets(t *testing.T) {
	got, err := parseBudgets(renderOutput, fixedSrc, leanSrc)
	if err != nil {
		t.Fatal(err)
	}
	want := Budgets{
		"fixed context claude-code": {18700, 23015},
		"fixed context pi":          {18500, 22862},
		"lean text claude-code":     {4980, 5120},
		"lean text pi":              {4990, 5120},
	}
	if len(got) != len(want) {
		t.Errorf("budgets %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %+v, want %+v", k, got[k], v)
		}
	}
}

func TestParseBudgetsFailsOnAMissingValueOrLimit(t *testing.T) {
	noPi := strings.Replace(renderOutput, "pi: lean 4990 bytes", "pi: something else", 1)
	if _, err := parseBudgets(noPi, fixedSrc, leanSrc); err == nil || !strings.Contains(err.Error(), "lean text pi") {
		t.Errorf("a missing lean line: err %v", err)
	}
	if _, err := parseBudgets(renderOutput, fixedSrc, "const other = 1\n"); err == nil {
		t.Error("a lean source with no leanLimit gave no error")
	}
	if _, err := parseBudgets(renderOutput, "b+o > 100", leanSrc); err == nil {
		t.Error("a fixed-context source with no percentage gave no error")
	}
}

// The repository's own budget tests still say their limits the way the
// tool reads them.
func TestTheRepositoryTestsHoldTheLimitsTheToolReads(t *testing.T) {
	render := filepath.Join("..", "..", "engine", "internal", "render")
	f, err := os.ReadFile(filepath.Join(render, "fixedcontext_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	l, err := os.ReadFile(filepath.Join(render, "lean_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	pct, lean, err := budgetLimits(string(f), string(l))
	if err != nil || pct <= 0 || pct > 100 || lean <= 0 {
		t.Errorf("budgetLimits = %d%%, %d, %v", pct, lean, err)
	}
}
