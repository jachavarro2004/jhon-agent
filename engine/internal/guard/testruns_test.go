package guard

import "testing"

// The audit's TestTestsByHand cases, against the rule route-guard and the
// audit both read (testruns.go).
func TestTestsByHand(t *testing.T) {
	for c, want := range map[string]bool{
		"cd x && timeout 900 uv run --extra dev pytest tests/integration -m \"not migration\" -q": true,
		"uv run pytest":  true,
		"go test ./...":  true,
		"npx vitest run": true,
		// A focused TDD run: named files, a node, a filter.
		"cd x && timeout 600 uv run --extra dev pytest tests/integration/test_flow_schedules_a.py -q": false,
		"cd x && timeout 1500 uv run --extra dev pytest -q tests/a.py":                                false,
		"pytest tests/test_a.py::test_b":                                                              false,
		"python3 -m pytest -q --lf":                                                                   false,
		"pytest -k x":                                                                                 false,
		"go test ./internal/audit/ -run TestX":                                                        false,
		"npx vitest run src/a.test.ts":                                                                false,
		"jhon executions run c1 --affected origin/main 2>&1 | rg pass":                                false,
		"uv run ruff check src":                                                                       false,
		"rg -n pytest pyproject.toml":                                                                 false,
		"git commit -m 'run pytest'":                                                                  false,
		// The suite route-guard refuses (S8), and what it never does.
		"uv run pytest -m unit":                  true,
		"npm test":                               true,
		"pnpm run test":                          true,
		"npm test -- src/a.test.ts":              false,
		"npm run build":                          false,
		"jhon executions run c1 --affected main": false,
		"echo 'pytest' | cat":                    false,
		"pytest tests/test_a.py":                 false,
	} {
		if got := TestsByHand(c); got != want {
			t.Errorf("TestsByHand(%q) = %v, want %v", c, got, want)
		}
	}
}

// The whole-project run route-guard refuses (L12): a package or a directory
// passes, and an invocation that runs no test is never a suite.
func TestWholeSuiteByHand(t *testing.T) {
	for c, want := range map[string]bool{
		"go test ./engine/internal/guard ./engine/internal/hook -count=1": false,
		"go test ./pkg":                      false,
		"pytest tests/unit":                  false,
		"cd x && pytest tests/":              false,
		"uv run pytest tests/integration -q": false,
		"pytest --version":                   false,
		"python -m pytest -h":                false,
		"pytest --co -q":                     false,
		"pytest --collect-only":              false,
		"pytest -V":                          false,
		"npx jest --help":                    false,
		"npx jest --listTests":               false,
		"go test -list . ./...":              false,
		"go test -c ./pkg":                   false,
		"go test -c":                         false,
		"pytest tests/test_a.py::test_b":     false,
		"pytest -k x":                        false,
		"go test ./...":                      true,
		"go test":                            true,
		"go test . -count=1":                 true,
		"pytest":                             true,
		"pytest .":                           true,
		"pytest /p/proj":                     true,
		"uv run pytest -m unit":              true,
		"cd x && timeout 900 uv run --extra dev pytest -m \"not migration\" -q": true,
		"npm test":       true,
		"pnpm run test":  true,
		"npx vitest run": true,
	} {
		if got := WholeSuiteByHand(c, "/p/proj"); got != want {
			t.Errorf("WholeSuiteByHand(%q) = %v, want %v", c, got, want)
		}
	}
}

// The whole-suite forms T25 adds (L83): the working directory by name, a
// directory at or above the project, and an option whose value the reader
// does not know; the focused runs beside them stay focused.
func TestWholeSuiteByHandMoreForms(t *testing.T) {
	for c, want := range map[string]bool{
		"pytest $(pwd)":                             true,
		"pytest \"$(pwd)\"":                         true,
		"pytest $PWD":                               true,
		"pytest ..":                                 true,
		"pytest ../..":                              true,
		"pytest /p":                                 true,
		"cd sub && pytest .":                        true,
		"pytest --maxfail 3":                        true,
		"pytest -p no:cacheprovider":                true,
		"pytest --tb short":                         true,
		"pytest --durations-min 1":                  true,
		"pytest --log-level DEBUG":                  true,
		"pytest -x --log-level DEBUG -q":            true,
		"pytest tests/test_x.py":                    false,
		"pytest -k x":                               false,
		"pytest tests/test_x.py::test_y":            false,
		"cd sub && pytest tests/x.py":               false,
		"pytest --log-level DEBUG tests/unit":       false,
		"pytest /p/other":                           false,
		"pytest --deselect tests/test_x.py::test_y": true,
		"pytest $(pwd)/tests":                       false,
	} {
		if got := WholeSuiteByHand(c, "/p/proj"); got != want {
			t.Errorf("WholeSuiteByHand(%q) = %v, want %v", c, got, want)
		}
	}
}

// The audit's count keeps a directory run, and drops what runs no test.
func TestTestsByHandRunsNoTest(t *testing.T) {
	for c, want := range map[string]bool{
		"pytest tests/unit":     true,
		"go test ./pkg":         true,
		"pytest --version":      false,
		"pytest -V":             false,
		"python -m pytest -h":   false,
		"pytest --help":         false,
		"pytest --co -q":        false,
		"go test -list . ./...": false,
		"go test -c ./pkg":      false,
		"npx jest --listTests":  false,
	} {
		if got := TestsByHand(c); got != want {
			t.Errorf("TestsByHand(%q) = %v, want %v", c, got, want)
		}
	}
}
