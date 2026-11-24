package cli

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/discover"
	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/vcs"
	"github.com/jachavarro2004/jhon-agent/engine/internal/workspace"
)

// `jhon executions run <change> --repo <name> [--timeout <minutes>]
// [--json]` (T57 slice 6, L1315): in a workspace's primary or any of its
// members, run member name's own projects (each testable one's resolved
// test command, then its code checks, as a run of its own does) in the
// member, one after another, and record the run in the primary's record of
// change under `repos.<name>` (execution.WriteRepoRun), after every
// command, complete once the last ends. The member's working tree is read
// before the first command and again after the last; a difference is a
// disturbed run, which `jhon deliver check` in the member refuses. The run
// is whole: no --affected, --dry-run or --jobs, and nothing is reused.
// Exit 0 when every execution passed, 1 otherwise, 2 on a usage error,
// 128 + the signal when interrupted.

// repoSummary is a member's run as --json prints it.
type repoSummary struct {
	Record string `json:"record"`
	Repo   string `json:"repo"`
	execution.RepoRun
}

func runRepoExecutions(root, change, name string, minutes float64, json bool) (int, error) {
	info := workspace.Of(root)
	if info.Role == workspace.None {
		fmt.Fprintf(os.Stderr, "jhon executions: --repo needs a multi-repository workspace, and %s is in none (%s)\n", root, info.Reason)
		return 2, nil
	}
	var member *workspace.Listed
	var names []string
	for i, l := range info.Members {
		names = append(names, l.Name)
		if l.Name == name {
			member = &info.Members[i]
		}
	}
	switch {
	case member == nil:
		fmt.Fprintf(os.Stderr, "jhon executions: the workspace of %s lists no member %s (members: %s)\n", info.Primary, name, strings.Join(names, ", "))
		return 2, nil
	case !member.Joined:
		fmt.Fprintf(os.Stderr, "jhon executions: workspace member %s (%s) has not joined: the owner runs `jhon workspace join %s` in it\n", name, member.Path, info.Primary)
		return 2, nil
	}
	primary := info.Primary
	if !activeChange(primary, change) {
		fmt.Fprintf(os.Stderr, "jhon executions: %s/ does not exist or is not an active change under %s\n", filepath.Join(execution.ChangesOf(primary), change), primary)
		return 2, nil
	}
	lib := member.Path
	resolved, err := discover.ProjectsFor(lib)
	if err != nil {
		return 1, err
	}
	type step struct{ project, kind, command string }
	var steps []step
	for _, p := range resolved.Projects {
		if p.Testability == discover.Testable() && p.Command != nil {
			steps = append(steps, step{p.Path, "", *p.Command})
		}
		for _, c := range p.Checks {
			steps = append(steps, step{p.Path, c.Kind, c.Command})
		}
	}
	label := func(s step) string {
		if s.kind == "" {
			return s.project
		}
		return s.project + " (" + s.kind + ")"
	}
	say := func(line string) {
		if json {
			fmt.Fprintln(os.Stderr, line)
		} else {
			fmt.Println(line)
		}
	}
	stdout := os.Stdout
	if json {
		stdout = os.Stderr
	}
	shown := execution.RecordShown(primary, change)
	if root != primary {
		shown = execution.RecordFile(primary, change)
	}
	exempt := execution.ChangeRecordPaths(lib, change)
	run := execution.RepoRun{WorkingTree: vcs.WorkingTree(lib, exempt), Executions: []execution.Execution{},
		History: execution.RepoHistoryOf(primary, change, name)}
	write := func() error { return execution.WriteRepoRun(primary, change, name, run) }
	if err := write(); err != nil {
		return 1, err
	}
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigs)
	for _, s := range steps {
		say(fmt.Sprintf("==> %s: %s", label(s), s.command))
		started := time.Now()
		tail := &execution.Tail{}
		r := execution.RunOne(s.command, filepath.Join(lib, s.project), execution.Run{Stdout: stdout, Deadline: execution.Deadline(minutes), Signals: sigs, Capture: tail})
		if r.Interrupted != nil {
			sig := r.Interrupted.(syscall.Signal)
			fmt.Fprintf(os.Stderr, "jhon executions: interrupted by %s; the partial record %s (repos.%s) holds %d execution(s)\n", js.SignalName(sig), shown, name, len(run.Executions))
			return 128 + int(sig), nil
		}
		if r.StartError != "" {
			fmt.Fprintf(os.Stderr, "jhon executions: could not start sh for %s: %s; the record holds the executions before it\n", label(s), r.StartError)
			return 2, nil
		}
		status := execution.Timeout
		switch {
		case r.Code == nil:
			say(fmt.Sprintf("<== %s: timeout after %s min; its process group was killed", label(s), js.NumberString(minutes)))
		case *r.Code == 0:
			status = execution.Pass
		default:
			status = execution.Fail
		}
		if r.Code != nil {
			say(fmt.Sprintf("<== %s: exit %d", label(s), *r.Code))
		}
		e := execution.Execution{Project: s.project, Kind: s.kind, Command: s.command, Cwd: s.project,
			ExitCode: r.Code, Status: status, DurationMs: time.Since(started).Milliseconds()}
		if status != execution.Pass {
			e.OutputTail = tail.String()
			e.FailedTests = execution.FailedTests(e.OutputTail)
		}
		run.Executions = append(run.Executions, e)
		if err := write(); err != nil {
			return 1, err
		}
	}
	run.AfterWorkingTree = vcs.WorkingTree(lib, exempt)
	run.Complete = true
	run.Timestamp = now()
	passed := 0
	for _, e := range run.Executions {
		if e.Status == execution.Pass {
			passed++
		}
	}
	run.Passed = len(run.Executions) > 0 && passed == len(run.Executions)
	if err := write(); err != nil {
		return 1, err
	}
	reportRecord(execution.RecordFile(primary, change))
	if json {
		if err := printJSON(repoSummary{shown, name, run}); err != nil {
			return 1, err
		}
	} else {
		lines := []string{"", "record     " + shown + ", repos." + name, "repo       " + name + "  (" + lib + ")", "tree       " + run.WorkingTree}
		if run.Dirty() {
			lines = append(lines, "dirty      the working tree changed during the run (after: "+run.AfterWorkingTree+")")
		}
		for _, e := range run.Executions {
			lines = append(lines, js.PadEnd(e.Status, 8)+"   "+label(step{e.Project, e.Kind, e.Command}))
		}
		if len(run.Executions) == 0 {
			lines = append(lines, "note       no testable project in "+lib+"; the entry holds no execution")
		}
		fmt.Println(strings.Join(lines, "\n"))
	}
	result, code := "pass", 0
	if passed < len(run.Executions) {
		result, code = "fail", 1
	}
	say(fmt.Sprintf("jhon executions: %s, exit %d (%d of %d passed; record %s, repos.%s)", result, code, passed, len(run.Executions), shown, name))
	return code, nil
}
