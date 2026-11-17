package guard

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
)

// testRunners are the programs that run tests, and those that run one
// (`uv run pytest`, `npx vitest`, `python -m pytest`); packageManagers run
// a project's `test` script (`npm test`, `pnpm run test`).
var (
	testRunners     = []string{"pytest", "vitest", "jest", "mocha"}
	runnerHosts     = []string{"uv", "poetry", "pipenv", "hatch", "npx", "pnpm", "yarn", "bunx", "python", "python3"}
	packageManagers = []string{"npm", "pnpm", "yarn", "bun"}
)

var byHand, wholeSuite sync.Map

// TestsByHand is whether command runs a suite itself rather than through
// `jhon executions`: a project's tests, a directory of them, or `./...`. A
// focused TDD run (named test files, a `::` node, a `-k`, `-run`, or
// `--lf` filter) is the slice's own work, and an invocation that runs no
// test (`--version`, `-h`, `--co`, `go test -list` or `-c`) is none: the
// audit counts the rest (ChangeLDD.ByHand); route-guard notes only the
// narrower WholeSuiteByHand. A command the shell reader cannot read runs no
// suite it can name.
func TestsByHand(command string) bool {
	if v, ok := byHand.Load(command); ok {
		return v.(bool)
	}
	out := slices.ContainsFunc(testRuns(command), func(r testRun) bool { return !focused(r.own, "") })
	byHand.Store(command, out)
	return out
}

// WholeSuiteByHand is whether command runs a whole project's tests by hand:
// a TestsByHand run with no path operand, or whose only ones name the
// directory it runs in or one above it (`./...`, `.`, `..`, `$(pwd)`), or
// root or a directory above it (`-m unit` alone is still the whole
// project). A package or a directory of tests is focused: route-guard notes
// only this in an active change's project, never refusing it.
func WholeSuiteByHand(command, root string) bool {
	key := command + "\x00" + root
	if v, ok := wholeSuite.Load(key); ok {
		return v.(bool)
	}
	out := slices.ContainsFunc(testRuns(command), func(r testRun) bool {
		return !focused(r.own, root) && !slices.ContainsFunc(paths(r.own, root), func(p string) bool { return !wholeProject(r.pwd(p), root) })
	})
	wholeSuite.Store(key, out)
	return out
}

// A testRun is one program in a command that runs tests: own, its arguments
// after the runner, `go test`, or the `test` script, and whether every
// command substitution in it is `$(pwd)`.
type testRun struct {
	own     []string
	pwdOnly bool
}

var (
	// pwdSubst is the working directory by a command substitution.
	pwdSubst = lazyre.New(`\$\(\s*pwd(?:\s+-[LP])?\s*\)|` + "`\\s*pwd(?:\\s+-[LP])?\\s*`")
	// pwdWord is an operand that starts with the working directory by name.
	pwdWord = lazyre.New(`^(?:\$PWD|\$\{PWD\}|\$\(…\)|` + "`…`" + `)(/|$)`)
)

// pwd is operand p with the working directory it starts with, `$PWD` or a
// `$(pwd)` substitution, written `.`; another substitution stays as it is.
func (r testRun) pwd(p string) string {
	m := pwdWord.FindStringSubmatchIndex(p)
	if m == nil || (!strings.HasPrefix(p, "$PWD") && !strings.HasPrefix(p, "${PWD}") && !r.pwdOnly) {
		return p
	}
	return "." + p[m[2]:]
}

// testRuns are the programs in command that run tests, without those that
// run none (noRun).
func testRuns(command string) []testRun {
	ps, err := sh.JudgeString(command, nil, nil, sh.Opts{})
	if err != nil {
		return nil
	}
	var out []testRun
	for _, p := range ps {
		if len(p.Argv) == 0 || p.Code != nil {
			continue
		}
		args := p.Argv[1:]
		own, ok := runnerArgs(p.Name, args)
		if ok && !noRun(p.Name == "go", own) {
			raw := ""
			if p.Raw != nil {
				raw = pwdSubst.ReplaceAllString(*p.Raw, "")
			}
			out = append(out, testRun{own: own, pwdOnly: !strings.Contains(raw, "$(") && !strings.Contains(raw, "`")})
		}
	}
	return out
}

// runnerArgs are the arguments a test run takes after its runner, and
// whether the program name with args runs tests at all.
func runnerArgs(name string, args []string) ([]string, bool) {
	switch {
	case slices.Contains(testRunners, name):
		return vitestMode(name, args), true
	case slices.Contains(packageManagers, name) && script(args) == "test":
		return after(args, "test"), true
	case slices.Contains(runnerHosts, name):
		if k := slices.IndexFunc(args, func(a string) bool { return slices.Contains(testRunners, a) }); k >= 0 {
			return vitestMode(args[k], args[k+1:]), true
		}
	case name == "go" && at(nonOptions(args), 0) == "test":
		return after(args, "test"), true
	}
	return nil, false
}

