package cli

import (
	"cmp"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"io"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	"github.com/jachavarro2004/jhon-agent/engine/internal/discover"
	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
	"github.com/jachavarro2004/jhon-agent/engine/internal/isodate"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/telemetry"
	"github.com/jachavarro2004/jhon-agent/engine/internal/vcs"
)

// `jhon executions run [<change>] [--affected [<base>]] [--timeout <minutes>]
// [--dry-run] [--json] [--root <project>]`: with no change named, the one
// `jhon status` selects (its first line says which); run every
// selected project in R (the resolved projects whose testability is
// `testable`) through `sh -c` in its directory, projects in parallel up to
// --jobs, and record each exit code (internal/execution).
//
// The commands' output streams to the terminal (to stderr with --json, so
// stdout stays one JSON document) and is not captured. Scope is every
// project in R (`full`), or with --affected the projects changed since the
// base, the projects whose test modules name a changed file's path (not its
// bare name) or a changed document, plus their dependents; with no base, a
// unit's run, since the change's last passing run (execution.LastPassOf),
// else the commit its slice in progress started from (ldd.SliceBase).
// stdin is closed,
// so a command that waits for input fails instead of hanging. --dry-run shows what would run and runs and
// writes nothing. Exit 0 when every execution passed, 1 otherwise, 2 on a
// usage error or a run that could not start, 128 + the signal when interrupted.
//
// A completed run's last line (on stdout; on stderr with --json) is its status:
// `jhon executions: <pass|fail>, exit <code> (<n> of <m> passed; record <path>)`,
// so a pipe through tail, whose exit is tail's, still shows the result. When
// JHON_EXECUTIONS_REPORT names a file, the absolute path of every record the
// run writes is appended to it, for a job runner to read the result from the
// record (the shell's background jobs, lib/background-tasks.ts).
//
// Configuration in openspec/config.yaml: executions_jobs, executions_exclusive,
// executions_inert, executions_always (below), and executions_data_access with
// executions_database_tests (ldd.DataReachNote; a slice commit's check,
// ldd.SliceCommitRefusal): a run whose change's slice touches a data-access
// path, and whose tests have no pytest-testmon record to show what reached it,
// says so in a note.
func init() { register("executions", runExecutions) }

const executionsUsage = "usage: jhon executions run [<change>] [--affected [<base-ref>]] [--timeout <minutes>] [--jobs <n>] [--force] [--dry-run] [--json] [--root <project>] [--repo <name>]"

var changeSep = lazyre.New(`[\\/]`)

// activeChange is whether change names an active change directory under
// root, or the change the branch names with no folder (ldd.FolderlessChange).
func activeChange(root, change string) bool {
	if change == "" || change == "archive" || strings.HasPrefix(change, ".") || changeSep.MatchString(change) {
		return false
	}
	st, err := os.Stat(filepath.Join(root, execution.ChangesOf(root), change))
	return err == nil && st.IsDir() || ldd.FolderlessChange(root) == change
}

// branchsChange is the change `jhon status` selects in root with none
// named, and the line that says which (ok), or the refusal that says none
// is selected and how to name one.
func branchsChange(root string) (change, line string, ok bool) {
	branch := ""
	if out, err := project.ReadGitCmd(root, "branch", "--show-current").Output(); err == nil {
		branch = strings.TrimSpace(string(out))
	}
	s, err := ldd.StatusOf(root, nil)
	if err == nil && s.Change != nil {
		where := ""
		if branch != "" {
			where = " (branch " + branch + ")"
		}
		return *s.Change, "jhon executions: change " + *s.Change + where, true
	}
	// On the default branch, the commands a small change starts with
	// (T146g G1), not a name that branch cannot give.
	if err == nil && s.Command != "" && len(s.Notes) > 0 {
		return "", "jhon executions: no change named, and jhon status selects none on this branch: " + layout.Words(root, s.Notes[0]), false
	}
	why := ""
	if err != nil {
		why = err.Error()
	} else if len(s.Notes) > 0 {
		why = layout.Words(root, s.Notes[0])
	}
	if why != "" {
		why = " (" + why + ")"
	}
	return "", "jhon executions: no change named, and jhon status selects none on this branch" + why + ": name it: jhon executions run <change>", false
}

type planned struct {
	Project string `json:"project"`
	Kind    string `json:"kind,omitempty"`
	Command string `json:"command"`
	Cwd     string `json:"cwd"`
	// Reused is a command the run would not run: it passed on this working tree.
	Reused bool `json:"reused,omitempty"`
	// TimeoutMinutes is the command's own deadline, when it is not the
	// run's: the first-run deadline (execution.FirstRunMinutes).
	TimeoutMinutes float64 `json:"timeout_minutes,omitempty"`
}

type dryRun struct {
	DryRun         bool                 `json:"dry_run"`
	Record         string               `json:"record"`
	Scope          string               `json:"scope"`
	Base           *string              `json:"base"`
	Selection      []execution.Selected `json:"selection"`
	TimeoutMinutes float64              `json:"timeout_minutes"`
	Plan           []planned            `json:"plan"`
	// Projects is each planned project's tests as data, for the shell's
	// change detail (alt+j): how they are narrowed, and how long they should take.
	Projects []projectPlan `json:"projects"`
	Notes    []string      `json:"notes"`
	// Unit and Since are a unit's run (execution.Record.Unit).
	Unit  bool   `json:"unit,omitempty"`
	Since string `json:"since,omitempty"`
}

// projectPlan is one planned project's tests in a dry run. Tests is how they
// are narrowed: testmon (to those whose executed code changed), own (only
// the test modules the change touched, the rest left to the PR's CI),
// related (vitest --related under nx affected), whole, or none (no test
// module of it changed: the PR's CI runs them; a wide run of a project with
// a usable record is testmon then); "" when it has no tests.
type projectPlan struct {
	Project string `json:"project"`
	Tests   string `json:"tests"`
	// Modules is how many test modules an own run names.
	Modules int `json:"modules,omitempty"`
	// Estimate is testmon's reach: tests of total, seconds of all_seconds.
	Estimate *reachJSON `json:"estimate,omitempty"`
	// LastSeconds is how long its tests took in the last recorded run.
	LastSeconds float64  `json:"last_seconds,omitempty"`
	Checks      []string `json:"checks"`
	Reused      bool     `json:"reused,omitempty"`
	// FirstRun is a project with no usable pytest-testmon record, whose
	// tests all run once to build it (T255).
	FirstRun *firstRunJSON `json:"first_run,omitempty"`
	// FailedMarks is how many of its record's tests pytest-testmon marks
	// failed, which it runs again whatever changed.
	FailedMarks *failedMarksJSON `json:"failed_marks,omitempty"`
}

type firstRunJSON struct {
	// Record is the record's state: none or unusable.
	Record string `json:"record"`
	// Tests and Modules are the test functions its test modules define.
	Tests   int `json:"tests"`
	Modules int `json:"modules"`
	// DeadlineMinutes is the first run's deadline, absent when the run names
	// its own (--timeout, timeout_minutes) (T256).
	DeadlineMinutes float64 `json:"deadline_minutes,omitempty"`
}

type failedMarksJSON struct {
	Failed int `json:"failed"`
	Total  int `json:"total"`
}

type reachJSON struct {
	Tests      int     `json:"tests"`
	Total      int     `json:"total"`
	Seconds    float64 `json:"seconds"`
	AllSeconds float64 `json:"all_seconds"`
}

type runSummary struct {
	Record           string                `json:"record"`
	Scope            string                `json:"scope"`
	Base             *string               `json:"base"`
	Executions       []execution.Execution `json:"executions"`
	CandidateCommit  string                `json:"candidate_commit"`
	CandidateTree    string                `json:"candidate_tree"`
	WorkingTreeClean any                   `json:"working_tree_clean"`
	Dirty            bool                  `json:"dirty"`
	Notes            []string              `json:"notes"`
	// RedBeforeGreen is this run's pass after an earlier failing run.
	RedBeforeGreen bool `json:"red_before_green"`
	// Unit and Since are a unit's run (execution.Record.Unit).
	Unit  bool   `json:"unit,omitempty"`
	Since string `json:"since,omitempty"`
}

