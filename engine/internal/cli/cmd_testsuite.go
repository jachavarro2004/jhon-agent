package cli

import (
	"os/exec"

	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
)

// `jhon test [filter]`: the source's binary built at build/jhon, the Go
// tests (the engine's unit tests and the conformance suite under test/),
// the pi extension's tests, then the rule coverage matrix, which gets the
// arguments. The fast tests run first: they need no
// clean tree, and a renderer that fails them makes the matrix meaningless.
// Any failing fails; the exit code is the matrix's.
func init() { register("test", runTest) }

// goTestArgs are the Go tests' arguments: their own timeout, as the
// sandbox suite (test/sandbox) alone takes 8 to 10 minutes on a loaded
// machine, past go's default 10, which would fail with no test at fault
// (T250).
var goTestArgs = []string{"test", "-timeout", "30m", "./..."}

func runTest(argv []string) (int, error) {
	r, o := release.Locate(), out()
	if code, ok := r.NeedRepo(o, "test"); !ok {
		return code, nil
	}
	if code, ok := release.NeedNode(o); !ok {
		return code, nil
	}
	build := exec.Command("go", "build", "-o", "build/jhon", "./engine/cmd/jhon")
	build.Dir = r.Source
	if release.Run(build) != 0 {
		return o.Die("the binary does not build"), nil
	}
	gotest := exec.Command("go", goTestArgs...)
	gotest.Dir = r.Source
	if release.Run(gotest) != 0 {
		return o.Die("Go tests failed"), nil
	}
	// jhon-shell is TypeScript, as pi loads it; its tests import pi's packages,
	// its devDependencies, which npm installs from the lockfile when missing.
	if !release.IsDir(r.Source + "/shell/node_modules") {
		ci := exec.Command("npm", "ci", "--no-audit", "--no-fund")
		ci.Dir = r.Source + "/shell"
		if release.Run(ci) != 0 {
			return o.Die("cannot install jhon-shell's devDependencies (npm ci in shell/)"), nil
		}
	}
	// Its types are checked strictly (shell/tsconfig.json) before its tests run.
	tsc := exec.Command("npx", "--no-install", "tsc", "-p", ".")
	tsc.Dir = r.Source + "/shell"
	if release.Run(tsc) != 0 {
		return o.Die("jhon-shell does not type-check (tsc -p shell)"), nil
	}
	// node expands the glob.
	if release.Run(exec.Command("node", "--experimental-strip-types", "--test", r.Source+"/shell/test/*.test.ts")) != 0 {
		return o.Die("jhon-shell tests failed"), nil
	}
	if release.Run(exec.Command("node", "--test", r.Source+"/dashboard/test/*.test.mjs")) != 0 {
		return o.Die("the dashboard component's tests failed"), nil
	}
	coverage := exec.Command("go", append([]string{"run", "./tools/coverage"}, argv...)...)
	coverage.Dir = r.Source
	return release.Run(coverage), nil
}