// vitestMode drops vitest's `run` or `watch`, a mode and not a path.
func vitestMode(runner string, args []string) []string {
	if runner == "vitest" && len(args) > 0 && (args[0] == "run" || args[0] == "watch") {
		return args[1:]
	}
	return args
}

// after is args past the first word, the subcommand or script.
func after(args []string, word string) []string {
	if k := slices.Index(args, word); k >= 0 {
		return args[k+1:]
	}
	return args
}

// noRuns are the options with which a runner runs no test: it prints its
// version, its help, or the tests it would run; goNoRuns are `go test`'s
// (pytest's `-c` names a config file).
var (
	noRuns   = []string{"--version", "-V", "-h", "--help", "--co", "--collect-only", "--listTests"}
	goNoRuns = []string{"-list", "--list", "-c", "-h", "-help", "--help"}
)

// noRun is whether a test run's own arguments run no test.
func noRun(goTest bool, own []string) bool {
	names := noRuns
	if goTest {
		names = goNoRuns
	}
	return slices.ContainsFunc(own, func(a string) bool {
		name, _, _ := strings.Cut(a, "=")
		return slices.Contains(names, name)
	})
}

// valueOptions are the runners' options whose value is the next argument,
// so it is not a path: `-m unit`, `-count 1`, `--ignore tests/slow`.
var valueOptions = []string{
	"-m", "-p", "-n", "-c", "-o", "-W", "--rootdir", "--maxfail", "--tb", "--timeout", "--durations",
	"--cov", "--cov-report", "--junitxml", "--basetemp", "--ignore", "--deselect", "--confcutdir",
	"-count", "-timeout", "-tags", "-coverprofile", "-covermode", "-coverpkg", "-bench", "-benchtime",
	"-cpu", "-parallel", "-exec", "-shuffle", "-vet", "-skip",
	"--config", "--reporter", "--project", "--shard", "--maxWorkers", "-w", "--environment",
}

// paths are a test run's own path operands: what is neither an option nor
// an option's value. The word after an option valueOptions does not name
// (`--log-level DEBUG`) is its value unless it reads as a path: it has a
// slash, a dot, or a substitution, or names a directory under root.
func paths(own []string, root string) []string {
	var out []string
	for k := 0; k < len(own); k++ {
		a := own[k]
		switch {
		case a == "--":
		case strings.HasPrefix(a, "-"):
			if slices.Contains(valueOptions, a) || (!strings.Contains(a, "=") && k+1 < len(own) && !pathLike(own[k+1], root)) {
				k++
			}
		default:
			out = append(out, a)
		}
	}
	return out
}

// pathLike is whether a word after an option reads as a path operand.
func pathLike(a, root string) bool {
	if strings.HasPrefix(a, "-") || strings.ContainsAny(a, "/.$`") {
		return true
	}
	if root == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(root, a))
	return err == nil && info.IsDir()
}

// wholeProject is whether a path operand names the whole project: the
// directory the run is in or one above it (`./...`, `.`, `..`), or root or
// a directory above it.
func wholeProject(p, root string) bool {
	p = strings.TrimSuffix(strings.TrimSuffix(p, "..."), "/")
	if p == "" {
		return true
	}
	if filepath.IsAbs(p) {
		p, root = filepath.Clean(p), filepath.Clean(root)
		return root != "." && (p == root || strings.HasPrefix(root, strings.TrimSuffix(p, "/")+"/"))
	}
	return !slices.ContainsFunc(strings.Split(filepath.Clean(p), "/"), func(s string) bool { return s != "." && s != ".." })
}

// script is the script a package manager's arguments run: `test` for
// `npm test` and `npm run test`.
func script(args []string) string {
	ops := nonOptions(args)
	if at(ops, 0) == "run" || at(ops, 0) == "run-script" {
		return at(ops, 1)
	}
	return at(ops, 0)
}

var (
	// testFile is an operand naming one test file or one test in it.
	testFile = lazyre.New(`::|\.(?:py|go|[cm]?[jt]sx?)$`)
	// testFilters are the options that pick tests by name or by last result.
	testFilters = []string{"-k", "-run", "--run", "-t", "--testNamePattern", "--lf", "--last-failed", "--ff"}
)

// focused is whether a test run's own arguments pick tests: a file or a
// node among its paths (an option's value, `--deselect a.py::t`, is none),
// or a filter.
func focused(own []string, root string) bool {
	return slices.ContainsFunc(own, func(a string) bool {
		name, _, _ := strings.Cut(a, "=")
		return slices.Contains(testFilters, name)
	}) || slices.ContainsFunc(paths(own, root), testFile.MatchString)
}