func printJSON(v any) error {
	s, err := js.Stringify(v)
	if err == nil {
		fmt.Println(s)
	}
	return err
}

// reportRecord appends abs to the file JHON_EXECUTIONS_REPORT names, if any.
func reportRecord(abs string) {
	p := os.Getenv("JHON_EXECUTIONS_REPORT")
	if p == "" {
		return
	}
	if f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		fmt.Fprintln(f, abs)
		f.Close()
	}
}

func now() string { return isodate.Format(time.Now()) }

func runExecutions(argv []string) (int, error) {
	if err := execution.Vocabulary(); err != nil {
		return 1, err
	}
	if len(argv) > 0 && argv[0] == "verify" {
		return runExecutionsVerify(argv[1:])
	}
	if len(argv) > 0 && argv[0] == "prime" {
		return runExecutionsPrime(argv[1:])
	}
	if len(argv) > 0 && argv[0] == "forget" {
		return runExecutionsForget(argv[1:])
	}
	// unit is `--affected` with no base: the last argument, or before an option.
	json, dry, force, unit := false, false, false, false
	var rootArg, base, timeoutArg, jobsArg, repoArg *string
	var rest []string
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		switch a {
		case "--json":
			json = true
		case "--dry-run":
			dry = true
		case "--force":
			force = true
		case "--root", "--affected", "--timeout", "--jobs", "--repo":
			if a == "--affected" && (i+1 >= len(argv) || strings.HasPrefix(argv[i+1], "-")) {
				unit = true
				continue
			}
			i++
			if i >= len(argv) || strings.HasPrefix(argv[i], "-") && a != "--timeout" {
				fmt.Fprintln(os.Stderr, executionsUsage)
				return 2, nil
			}
			v := argv[i]
			switch a {
			case "--root":
				rootArg = &v
			case "--affected":
				base = &v
			case "--jobs":
				jobsArg = &v
			case "--repo":
				repoArg = &v
			default:
				timeoutArg = &v
			}
		default:
			rest = append(rest, a)
		}
	}
	// With no change named, the run takes the change `jhon status` selects
	// on the branch (T146, L1211), a folderless one included.
	named := len(rest) == 2
	if len(rest) < 1 || len(rest) > 2 || rest[0] != "run" || named && strings.HasPrefix(rest[1], "-") {
		fmt.Fprintln(os.Stderr, executionsUsage)
		return 2, nil
	}
	// A named base wins over a bare `--affected` beside it.
	unit = unit && base == nil
	minutes, ok := timeoutMinutes(timeoutArg, executionsUsage)
	if !ok {
		return 2, nil
	}
	root := discover.FindRoot(cwd())
	if rootArg != nil {
		root = project.Resolve(cwd(), *rootArg)
	}
	// A workspace member's run, recorded in the primary's record (T57 slice 6).
	if repoArg != nil {
		switch {
		case !named:
			fmt.Fprintln(os.Stderr, "jhon executions: --repo needs the change named: jhon executions run <change> --repo <name>")
			return 2, nil
		case base != nil || unit || dry || jobsArg != nil:
			fmt.Fprintln(os.Stderr, "jhon executions: --repo runs the member's whole test command: --affected, --dry-run and --jobs are not taken with it")
			return 2, nil
		}
		return runRepoExecutions(root, rest[1], *repoArg, minutes, json)
	}
	// How many projects run at once: --jobs, else the project's
	// executions_jobs (projects that share state, a test database, set 1),
	// else the CPUs, at most 4.
	jobs := min(runtime.NumCPU(), 4)
	configSrc := ""
	if b, err := project.ReadLddConfig(root); err == nil {
		configSrc = b
	}
	if v := contract.Scalar(configSrc, "executions_jobs"); jobsArg == nil && v != "" {
		jobsArg = &v
	}
	if jobsArg != nil {
		n, err := strconv.Atoi(*jobsArg)
		if err != nil || n < 1 {
			fmt.Fprintf(os.Stderr, "jhon executions: --jobs (or executions_jobs) takes a positive whole number, not '%s'\n%s\n", *jobsArg, executionsUsage)
			return 2, nil
		}
		jobs = n
	}
	var change string
	if named {
		change = rest[1]
	} else {
		selected, line, ok := branchsChange(root)
		if !ok {
			fmt.Fprintln(os.Stderr, line)
			return 2, nil
		}
		// The first line says which change runs; with --json, stdout stays
		// one JSON document and the line goes to stderr.
		if json {
			fmt.Fprintln(os.Stderr, line)
		} else {
			fmt.Println(line)
		}
		change = selected
	}
	// A folderless change is named by its branch (ldd.FolderlessChange), so
	// that branch's name, slash and all, is taken as the change's name; a
	// name the branch's change ends with is refused with the name to use.
	folderless, branch := "", ""
	if !activeChange(root, change) {
		folderless, branch = ldd.FolderlessBranch(root)
	}
	if folderless != "" && change == branch {
		change = folderless
	} else if !activeChange(root, change) {
		fmt.Fprintf(os.Stderr, "jhon executions: %s/ does not exist or is not an active change under %s\n", filepath.Join(execution.ChangesOf(root), change), root)
		if folderless != "" && strings.HasSuffix(folderless, "-"+change) {
			fmt.Fprintf(os.Stderr, "jhon executions: branch %s names change %s: run `jhon executions run %s`\n", branch, folderless, folderless)
		}
		// The branch would name a change that has no file of its own yet
		// (T84, L606): say so, and that its failing test starts it.
		if pending, pb := ldd.FolderlessPending(root); pending != "" && (change == pending || change == pb || strings.HasSuffix(pending, "-"+change)) {
			fmt.Fprintln(os.Stderr, "jhon executions: "+ldd.FolderlessPendingNote(root))
		}
		return 2, nil
	}

	resolved, err := discover.ProjectsFor(root)
	if err != nil {
		return 1, err
	}
	// A project runs its test command when it is testable, then its code
	// checks (contracts/ldd.md, "Code checks"); one with neither is
	// not a candidate.
	// build is the pytest-testmon record a first run's command builds, or
	// "", and data the record a pytest-testmon command narrows with, which
	// it runs on a private copy of (execution.CheckOut). long is a run that
	// has to end once for the record to narrow (a first run, or one that
	// runs again the tests the record marks failed): with no --timeout, its
	// deadline is execution.FirstRunMinutes. notes are said before it starts.
	type step struct {
		kind, command, build, data string
		long                       bool
		notes                      []string
	}
	var testable, all []string
	steps := map[string][]step{}
	for _, p := range resolved.Projects {
		all = append(all, p.Path)
		if p.Testability == discover.Testable() && p.Command != nil {
			steps[p.Path] = append(steps[p.Path], step{kind: "", command: *p.Command})
		}
		for _, c := range p.Checks {
			steps[p.Path] = append(steps[p.Path], step{kind: c.Kind, command: c.Command})
		}
		if len(steps[p.Path]) > 0 {
			testable = append(testable, p.Path)
		}
	}
	label := func(p string, s step) string {
		if s.kind == "" {
			return p
		}
		return p + " (" + s.kind + ")"
	}
	scope, scopeReasons := execution.Full, []string{}
	selection := []execution.Selected{}
	for _, p := range testable {
		selection = append(selection, execution.Selected{Project: p, Reason: "full run"})
	}
	// executions_inert: changed files that are no test input.
	inert := contract.InlineList(contract.Scalar(configSrc, "executions_inert"))
	// executions_always: test modules a narrowed run still runs, such as a
	// test that lists every route of a service, which no change touches.
	always := contract.InlineList(contract.Scalar(configSrc, "executions_always"))
	top := vcs.GitTopLevel(root)
	exempt := execution.ChangeRecordPaths(root, change)
	workingTree := vcs.WorkingTree(root, exempt)
	// What an affected run measures from: the base it names, or for a unit's
	// run the change's last passing run, else the change's base. sinceShown
	// names it in a message, and nxBase is the commit `nx affected` takes.
	affected := base != nil || unit
	var changed []string
	since, sinceShown, nxBase := "", "", ""
	if affected {
		var outside []string
		msg := ""
		if unit {
			m, why := unitSince(root, top, change, workingTree)
			changed, outside, msg, since, sinceShown, nxBase = m.files, m.outside, m.errMsg, m.since, m.shown, m.commit
			scopeReasons = append(scopeReasons, why)
		} else {
			changed, outside, msg = execution.ChangedSince(root, *base)
			sinceShown, nxBase = *base, *base
		}
		if msg != "" {
			fmt.Fprintf(os.Stderr, "jhon executions: %s; nothing was run or written\n", msg)
			return 2, nil
		}
		s, err := execution.SelectChanged(root, sinceShown, changed, outside, all, testable, inert)
		if err != nil {
			return 1, err
		}
		scope, selection, scopeReasons = s.Scope, s.Selected, append(scopeReasons, s.Reasons...)
		// A changed shared project that fills the selection with its
		// dependents is said, with the slice's base when the run measured
		// from further back (T253, L1721): never a base the given one
		// descends from, which would select more (L1728).
		for _, span := range dependentSpans(s.Selected) {
			n := fmt.Sprintf("%s, which changed since %s, brings %d projects into the selection only as its dependents (%s)", span.project, sinceShown, len(span.dependents), listShort(span.dependents))
			if base != nil {
				if start, ok := ldd.SliceBase(root, change); ok && !isAncestor(top, start, revParse(top, *base)) {
					n += "; a slice's run measures from the commit the slice started from: `--affected " + start[:min(12, len(start))] + "`"
				}
			}
			scopeReasons = append(scopeReasons, n)
		}
	}
	var plan, precise []string
	// testmon tracks Python files only; any other changed file (data, a
	// lockfile, configuration) would keep every selected project's tests whole.
	// Documentation is not one: the tests that name a changed document run
	// beside testmon's selection (execution.ProseReaders).
	var untracked string
	var vitest []string
	var changedPy, docs []string
	if affected {
		for _, f := range changed {
			if !layout.For(root).Ignored(f) && execution.Prose(f) && !execution.Inert(f, inert) {
				docs = append(docs, f)
			}
		}
		if i := slices.IndexFunc(changed, func(f string) bool {
			return !layout.For(root).Ignored(f) && !strings.HasSuffix(f, ".py") && !execution.Prose(f) && !execution.Inert(f, inert)
		}); i >= 0 {
			untracked = changed[i]
		}
		for _, f := range changed {
			if rel, err := filepath.Rel(top, filepath.Join(root, f)); err == nil && strings.HasSuffix(f, ".py") {
				changedPy = append(changedPy, rel)
			}
		}
	}
	// A wide run is one testmon would not narrow, or would narrow to at least
	// execution.WideShare of the project's test time, or past
	// execution.WideSeconds: about what the PR's CI runs whole anyway. It
	// runs here only the test modules the change touched, and leaves the rest
	// to the CI, whose green checks test the delivery. A narrowed run's
	// estimate is a note, for announcing when it should end.
	estimates := map[string]execution.Reach{}
	// How each project's tests are narrowed, and an own run's module count.
	narrowed, modules := map[string]string{}, map[string]int{}
	// Each pytest project's record state, read once, and what a usable one
	// says the change reaches (execution.TestmonReach).
	states := map[string]string{}
	reaches := map[string]execution.Reach{}
	// firstRuns are the first runs' notes and their test counts.
	firstRuns := map[string]firstRunJSON{}
	firstNotes := map[string]string{}
	// explicit is a run that names its deadline: it binds every command.
	explicit := timeoutArg != nil
	// reads is the file each pytest project's record is read from: the
	// record a run uses (execution.TestmonData, which adopts a worktree's own
	// record from before), or in a dry run, which writes nothing, the one it
	// would read (execution.TestmonReadable) (T256).
	reads := map[string]string{}
	// wide is why the run of p is wide, "" when it is not; byReach is whether
	// its usable record says so, and so can still narrow the touched modules.
	wide := func(p string) (why string, byReach bool) {
		data := execution.TestmonPath(top, p)
		// No record, and the first run that builds one was cut: it is not run
		// again here, where it would be cut again.
		if why, cut := execution.FirstRunCut(data); cut && needsFirstRun(states[p]) {
			return "pytest-testmon has no record, since the first run that builds it " + why + " (`jhon executions prime --timeout <minutes>` builds it with a longer deadline)", false
		}
		if untracked != "" {
			return untracked + " changed, and pytest-testmon tracks Python files only", false
		}
		if r, ok := reaches[p]; ok && r.Total > 0 {
			if r.Wide() {
				return "the change reaches " + r.String(), true
			}
			estimates[p] = r
		}
		return "", false
	}
	// failedOnly makes step i of p, a pytest-testmon run of the record at
	// data, one that runs again the tests the record marks failed when they
	// are all or most of what it selects: it gets the first-run deadline,
	// since pytest-testmon writes its record only at a run's end (T255, L1725).
	failedOnly := func(p string, i int) {
		r := reaches[p]
		if explicit || !r.OnlyFailed() && !r.MostlyFailed() {
			return
		}
		which := "every test pytest-testmon selects is one"
		if !r.OnlyFailed() {
			which = "most tests pytest-testmon selects are ones"
		}
		n := fmt.Sprintf("%s: %s its record marks failed (%d), so it runs under the first-run deadline of %d min: once it ends, the record narrows again", p, which, r.Failed, execution.FirstRunMinutes)
		steps[p][i].long = true
		steps[p][i].notes = append(steps[p][i].notes, n)
		scopeReasons = append(scopeReasons, n)
	}
	for _, p := range testable {
		if !slices.ContainsFunc(selection, func(s execution.Selected) bool { return s.Project == p }) {
			continue
		}
		plan = append(plan, p)
		// An affected run narrows a project's tests to those its change reaches.
		for i, s := range steps[p] {
			if !affected || s.kind != "" {
				continue
			}
			// Nx narrows to the projects its own graph reaches from the change,
			// and Vitest, inside each, to the tests whose imports reach it.
			if c, ok := execution.NxAffected(s.command, nxBase); ok {
				if files, ok := execution.RelatedFiles(root, changed); ok && execution.VitestOnly(filepath.Join(root, p)) {
					c = execution.VitestRelated(c, files)
					vitest = append(vitest, p)
				}
				steps[p][i].command = c
				narrowed[p] = "related"
				continue
			}
			cov := execution.PytestCov(filepath.Join(root, p))
			c, ok := execution.Precise(s.command, top, p, cov)
			if !ok {
				continue
			}
			data, read := execution.TestmonPath(top, p), execution.TestmonReadable(top, p)
			if !dry {
				data = execution.TestmonData(top, p)
				read = data
			}
			reads[p] = read
			states[p] = execution.TestmonRecord(read)
			steps[p][i].data = data
			if states[p] == execution.RecordUsable {
				if r, ok := execution.TestmonReach(read, changedPy); ok {
					reaches[p] = r
					// A record that marks most tests failed runs them all,
					// whatever changed (T255, L1725): said, with its cause and
					// the command that drops it.
					if r.MostlyFailed() {
						n := mostlyFailedNote(p, r)
						steps[p][i].notes = append(steps[p][i].notes, n)
						scopeReasons = append(scopeReasons, n)
					}
				}
			}
			// Narrowing starts with a record: with none, or one that holds no
			// fingerprints, the whole command runs once, collecting it.
			if _, cut := execution.FirstRunCut(data); needsFirstRun(states[p]) && !cut {
				steps[p][i].command, _ = execution.Collect(s.command, top, p, cov)
				steps[p][i].build, steps[p][i].data = data, ""
				steps[p][i].long = !explicit
				firstRuns[p], firstNotes[p] = firstRunOf(root, p, states[p], !explicit)
				steps[p][i].notes = append(steps[p][i].notes, firstNotes[p])
				precise = append(precise, p)
				continue
			}
			readers := execution.ProseReaders(root, p, docs, all)
			if why, byReach := wide(p); why != "" {
				touch := execution.OwnTests(root, p, changed)
				own := slices.Clone(touch)
				for _, r := range readers {
					if !slices.Contains(own, r) {
						own = append(own, r)
					}
				}
				kept := execution.AlwaysTests(root, p, always, own)
				ran := "only the " + strconv.Itoa(len(own)) + " test module(s) the change touched run here"
				if len(kept) > 0 {
					ran = "only the " + strconv.Itoa(len(own)) + " test module(s) the change touched and the " + strconv.Itoa(len(kept)) + " executions_always declares run here"
				}
				// With a usable record whose reach made the run wide, the
				// touched modules it holds tests of run under pytest-testmon,
				// which keeps only their tests it marks affected (T145, L1174
				// E1); a document's readers and executions_always, touched or
				// not (T145c H2), run whole.
				var held []string
				if byReach {
					var unheld []string
					var read bool
					held, unheld, read = heldModules(top, root, p, reads[p], touch, slices.Concat(readers, execution.AlwaysTests(root, p, always, nil)))
					if len(held) > 0 {
						ran += "; pytest-testmon narrows " + strconv.Itoa(len(held)) + " of them to the tests its record marks affected: " + listShort(held)
					}
					switch {
					case len(unheld) > 0 && !read:
						ran += "; the testmon record could not be read, so " + strconv.Itoa(len(unheld)) + " of them " + runs(len(unheld)) + " whole: " + listShort(unheld)
					case len(unheld) > 0:
						ran += "; " + strconv.Itoa(len(unheld)) + " its record holds no test of (new, or unknown to it) " + runs(len(unheld)) + " whole: " + listShort(unheld)
					}
				}
				own = append(own, kept...)
				whole := slices.DeleteFunc(slices.Clone(own), func(m string) bool { return slices.Contains(held, m) })
				steps[p][i].command = execution.OwnTestsCommand(s.command, whole, cov)
				// Both parts run, the narrowed one even when the whole one
				// fails, so the failures of both are reported in one pass
				// (T145c H1).
				if len(held) > 0 {
					narrow := execution.TestmonModulesCommand(c, held)
					if len(whole) > 0 {
						narrow = execution.BothCommand(steps[p][i].command, narrow)
					}
					steps[p][i].command = narrow
				}
				narrowed[p], modules[p] = "own", len(own)
				if len(own) == 0 {
					steps[p][i].command = ""
					ran = "no test module of it changed, so none runs here"
					// A slice's close trusts pytest-testmon (L1011): a wide run
					// with a usable record and no module to run runs what the
					// record holds stale, which may be nothing, and is then an
					// execution on record. A unit's run never runs half the suite.
					if !unit && states[p] == execution.RecordUsable {
						steps[p][i].command = c
						ran = "no test module of it changed, so pytest-testmon runs what its record holds stale"
						narrowed[p], modules[p] = "testmon", 0
						precise = append(precise, p)
						failedOnly(p, i)
					}
				}
				scopeReasons = append(scopeReasons, fmt.Sprintf("%s: %s; %s, and the rest is left to the PR's CI, whose green checks test the delivery", p, why, ran))
				continue
			}
			// The readers run first: testmon's run may select no test at all.
			if len(readers) > 0 {
				c = execution.OwnTestsCommand(s.command, readers, cov) + " && " + c
				scopeReasons = append(scopeReasons, fmt.Sprintf("%s: %d test module(s) name a changed document, and run beside testmon's selection", p, len(readers)))
			}
			steps[p][i].command = c
			narrowed[p] = "testmon"
			precise = append(precise, p)
			failedOnly(p, i)
		}
		steps[p] = slices.DeleteFunc(steps[p], func(s step) bool { return s.command == "" })
		if len(steps[p]) == 0 {
			plan = plan[:len(plan)-1]
		}
	}
	recordPath := execution.RecordShown(root, change)
	// A command that passed in the last run on these same bytes is reused:
	// running it again could only repeat the pass. --force runs it anyway. A
	// first run is never reused: it is run for the record it builds.
	passes := map[string]execution.Execution{}
	if !force {
		passes = execution.PassesOn(root, change, workingTree)
	}
	reusable := func(p string, s step) bool {
		_, ok := passes[execution.Key(p, s.kind, s.command)]
		return ok && s.build == ""
	}
	// Progress and notes go where they cannot corrupt a JSON document.
	say := func(line string) {
		if json {
			fmt.Fprintln(os.Stderr, line)
		} else {
			fmt.Println(line)
		}
	}
	notes := append(append([]string{}, resolved.Notes...), scopeReasons...)
	if n := len(passes); n > 0 {
		notes = append(notes, fmt.Sprintf("commands that passed on this same working tree in the last run are reused, not run again (up to %d); --force runs them", n))
	}
	groups, groupNotes := exclusiveGroups(configSrc, all)
	notes = append(notes, groupNotes...)
	if len(vitest) > 0 {
		notes = append(notes, "tests narrowed by vitest --related, inside each project nx affected selects, to those whose imports reach a changed file or its component: "+strings.Join(vitest, ", "))
	}
	if len(precise) > 0 {
		notes = append(notes, testmonNotes(states, precise, firstNotes)...)
	}
	for _, p := range precise {
		if r, ok := estimates[p]; ok {
			notes = append(notes, fmt.Sprintf("%s: pytest-testmon would run %s", p, r.Selection()))
		}
	}
	if n := lastRuns(root, plan); n != "" {
		notes = append(notes, n)
	}
	var unknown []string
	for _, p := range resolved.Projects {
		if p.Testability == discover.Unknown() {
			unknown = append(unknown, p.Path)
		}
	}
	if len(unknown) > 0 {
		notes = append(notes, fmt.Sprintf("not run, %s: %s", discover.Unknown(), strings.Join(unknown, ", ")))
	}
	// A unit's run with nothing to run leaves the record of the last run as it
	// is: on the tree a wide run passed on, it would otherwise replace the
	// record a slice's close and a delivery read with an empty one.
	keep := unit && len(plan) == 0 && project.Exists(execution.RecordFile(root, change))
	if keep {
		notes = append(notes, "no project selected; the record is left as the last run wrote it")
	} else if len(plan) == 0 {
		notes = append(notes, "no project selected; the record holds no execution")
	}
	affectedSince := ""
	if affected {
		affectedSince = " (affected since " + sinceShown + ")"
	}
	// deadlineOf is a command's deadline: the first-run deadline for one that
	// has to end once (step.long), unless the run names its own.
	deadlineOf := func(long bool) float64 {
		if long && !explicit {
			return execution.FirstRunMinutes
		}
		return minutes
	}

	if dry {
		if json {
			out := dryRun{true, recordPath, scope, base, selection, minutes, []planned{}, []projectPlan{}, notes, unit, since}
			for _, p := range plan {
				pp := projectPlan{Project: p, Checks: []string{}, Reused: true}
				for _, s := range steps[p] {
					step := planned{p, s.kind, s.command, p, reusable(p, s), 0}
					if m := deadlineOf(s.long); m != minutes {
						step.TimeoutMinutes = m
					}
					out.Plan = append(out.Plan, step)
					pp.Reused = pp.Reused && reusable(p, s)
					if s.kind != "" {
						pp.Checks = append(pp.Checks, s.kind)
						continue
					}
					pp.Tests = cmp.Or(narrowed[p], "whole")
					pp.Modules = modules[p]
					if r, ok := estimates[p]; ok {
						pp.Estimate = &reachJSON{r.Selects, r.Total, r.SelectSeconds, r.AllTime}
					}
					if ms, ok := execution.LastTestRun(root, p); ok {
						pp.LastSeconds = float64(ms) / 1000
					}
					if f, ok := firstRuns[p]; ok {
						pp.FirstRun = &f
					}
					if r, ok := reaches[p]; ok && r.Failed > 0 {
						pp.FailedMarks = &failedMarksJSON{r.Failed, r.Total}
					}
				}
				if pp.Tests == "" && narrowed[p] == "own" {
					pp.Tests = "none" // no test module of it changed: the PR's CI runs them
				}
				out.Projects = append(out.Projects, pp)
			}
			// A selected project left with nothing to run here is listed too:
			// its tests are the PR's CI's.
			for _, s := range selection {
				if !slices.Contains(plan, s.Project) && narrowed[s.Project] == "own" {
					out.Projects = append(out.Projects, projectPlan{Project: s.Project, Tests: "none", Checks: []string{}})
				}
			}
			return 0, printJSON(out)
		}
		lines := []string{fmt.Sprintf("scope  %s%s, deadline %s min per command, %d project(s) at a time", scope, affectedSince, js.NumberString(minutes), jobs)}
		for _, p := range plan {
			reason := "undefined"
			if i := slices.IndexFunc(selection, func(s execution.Selected) bool { return s.Project == p }); i >= 0 {
				reason = selection[i].Reason
			}
			for _, s := range steps[p] {
				verb := "would run"
				if reusable(p, s) {
					verb = "would reuse"
				}
				deadline := ""
				if m := deadlineOf(s.long); m != minutes {
					deadline = "; deadline " + js.NumberString(m) + " min"
				}
				lines = append(lines, fmt.Sprintf("%s  %s: sh -c '%s'  (cwd %s; %s%s)", verb, label(p, s), s.command, filepath.Join(root, p), reason, deadline))
			}
		}
		lines = append(lines, "would write  "+recordPath)
		for _, n := range notes {
			lines = append(lines, "note  "+n)
		}
		lines = append(lines, "dry run: nothing was run and nothing was written")
		fmt.Println(strings.Join(lines, "\n"))
		return 0, nil
	}

	// Outside a git repository the candidate is unknown; that never refuses a
	// run, it only leaves delivery without an identity to match.
	record := &execution.Record{
		Schema: execution.Schema(), Timestamp: now(), Scope: scope, Base: base, ScopeReasons: scopeReasons,
		Selection: selection, TimeoutMinutes: minutes, CandidateFields: execution.Fields(vcs.CandidateOf(root, exempt)), WorkingTree: workingTree,
		Executions: []execution.Execution{}, History: execution.HistoryOf(root, change),
		LightVerify: execution.LightVerifyOf(root, change),
		Unit:        unit, Since: since, LastPass: execution.LastPassOf(root, change),
		TurnedGreen: execution.TurnedGreenOf(root, change),
	}
	abs := execution.RecordFile(root, change)
	// A folderless change's record directory is made with its first run.
	if err := os.MkdirAll(filepath.Dir(abs), 0o777); err != nil {
		return 1, err
	}
	// A signal from here on is the run's to handle: it stops the running
	// project, writes the partial record, and exits. One that arrives between
	// projects is handled while the next one runs, as node handles it at its
	// next wait; one after the last project is ignored.
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	// The first signal stops every running project and starts no other.
	stop := make(chan struct{})
	var stopSig os.Signal
	go func() {
		stopSig = <-sigs
		close(stop)
	}()
	stopped := func() bool {
		select {
		case <-stop:
			return true
		default:
			return false
		}
	}

	stdout := os.Stdout
	if json {
		stdout = os.Stderr
	}
	// One project's commands run in order; projects run in parallel, up to
	// jobs at a time. In parallel a project's output is held in a file and
	// printed whole when it finishes, so outputs never interleave. The file
	// is unlinked as soon as it is made and read through its descriptor, so
	// a run killed mid-way leaves nothing in TMPDIR (T267).
	var mu sync.Mutex
	done := make([][]execution.Execution, len(plan))
	var startErr string
	write := func() error {
		record.Executions = slices.Concat(done...)
		record.Timestamp = now()
		record.Repos = execution.ReposOf(root, change)
		if keep {
			return nil
		}
		return execution.WriteRecord(abs, record)
	}
	var writeErr error
	// runNotes are what the run learned while it ran: a record it did not
	// keep, a run its deadline cut.
	var runNotes []string
	runProject := func(i int, p string) {
		out, held := stdout, (*os.File)(nil)
		if jobs > 1 {
			if f, err := os.CreateTemp("", "jhon-executions-"); err == nil {
				os.Remove(f.Name())
				out, held = f, f
				defer f.Close()
			}
		}
		var lines []string
		show := func(line string) {
			if held == nil {
				say(line)
				return
			}
			held.Seek(0, 0)
			if b, err := io.ReadAll(held); err == nil && len(b) > 0 {
				lines = append(lines, strings.TrimSuffix(string(b), "\n"))
				held.Truncate(0)
				held.Seek(0, 0)
			}
			lines = append(lines, line)
		}
		var errOut *os.File
		if held != nil {
			errOut = held
		}
		defer func() {
			if held != nil {
				mu.Lock()
				say(strings.Join(lines, "\n"))
				mu.Unlock()
			}
		}()
		// The project's exclusive groups hold across runs too.
		release, ok := execution.LockGroups(root, groups[p], stop, func(g string) {
			mu.Lock()
			say(fmt.Sprintf("... %s: waiting for executions_exclusive.%s, which another run holds", p, g))
			mu.Unlock()
		})
		if !ok {
			return
		}
		defer release()
		for _, s := range steps[p] {
			if stopped() {
				return
			}
			if prev, ok := passes[execution.Key(p, s.kind, s.command)]; ok && s.build == "" {
				show(fmt.Sprintf("==> %s: %s  (reused: it passed on this same working tree in the last run)", label(p, s), s.command))
				mu.Lock()
				done[i] = append(done[i], execution.Execution{Project: p, Kind: s.kind, Command: s.command, Cwd: p,
					ExitCode: prev.ExitCode, Status: execution.Pass, Reused: true})
				if err := write(); err != nil && writeErr == nil {
					writeErr = err
				}
				mu.Unlock()
				continue
			}
			for _, n := range s.notes {
				show("... " + n)
			}
			show(fmt.Sprintf("==> %s: %s", label(p, s), s.command))
			started := time.Now()
			stepMinutes := deadlineOf(s.long)
			sig := make(chan os.Signal, 1)
			finished := make(chan struct{})
			go func() {
				select {
				case <-stop:
					sig <- stopSig
				case <-finished:
				}
			}()
			tail := &execution.Tail{}
			watch := execution.NewWatch()
			if s.build != "" {
				execution.StartBuild(s.build)
			}
			// A pytest-testmon run writes a private copy of the shared record,
			// which replaces it only when pytest ran to its end (T255).
			command := s.command
			var private *execution.Private
			if s.data != "" && strings.Contains(command, "TESTMON_DATAFILE=") {
				if c, err := execution.CheckOut(s.data); err == nil {
					private, command = c, c.Command(command)
				}
			}
			r := execution.RunOne(command, filepath.Join(root, p), execution.Run{Stdout: out, Stderr: errOut, Deadline: execution.Deadline(stepMinutes), Signals: sig, Capture: io.MultiWriter(tail, watch)})
			close(finished)
			refused := ""
			if private != nil {
				code := r.Code
				if r.Interrupted != nil || r.StartError != "" {
					code = nil
				}
				_, refused = private.Finish(code, watch)
			}
			// A first run's file is the record only when pytest ran to its end.
			if s.build != "" {
				why, notKept := firstRunEnd(s.build, r, stepMinutes, watch)
				refused = notKept
				if why != "" {
					show(fmt.Sprintf("... %s: the first run under pytest-testmon %s, so no record was kept and later runs run only the test modules the change touched; `jhon executions prime --timeout <minutes>` builds it with a longer deadline", p, why))
				}
			}
			if refused != "" {
				n := refusedNote(p, refused)
				show("... " + n)
				mu.Lock()
				runNotes = append(runNotes, n)
				mu.Unlock()
			}
			if r.Interrupted != nil {
				return
			}
			if r.StartError != "" {
				mu.Lock()
				startErr = fmt.Sprintf("could not start sh for %s: %s", label(p, s), r.StartError)
				mu.Unlock()
				return
			}
			status := execution.Timeout
			switch {
			case r.Code == nil:
				got := ""
				if progress := watch.Progress(); progress != "" {
					got = "; " + progress
				}
				show(fmt.Sprintf("<== %s: timeout after %s min; its process group was killed%s", label(p, s), js.NumberString(stepMinutes), got))
				// pytest-testmon writes its record at a run's end: a run cut
				// every time never narrows the next one (T255, L1725).
				if private != nil {
					after := ""
					if progress := watch.Progress(); progress != "" {
						after = " after " + progress
					}
					mu.Lock()
					runNotes = append(runNotes, fmt.Sprintf("%s: cut at its %s min deadline%s: pytest-testmon writes its record only at a run's end, so the next run selects the same tests again; give it a longer --timeout <minutes>", p, js.NumberString(stepMinutes), after))
					mu.Unlock()
				}
			case *r.Code == 0:
				status = execution.Pass
			default:
				status = execution.Fail
			}
			if r.Code != nil {
				show(fmt.Sprintf("<== %s: exit %d", label(p, s), *r.Code))
			}
			mu.Lock()
			e := execution.Execution{Project: p, Kind: s.kind, Command: s.command, Cwd: p,
				ExitCode: r.Code, Status: status, DurationMs: time.Since(started).Milliseconds()}
			if status != execution.Pass {
				e.OutputTail = tail.String()
				e.FailedTests = execution.FailedTests(e.OutputTail)
			}
			done[i] = append(done[i], e)
			if err := write(); err != nil && writeErr == nil {
				writeErr = err
			}
			failedStart := startErr != ""
			mu.Unlock()
			if failedStart {
				return
			}
		}
	}
	// The next project to start is the first in plan order with a free slot
	// and none of its exclusive groups running; otherwise wait for one to end.
	var wg sync.WaitGroup
	busy := map[string]bool{}
	started := make([]bool, len(plan))
	ended := make(chan int, len(plan))
	running := 0
	for left := len(plan); left > 0; {
		mu.Lock()
		halt := startErr != ""
		mu.Unlock()
		if halt || stopped() {
			break
		}
		next := -1
		for i, p := range plan {
			if running < jobs && !started[i] && !slices.ContainsFunc(groups[p], func(g string) bool { return busy[g] }) {
				next = i
				break
			}
		}
		if next < 0 {
			for _, g := range groups[plan[<-ended]] {
				busy[g] = false
			}
			running--
			continue
		}
		started[next], running, left = true, running+1, left-1
		for _, g := range groups[plan[next]] {
			busy[g] = true
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { ended <- next }()
			runProject(next, plan[next])
		}()
	}
	wg.Wait()
	notes = append(notes, runNotes...)
	if writeErr != nil {
		return 1, writeErr
	}
	if stopped() {
		if err := write(); err != nil {
			return 1, err
		}
		reportRecord(abs)
		sig := stopSig.(syscall.Signal)
		fmt.Fprintf(os.Stderr, "jhon executions: interrupted by %s; the partial record %s holds %d execution(s)\n", js.SignalName(sig), recordPath, len(record.Executions))
		return 128 + int(sig), nil
	}
	if startErr != "" {
		fmt.Fprintf(os.Stderr, "jhon executions: %s; the record holds the executions before it\n", startErr)
		if err := write(); err != nil {
			return 1, err
		}
		reportRecord(abs)
		return 2, nil
	}

	after := execution.Fields(vcs.CandidateOf(root, exempt))
	record.After = &after
	// The working tree is read again, as at the start: a run that started on
	// uncommitted work and saw a file change, appear or go while it ran did
	// not test one tree, however clean or dirty both ends are (T146d).
	record.AfterWorkingTree = vcs.WorkingTree(root, exempt)
	record.Dirty = after.CandidateCommit != record.CandidateCommit || after.CandidateTree != record.CandidateTree ||
		after.WorkingTreeClean != record.WorkingTreeClean || record.AfterWorkingTree != record.WorkingTree
	record.Complete = true
	record.Timestamp = now()
	// A test turned green by editing only its test is marked, never refused
	// (T137): verify and the Prism facets are launched with it to judge.
	for _, m := range execution.MarkTurnedGreen(root, record, all, inert) {
		notes = append(notes, fmt.Sprintf("%s: %s turned green by editing only its test (%s): failed in run %s, passed in run %s; verify and the Prism facets judge whether it was weakened", m.Project, m.Test, m.File, m.RedRun, m.GreenRun))
	}
	record.Repos = execution.ReposOf(root, change)
	if !keep {
		if err := execution.WriteRecord(abs, record); err != nil {
			return 1, err
		}
	}
	reportRecord(abs)
	// A slice's data-access files whose real-database reach no testmon record
	// can show (ldd.DataReachNote): said, never refused.
	if n := ldd.DataReachNote(top, change); n != "" {
		notes = append(notes, n)
	}
	passed := 0
	for _, e := range record.Executions {
		if e.Status == execution.Pass {
			passed++
		}
	}
	telemetry.Note("jhon.change", change)
	telemetry.Note("jhon.tests.scope", scope)
	telemetry.Note("jhon.tests.executions", len(record.Executions))
	telemetry.Note("jhon.tests.passed", passed)
	telemetry.Note("jhon.tests.dirty", record.Dirty)

	if json {
		if err := printJSON(runSummary{recordPath, scope, base, record.Executions, record.CandidateCommit, record.CandidateTree,
			record.WorkingTreeClean, record.Dirty, notes, execution.RedBeforeGreen(root, change), unit, since}); err != nil {
			return 1, err
		}
	} else {
		lines := []string{"", "record     " + recordPath, "scope      " + scope + affectedSince,
			fmt.Sprintf("candidate  %s  tree %s  clean %v", record.CandidateCommit, record.CandidateTree, record.WorkingTreeClean)}
		if record.Dirty {
			lines = append(lines, fmt.Sprintf("dirty      the candidate changed during the run (after: %s tree %s clean %v working tree %s)",
				after.CandidateCommit, after.CandidateTree, after.WorkingTreeClean, record.AfterWorkingTree))
		}
		for _, e := range record.Executions {
			lines = append(lines, js.PadEnd(e.Status, 8)+"   "+label(e.Project, step{kind: e.Kind, command: e.Command}))
		}
		for _, n := range notes {
			lines = append(lines, "note       "+n)
		}
		fmt.Println(strings.Join(lines, "\n"))
	}
	result, code := "pass", 0
	if passed < len(record.Executions) {
		result, code = "fail", 1
	}
	say(fmt.Sprintf("jhon executions: %s, exit %d (%d of %d passed; record %s)", result, code, passed, len(record.Executions), recordPath))
	return code, nil
}

