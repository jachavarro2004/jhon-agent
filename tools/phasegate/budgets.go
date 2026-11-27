package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

// renderDir is the renderer, whose own tests hold the fixed texts to their
// budgets: TestFixedContextBytes and TestLeanTextFitsBothRuntimes.
var renderDir = filepath.Join("engine", "internal", "render")

const (
	// budgetTests are the two tests run; their logs give the values.
	budgetTests = "^(TestFixedContextBytes|TestLeanTextFitsBothRuntimes)$"
	// budgetTimeout bounds their run, compile included.
	budgetTimeout = 10 * time.Minute
)

var (
	// fixedLog is TestFixedContextBytes's log line per runtime.
	fixedLog = regexp.MustCompile(`(?m)\b(claude-code|pi): bundle \d+ \+ orchestrator \d+ = (\d+) \(fix/t145b (\d+),`)
	// leanLog is TestLeanTextFitsBothRuntimes's log line per runtime.
	leanLog = regexp.MustCompile(`(?m)\b(claude-code|pi): lean (\d+) bytes, full \d+ bytes`)
	// fixedPct is the share of fix/t145b TestFixedContextBytes allows.
	fixedPct = regexp.MustCompile(`before \* (\d+) / 100`)
	// leanConst is lean_test.go's ceiling.
	leanConst = regexp.MustCompile(`(?m)^const leanLimit = (\d+)`)
)

// measureBudgets runs the renderer's two budget tests in the repository
// repo, verbosely, and reads each text's bytes from their logs and each
// limit from their source. skip says why there are none.
func measureBudgets(repo string, env []string) (b Budgets, skip string) {
	dir := filepath.Join(repo, renderDir)
	fixed, err1 := os.ReadFile(filepath.Join(dir, "fixedcontext_test.go"))
	lean, err2 := os.ReadFile(filepath.Join(dir, "lean_test.go"))
	if err1 != nil || err2 != nil {
		return nil, "the repository has no renderer budget tests (engine/internal/render)"
	}
	r := timed(repo, env, nil, budgetTimeout, "nice", "-n", "19", "go", "test", "-run", budgetTests, "-v", "./"+filepath.ToSlash(renderDir))
	if r.timedOut {
		return nil, "the renderer's budget tests did not end in " + budgetTimeout.String()
	}
	b, err := parseBudgets(r.stdout, string(fixed), string(lean))
	if err != nil {
		return nil, fmt.Sprintf("the renderer's budget tests (exit %d) gave no reading: %v", r.code, err)
	}
	return b, ""
}

// parseBudgets reads the fixed context and the lean text of both runtimes
// from the tests' verbose output, and their limits from the tests' source:
// the fixed context's is fix/t145b's bytes times the test's percentage.
func parseBudgets(testOutput, fixedSrc, leanSrc string) (Budgets, error) {
	pct, leanLimit, err := budgetLimits(fixedSrc, leanSrc)
	if err != nil {
		return nil, err
	}
	b := Budgets{}
	for _, m := range fixedLog.FindAllStringSubmatch(testOutput, -1) {
		value, _ := strconv.Atoi(m[2])
		before, _ := strconv.Atoi(m[3])
		b["fixed context "+m[1]] = Budget{Value: value, Limit: before * pct / 100}
	}
	for _, m := range leanLog.FindAllStringSubmatch(testOutput, -1) {
		value, _ := strconv.Atoi(m[2])
		b["lean text "+m[1]] = Budget{Value: value, Limit: leanLimit}
	}
	for _, k := range []string{"fixed context claude-code", "fixed context pi", "lean text claude-code", "lean text pi"} {
		if _, ok := b[k]; !ok {
			return nil, fmt.Errorf("no %s in the tests' output", k)
		}
	}
	return b, nil
}

// budgetLimits are the fixed context's percentage of fix/t145b and the lean
// text's ceiling, as the tests' source states them.
func budgetLimits(fixedSrc, leanSrc string) (pct, lean int, err error) {
	p, l := fixedPct.FindStringSubmatch(fixedSrc), leanConst.FindStringSubmatch(leanSrc)
	if p == nil {
		return 0, 0, fmt.Errorf("fixedcontext_test.go states no `before * N / 100` limit")
	}
	if l == nil {
		return 0, 0, fmt.Errorf("lean_test.go states no `const leanLimit = N`")
	}
	pct, _ = strconv.Atoi(p[1])
	lean, _ = strconv.Atoi(l[1])
	return pct, lean, nil
}
