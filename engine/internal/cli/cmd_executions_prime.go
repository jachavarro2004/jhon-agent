package cli

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/jachavarro2004/jhon-agent/engine/internal/discover"
	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/vcs"
)

const executionsPrimeUsage = "usage: jhon executions prime [--timeout <minutes>] [--root <project>]"

// runExecutionsPrime builds the pytest-testmon records a run narrows with
// (T133): `jhon executions prime [--timeout <minutes>]` runs, one after
// another, the whole test command of each uv-run pytest project whose record
// is missing or holds no fingerprints, collecting it (execution.Collect), a
// first run its deadline cut included, which `jhon executions run` does not
// try again. Its deadline is the first-run deadline (execution.FirstRunMinutes)
// unless --timeout names another (T255, L1725). A run whose failures are the
// test environment's (execution.Watch.Refusal) keeps no record. It belongs to
// no change and writes no execution record. Exit 0 when every missing record
// was built or none was missing, 1 otherwise, 2 on a usage error, 128 + the
// signal when interrupted.
func runExecutionsPrime(argv []string) (int, error) {
	var rootArg, timeoutArg *string
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a != "--timeout" && a != "--root" || i+1 >= len(argv) || strings.HasPrefix(argv[i+1], "-") && a != "--timeout" {
			fmt.Fprintln(os.Stderr, executionsPrimeUsage)
			return 2, nil
		}
		i++
		v := argv[i]
		if a == "--root" {
			rootArg = &v
		} else {
			timeoutArg = &v
		}
	}
	minutes, ok := timeoutMinutes(timeoutArg, executionsPrimeUsage)
	if !ok {
		return 2, nil
	}
	if timeoutArg == nil {
		minutes = execution.FirstRunMinutes
	}
	root := discover.FindRoot(cwd())
	if rootArg != nil {
		root = project.Resolve(cwd(), *rootArg)
	}
	resolved, err := discover.ProjectsFor(root)
	if err != nil {
		return 1, err
	}
	top := vcs.GitTopLevel(root)
	type build struct{ project, command, data string }
	var builds []build
	var all []string
	for _, p := range resolved.Projects {
		all = append(all, p.Path)
		if p.Testability != discover.Testable() || p.Command == nil {
			continue
		}
		c, ok := execution.Collect(*p.Command, top, p.Path, execution.PytestCov(filepath.Join(root, p.Path)))
		data := execution.TestmonData(top, p.Path)
		if ok && needsFirstRun(execution.TestmonRecord(data)) {
			builds = append(builds, build{p.Path, c, data})
		}
	}
	if len(builds) == 0 {
		fmt.Println("jhon executions prime: every pytest project has its pytest-testmon record; nothing to build")
		return 0, nil
	}
	configSrc := ""
	if b, err := project.ReadLddConfig(root); err == nil {
		configSrc = b
	}
	groups, _ := exclusiveGroups(configSrc, all)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigs)
	built, timedOut := 0, false
	fmt.Printf("jhon executions prime: each first run's deadline is %s min (--timeout sets another)\n", js.NumberString(minutes))
	for _, b := range builds {
		// A project's exclusive groups hold against a run of its tests too.
		release, ok := execution.LockGroups(root, groups[b.project], nil, func(g string) {
			fmt.Printf("... %s: waiting for executions_exclusive.%s, which another run holds\n", b.project, g)
		})
		if !ok {
			continue
		}
		fmt.Printf("==> %s: %s\n", b.project, b.command)
		execution.StartBuild(b.data)
		watch := execution.NewWatch()
		r := execution.RunOne(b.command, filepath.Join(root, b.project), execution.Run{Stdout: os.Stdout, Deadline: execution.Deadline(minutes), Signals: sigs, Capture: watch})
		why, refused := firstRunEnd(b.data, r, minutes, watch)
		release()
		switch {
		case r.Interrupted != nil:
			sig := r.Interrupted.(syscall.Signal)
			fmt.Fprintf(os.Stderr, "jhon executions prime: interrupted by %s; %d of %d record(s) built\n", js.SignalName(sig), built, len(builds))
			return 128 + int(sig), nil
		case r.StartError != "":
			fmt.Fprintf(os.Stderr, "jhon executions prime: could not start sh for %s: %s\n", b.project, r.StartError)
			return 2, nil
		case r.Code == nil:
			timedOut = true
			got := ""
			if progress := watch.Progress(); progress != "" {
				got = "; " + progress
			}
			fmt.Printf("<== %s: timeout after %s min; its process group was killed and no record was kept%s\n", b.project, js.NumberString(minutes), got)
		case refused != "":
			fmt.Printf("<== %s: exit %d; no record was kept: %s, which points at the test environment, not the change: are the test services (a database, a cache) up?\n", b.project, *r.Code, refused)
		case why == "" && (*r.Code == 0 || *r.Code == 1):
			built++
			fmt.Printf("<== %s: exit %d; its pytest-testmon record is kept\n", b.project, *r.Code)
		default:
			fmt.Printf("<== %s: exit %d; no record was kept\n", b.project, *r.Code)
		}
	}
	again := ""
	if timedOut {
		again = "; run it again with a longer `--timeout <minutes>`"
	}
	fmt.Printf("jhon executions prime: %d of %d record(s) built%s\n", built, len(builds), again)
	if built < len(builds) {
		return 1, nil
	}
	return 0, nil
}