// needsFirstRun is whether a record in state cannot narrow a run until the
// whole suite has run once under pytest-testmon.
func needsFirstRun(state string) bool {
	return state == execution.RecordNone || state == execution.RecordUnusable
}

// firstRunEnd ends the first run of the record at data, which ended as r
// under a deadline of minutes and whose output w watched
// (execution.FinishBuild), and is why it built no record that later runs
// should not try again here: "" when it built one, and when it was stopped
// or failed before pytest's end, which the next run tries again. refused is
// why the record it wrote is not kept, its failures being the test
// environment's (execution.Watch.Refusal): the next run tries again too.
func firstRunEnd(data string, r execution.Outcome, minutes float64, w *execution.Watch) (why, refused string) {
	if r.Interrupted != nil || r.StartError != "" {
		execution.FinishBuild(data, nil, nil)
		return "", ""
	}
	kept, refused := execution.FinishBuild(data, r.Code, w)
	if refused != "" {
		return "", refused
	}
	switch {
	case r.Code == nil:
		why = "hit its " + js.NumberString(minutes) + " min deadline"
	case !kept && (*r.Code == 0 || *r.Code == 1):
		why = "ran to its end and left no record that narrows"
	}
	if why != "" {
		execution.MarkFirstRunCut(data, why)
	}
	return why, ""
}

// timeoutMinutes is --timeout's minutes, the default when arg is nil; ok is
// false, with the reason and usage printed, when it is no positive number.
func timeoutMinutes(arg *string, usage string) (minutes float64, ok bool) {
	minutes = float64(execution.DefaultTimeoutMinutes)
	if arg != nil {
		minutes = js.Number(*arg)
	}
	if math.IsNaN(minutes) || math.IsInf(minutes, 0) || minutes <= 0 {
		shown := "undefined"
		if arg != nil {
			shown = *arg
		}
		fmt.Fprintf(os.Stderr, "jhon executions: --timeout takes a positive number of minutes, not '%s'\n%s\n", shown, usage)
		return 0, false
	}
	return minutes, true
}

// exclusiveGroups is executions_exclusive: for each project of all, the
// named groups of projects that share state (a test database) and so never
// run at the same time, and a note for each name that is no project.
func exclusiveGroups(configSrc string, all []string) (groups map[string][]string, notes []string) {
	groups = map[string][]string{}
	names, members := contract.Map(configSrc, "executions_exclusive")
	for _, g := range names {
		for _, p := range contract.InlineList(members[g]) {
			if !slices.Contains(all, p) {
				notes = append(notes, fmt.Sprintf("executions_exclusive.%s names %s, which is not a project", g, p))
				continue
			}
			groups[p] = append(groups[p], g)
		}
	}
	return groups, notes
}

// measured is what a unit's run measures from (unitSince).
type measured struct {
	files, outside []string
	// since is the record's `since`, shown how a message names it, and commit
	// the change's base commit.
	since, shown, commit string
	errMsg               string
}

// unitSince is what a unit's run of change (`--affected` with no base) in
// root measures from, and the note that says which: the working tree of the
// change's last passing run, compared with workingTree, else, when no run
// passed or git no longer holds that tree, the commit the slice in progress
// started from (ldd.SliceBase, T253), which before the change's first
// closed slice is the change's base (ldd.ChangeBase).
func unitSince(root, top, change, workingTree string) (m measured, note string) {
	const unit = "a unit's run (--affected with no base): "
	commit, label, ok := ldd.ChangeBase(top)
	if !ok {
		commit, label = "HEAD", "HEAD"
	}
	m.commit = commit
	why := "no passing run of this change is on record"
	if last := execution.LastPassOf(root, change); last != nil {
		if files, outside, ok := execution.ChangedBetween(root, last.WorkingTree, workingTree); ok {
			m.files, m.outside, m.since, m.shown = files, outside, last.WorkingTree, "the change's last passing run"
			// A pass older than the slice's start selects what earlier
			// slices changed too: said, with the narrower base (L1728).
			past := ""
			if start, ok := ldd.SliceBase(root, change); ok && reachesPast(root, files, start) {
				short := start[:min(12, len(start))]
				past = ", which reaches back past the commit the slice in progress started from (" + short + "): `--affected " + short + "` selects only what this slice changed"
			}
			return m, unit + "what changed since the change's last passing run (" + last.Timestamp + ") is selected" + past + "; a slice's close takes `--affected <base>`"
		}
		why = "the tree of the change's last passing run is no longer in git"
	}
	from := "the change's base (" + label + ")"
	m.since, m.shown = label, "the change's base, "+label
	if start, ok := ldd.SliceBase(root, change); ok && start != commit {
		short := start[:min(12, len(start))]
		commit, m.commit = start, start
		m.since, m.shown = start, "the commit the slice in progress started from, "+short
		from = "the commit the slice in progress started from (" + short + ", the last that closed a slice of the change)"
	}
	m.files, m.outside, m.errMsg = execution.ChangedSince(root, commit)
	return m, unit + why + ", so what changed since " + from + " is selected; a slice's close takes `--affected <base>`"
}

// spanMin is how many projects a changed one must bring into a selection as
// its dependents for the run to say so (dependentSpans).
const spanMin = 2

// span is a changed project and the selected projects that depend on it.
type span struct {
	project    string
	dependents []string
}

// dependentSpans are the projects of an affected run's selection that bring
// in at least spanMin others as their dependents, directly or through
// another dependent (a reason `depends on <project>`), in selection order:
// a change to a shared project selects every project depending on it,
// which may be most of a monorepo (T253, L1721).
func dependentSpans(selected []execution.Selected) []span {
	why := map[string]string{}
	for _, s := range selected {
		why[s.Project] = s.Reason
	}
	// rootOf is the project p is selected through: the first of its chain
	// of `depends on` reasons that is not itself a dependent.
	rootOf := func(p string) string {
		for seen := map[string]bool{}; !seen[p]; {
			seen[p] = true
			d, ok := strings.CutPrefix(why[p], "depends on ")
			if !ok {
				return p
			}
			p = d
		}
		return ""
	}
	var order []string
	by := map[string][]string{}
	for _, s := range selected {
		if !strings.HasPrefix(s.Reason, "depends on ") {
			continue
		}
		r := rootOf(s.Project)
		if r == "" {
			continue
		}
		if _, ok := by[r]; !ok {
			order = append(order, r)
		}
		by[r] = append(by[r], s.Project)
	}
	var out []span
	for _, r := range order {
		if len(by[r]) >= spanMin {
			out = append(out, span{r, by[r]})
		}
	}
	return out
}

// reachesPast is whether files, what changed since a passing run, hold a
// project file that did not change since the commit start: the run would
// select more than what changed since start. The change's own record is
// left out (layout.Ignored).
func reachesPast(root string, files []string, start string) bool {
	since, _, msg := execution.ChangedSince(root, start)
	if msg != "" {
		return false
	}
	return slices.ContainsFunc(files, func(f string) bool {
		return !layout.For(root).Ignored(f) && !slices.Contains(since, f)
	})
}

// isAncestor is whether the commit a is b or an ancestor of it in the
// repository at top: measuring from a selects at least what b does.
func isAncestor(top, a, b string) bool {
	return project.ReadGitCmd(top, "merge-base", "--is-ancestor", a, b).Run() == nil
}

// revParse is the commit ref names in the repository at top, or ref itself
// when git cannot say.
func revParse(top, ref string) string {
	out, err := project.ReadGitCmd(top, "rev-parse", "--verify", "--quiet", ref+"^{commit}").Output()
	if err != nil {
		return ref
	}
	return strings.TrimSpace(string(out))
}

// testmonNotes say, for the projects pytest-testmon narrows, whose record is
// in states, which have a record that narrows this run, and, one by one with
// first's note (firstRunOf), which run all their tests because they have
// none, or one a crashed run left without fingerprints.
func testmonNotes(states map[string]string, projects []string, first map[string]string) []string {
	by := map[string][]string{}
	for _, p := range projects {
		by[states[p]] = append(by[states[p]], p)
	}
	var out []string
	for _, n := range []struct{ state, note string }{
		{execution.RecordUsable, "tests narrowed by pytest-testmon to those whose executed code changed: "},
		{execution.RecordNone, ""},
		{execution.RecordUnusable, ""},
		{execution.RecordUnknown, "pytest-testmon's record could not be read (python3 is needed), so how far it narrows is unknown: "},
	} {
		if n.note == "" {
			for _, p := range by[n.state] {
				if first[p] != "" {
					out = append(out, first[p])
				}
			}
			continue
		}
		if len(by[n.state]) > 0 {
			out = append(out, n.note+strings.Join(by[n.state], ", "))
		}
	}
	return out
}

// firstRunOf is the first run of the project at projectPath under root,
// whose pytest-testmon record is in state (none or unusable): every test of
// it runs once (T255, L1723), and the note that says so, with how many test
// functions its test modules define (execution.CountTests) and, when long,
// its deadline.
func firstRunOf(root, projectPath, state string, long bool) (firstRunJSON, string) {
	tests, modules := execution.CountTests(root, projectPath)
	f := firstRunJSON{Record: state, Tests: tests, Modules: modules}
	count, deadline := "", ""
	if tests > 0 {
		count = fmt.Sprintf(" (%s in %s)", plural(tests, "test function"), plural(modules, "module"))
	}
	if long {
		f.DeadlineMinutes = execution.FirstRunMinutes
		deadline = fmt.Sprintf(", under the first-run deadline of %d min", execution.FirstRunMinutes)
	}
	if state == execution.RecordUnusable {
		return f, projectPath + ": pytest-testmon's record of it holds no file fingerprints (a run crashed while writing it), so all its tests run once" + count + " and repair it" + deadline + "; later runs are narrowed"
	}
	return f, projectPath + ": pytest-testmon has no record of it yet, so all its tests run once" + count + " and become its record" + deadline + "; later runs are narrowed"
}

// plural is n things.
func plural(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return strconv.Itoa(n) + " " + thing + "s"
}

// mostlyFailedNote says a project's pytest-testmon record marks most of its
// tests failed (execution.Reach.MostlyFailed), why, and how to drop it.
func mostlyFailedNote(p string, r execution.Reach) string {
	return fmt.Sprintf("%s: pytest-testmon's record marks %d of its %d tests failed, and pytest-testmon runs every failed test again whatever changed: a run while the test services were down (connection refused) leaves such a record; with them up, a run that ends repairs it, or `jhon executions forget %s` drops it and the next run builds it again", p, r.Failed, r.Total, p)
}

// refusedNote says a project's run kept no pytest-testmon record, why being
// its failures the test environment's (execution.Watch.Refusal).
func refusedNote(p, why string) string {
	return p + ": the record pytest-testmon wrote in this run is not kept: " + why + ", which points at the test environment, not the change: are the test services (a database, a cache) up? The record stays as it was"
}

const executionsForgetUsage = "usage: jhon executions forget <project> [--root <project>]"

// runExecutionsForget drops a uv-run pytest project's pytest-testmon record
// (T255, L1725): `jhon executions forget <project> [--root <project>]`. The
// next affected run builds it again, every test once. The whole record, not
// only its failed marks: a test that failed because a service was down
// recorded only the code it ran before failing, so trusting it as passing
// would let a change it reaches select it no more, and a record must only
// ever make pytest-testmon select more, never less (L1724). Every
// worktree's own record from before the shared one goes too, or that
// worktree would adopt it again (T256). Exit 0, or 2 on a usage error or a name that is no such project.
func runExecutionsForget(argv []string) (int, error) {
	var rootArg *string
	var names []string
	for i := 0; i < len(argv); i++ {
		if argv[i] == "--root" {
			if i+1 >= len(argv) || strings.HasPrefix(argv[i+1], "-") {
				fmt.Fprintln(os.Stderr, executionsForgetUsage)
				return 2, nil
			}
			i++
			rootArg = &argv[i]
			continue
		}
		names = append(names, argv[i])
	}
	if len(names) != 1 || strings.HasPrefix(names[0], "-") {
		fmt.Fprintln(os.Stderr, executionsForgetUsage)
		return 2, nil
	}
	name := strings.TrimSuffix(names[0], "/")
	root := discover.FindRoot(cwd())
	if rootArg != nil {
		root = project.Resolve(cwd(), *rootArg)
	}
	resolved, err := discover.ProjectsFor(root)
	if err != nil {
		return 1, err
	}
	top := vcs.GitTopLevel(root)
	var pytest []string
	for _, p := range resolved.Projects {
		if p.Command == nil {
			continue
		}
		if _, ok := execution.Precise(*p.Command, top, p.Path, false); ok {
			pytest = append(pytest, p.Path)
		}
	}
	if !slices.Contains(pytest, name) {
		known := "none"
		if len(pytest) > 0 {
			known = strings.Join(pytest, ", ")
		}
		fmt.Fprintf(os.Stderr, "jhon executions forget: %s is not a uv-run pytest project here: %s\n", name, known)
		return 2, nil
	}
	data := execution.TestmonPath(top, name)
	dropped := execution.Forget(data)
	for _, legacy := range execution.LegacyTestmonRecords(top, name) {
		if execution.Forget(legacy) {
			dropped = true
		}
	}
	if !dropped {
		fmt.Printf("jhon executions forget: %s has no pytest-testmon record; nothing to drop\n", name)
		return 0, nil
	}
	fmt.Printf("jhon executions forget: %s's pytest-testmon record is dropped (%s); the next affected run builds it again, every test once\n", name, data)
	return 0, nil
}

// lastRuns is how long each planned project's tests took the last time an
// execution record ran them, for announcing when a long run will end; ""
// when no record has any.
func lastRuns(root string, projects []string) string {
	var parts []string
	for _, p := range projects {
		ms, ok := execution.LastTestRun(root, p)
		if !ok {
			continue
		}
		d := time.Duration(ms) * time.Millisecond
		if d >= time.Minute {
			parts = append(parts, fmt.Sprintf("%s %d min", p, int(d.Round(time.Minute).Minutes())))
		} else {
			parts = append(parts, fmt.Sprintf("%s %d s", p, int(d.Round(time.Second).Seconds())))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "the last recorded test run took: " + strings.Join(parts, ", ") + "; a run that narrows takes less"
}

// heldModules splits touch, the test modules (relative to the project at
// projectPath under root) a wide run's change touched, into those the
// project's pytest-testmon record, read from data, holds tests of, which it can narrow, and
// those it holds none of (new, or unknown to it), which run whole. A module
// of whole (a document's reader, executions_always) runs whole and is in
// neither. read is false when python3 could not read the record (no
// python3, an unreadable file): every module then runs whole.
func heldModules(top, root, projectPath, data string, touch, whole []string) (held, unheld []string, read bool) {
	var byTop []string
	at := map[string]string{}
	for _, m := range touch {
		if slices.Contains(whole, m) {
			continue
		}
		rel, err := filepath.Rel(top, filepath.Join(root, projectPath, m))
		if err != nil {
			unheld = append(unheld, m)
			continue
		}
		rel = filepath.ToSlash(rel)
		byTop, at[rel] = append(byTop, rel), m
	}
	in, ok := execution.TestmonHeld(data, byTop)
	for _, rel := range byTop {
		if ok && slices.Contains(in, rel) {
			held = append(held, at[rel])
		} else {
			unheld = append(unheld, at[rel])
		}
	}
	return held, unheld, ok
}

// listShort is names joined, the first five and how many more.
func listShort(names []string) string {
	s := strings.Join(names[:min(5, len(names))], ", ")
	if len(names) > 5 {
		s += fmt.Sprintf(", and %d more", len(names)-5)
	}
	return s
}

// runs is the verb for n modules.
func runs(n int) string {
	if n == 1 {
		return "runs"
	}
	return "run"
}
