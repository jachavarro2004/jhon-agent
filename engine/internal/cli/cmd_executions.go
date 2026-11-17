package cli

import (
	"cmp"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
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

// `jhon executions run <change> [--affected [<base>]] [--timeout <minutes>]
// [--dry-run] [--json] [--root <project>]`: run every
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
// else its base. stdin is closed,
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

const executionsUsage = "usage: jhon executions run <change> [--affected [<base-ref>]] [--timeout <minutes>] [--jobs <n>] [--force] [--dry-run] [--json] [--root <project>]"

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

type planned struct {
	Project string `json:"project"`
	Kind    string `json:"kind,omitempty"`
	Command string `json:"command"`
	Cwd     string `json:"cwd"`
	// Reused is a command the run would not run: it passed on this working tree.
	Reused bool `json:"reused,omitempty"`
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
	// unit is `--affected` with no base: the last argument, or before an option.
	json, dry, force, unit := false, false, false, false
	var rootArg, base, timeoutArg, jobsArg *string
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
		case "--root", "--affected", "--timeout", "--jobs":
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
			default:
				timeoutArg = &v
			}
		default:
			rest = append(rest, a)
		}
	}
	if len(rest) != 2 || rest[0] != "run" || strings.HasPrefix(rest[1], "-") {
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
	change := rest[1]
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
	// build is the pytest-testmon record a first run's command builds, or "".
	type step struct{ kind, command, build string }
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
	// Each pytest project's record state, read once.
	states := map[string]string{}
	// wide is why the run of p is wide, "" when it is not; byReach is whether
	// its usable record says so, and so can still narrow the touched modules.
	wide := func(p string) (why string, byReach bool) {
		data := execution.TestmonData(top, p)
		// No record, and the first run that builds one was cut: it is not run
		// again here, where it would be cut again.
		if why, cut := execution.FirstRunCut(data); cut && needsFirstRun(states[p]) {
			return "pytest-testmon has no record, since the first run that builds it " + why + " (`jhon executions prime --timeout <minutes>` builds it with a longer deadline)", false
		}
		if untracked != "" {
			return untracked + " changed, and pytest-testmon tracks Python files only", false
		}
		switch states[p] {
		case execution.RecordUsable:
			if r, ok := execution.TestmonReach(data, changedPy); ok && r.Total > 0 {
				if r.Wide() {
					return "the change reaches " + r.String(), true
				}
				estimates[p] = r
			}
		}
		return "", false
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
			data := execution.TestmonData(top, p)
			states[p] = execution.TestmonRecord(data)
			// Narrowing starts with a record: with none, or one that holds no
			// fingerprints, the whole command runs once, collecting it.
			if _, cut := execution.FirstRunCut(data); needsFirstRun(states[p]) && !cut {
				steps[p][i].command, _ = execution.Collect(s.command, top, p, cov)
				steps[p][i].build = data
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
					held, unheld, read = heldModules(top, root, p, touch, slices.Concat(readers, execution.AlwaysTests(root, p, always, nil)))
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
		notes = append(notes, testmonNotes(states, precise)...)
	}
	for _, p := range precise {
		if r, ok := estimates[p]; ok {
			notes = append(notes, fmt.Sprintf("%s: pytest-testmon would run %s", p, r))
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

	if dry {
		if json {
			out := dryRun{true, recordPath, scope, base, selection, minutes, []planned{}, []projectPlan{}, notes, unit, since}
			for _, p := range plan {
				pp := projectPlan{Project: p, Checks: []string{}, Reused: true}
				for _, s := range steps[p] {
					out.Plan = append(out.Plan, planned{p, s.kind, s.command, p, reusable(p, s)})
					pp.Reused = pp.Reused && reusable(p, s)
					if s.kind != "" {
						pp.Checks = append(pp.Checks, s.kind)
						continue
					}
					pp.Tests = cmp.Or(narrowed[p], "whole")
					pp.Modules = modules[p]
					if r, ok := estimates[p]; ok {
						pp.Estimate = &reachJSON{r.Tests, r.Total, r.Seconds, r.AllTime}
					}
					if ms, ok := execution.LastTestRun(root, p); ok {
						pp.LastSeconds = float64(ms) / 1000
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
				lines = append(lines, fmt.Sprintf("%s  %s: sh -c '%s'  (cwd %s; %s)", verb, label(p, s), s.command, filepath.Join(root, p), reason))
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
	// printed whole when it finishes, so outputs never interleave.
	var mu sync.Mutex
	done := make([][]execution.Execution, len(plan))
	var startErr string
	write := func() error {
		record.Executions = slices.Concat(done...)
		record.Timestamp = now()
		if keep {
			return nil
		}
		return execution.WriteRecord(abs, record)
	}
	var writeErr error
	runProject := func(i int, p string) {
		out, held := stdout, (*os.File)(nil)
		if jobs > 1 {
			if f, err := os.CreateTemp("", "jhon-executions-"); err == nil {
				out, held = f, f
				defer os.Remove(f.Name())
				defer f.Close()
			}
		}
		var lines []string
		show := func(line string) {
			if held == nil {
				say(line)
				return
			}
			if b, err := os.ReadFile(held.Name()); err == nil && len(b) > 0 {
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
			show(fmt.Sprintf("==> %s: %s", label(p, s), s.command))
			started := time.Now()
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
			if s.build != "" {
				execution.StartBuild(s.build)
			}
			r := execution.RunOne(s.command, filepath.Join(root, p), execution.Run{Stdout: out, Stderr: errOut, Deadline: execution.Deadline(minutes), Signals: sig, Capture: tail})
			close(finished)
			// A first run's file is the record only when pytest ran to its end.
			if s.build != "" {
				if why := firstRunEnd(s.build, r, minutes); why != "" {
					show(fmt.Sprintf("... %s: the first run under pytest-testmon %s, so no record was kept and later runs run only the test modules the change touched; `jhon executions prime --timeout <minutes>` builds it with a longer deadline", p, why))
				}
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
				show(fmt.Sprintf("<== %s: timeout after %s min; its process group was killed", label(p, s), js.NumberString(minutes)))
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
	record.Dirty = after.CandidateCommit != record.CandidateCommit || after.CandidateTree != record.CandidateTree ||
		after.WorkingTreeClean != record.WorkingTreeClean
	record.Complete = true
	record.Timestamp = now()
	// A test turned green by editing only its test is marked, never refused
	// (T137): verify and the 4R lenses are launched with it to judge.
	for _, m := range execution.MarkTurnedGreen(root, record, all, inert) {
		notes = append(notes, fmt.Sprintf("%s: %s turned green by editing only its test (%s): failed in run %s, passed in run %s; verify and the 4R lenses judge whether it was weakened", m.Project, m.Test, m.File, m.RedRun, m.GreenRun))
	}
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
			lines = append(lines, fmt.Sprintf("dirty      the candidate changed during the run (after: %s tree %s clean %v)",
				after.CandidateCommit, after.CandidateTree, after.WorkingTreeClean))
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
// under a deadline of minutes (execution.FinishBuild), and is why it built no
// record that later runs should not try again here: "" when it built one,
// and when it was stopped or failed before pytest's end, which the next run
// tries again.
func firstRunEnd(data string, r execution.Outcome, minutes float64) string {
	if r.Interrupted != nil || r.StartError != "" {
		execution.FinishBuild(data, nil)
		return ""
	}
	kept := execution.FinishBuild(data, r.Code)
	why := ""
	switch {
	case r.Code == nil:
		why = "hit its " + js.NumberString(minutes) + " min deadline"
	case !kept && (*r.Code == 0 || *r.Code == 1):
		why = "ran to its end and left no record that narrows"
	}
	if why != "" {
		execution.MarkFirstRunCut(data, why)
	}
	return why
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
// change's last passing run, compared with workingTree, else the change's
// base (ldd.ChangeBase) when no run passed or git no longer holds that tree.
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
			return m, unit + "what changed since the change's last passing run (" + last.Timestamp + ") is selected; a slice's close takes `--affected <base>`"
		}
		why = "the tree of the change's last passing run is no longer in git"
	}
	m.files, m.outside, m.errMsg = execution.ChangedSince(root, commit)
	m.since, m.shown = label, "the change's base, "+label
	return m, unit + why + ", so what changed since the change's base (" + label + ") is selected; a slice's close takes `--affected <base>`"
}

// testmonNotes say, for the projects pytest-testmon narrows, whose record is
// in states, which have a record that narrows this run and which run every
// test because they have none, or one a crashed run left without fingerprints.
func testmonNotes(states map[string]string, projects []string) []string {
	by := map[string][]string{}
	for _, p := range projects {
		by[states[p]] = append(by[states[p]], p)
	}
	var out []string
	for _, n := range []struct{ state, note string }{
		{execution.RecordUsable, "tests narrowed by pytest-testmon to those whose executed code changed: "},
		{execution.RecordNone, "pytest-testmon has no record yet, so every test runs once and becomes it; later runs are narrowed: "},
		{execution.RecordUnusable, "pytest-testmon's record holds no file fingerprints (a run crashed while writing it), so every test runs once and repairs it; later runs are narrowed: "},
		{execution.RecordUnknown, "pytest-testmon's record could not be read (python3 is needed), so how far it narrows is unknown: "},
	} {
		if len(by[n.state]) > 0 {
			out = append(out, n.note+strings.Join(by[n.state], ", "))
		}
	}
	return out
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
// project's pytest-testmon record holds tests of, which it can narrow, and
// those it holds none of (new, or unknown to it), which run whole. A module
// of whole (a document's reader, executions_always) runs whole and is in
// neither. read is false when python3 could not read the record (no
// python3, an unreadable file): every module then runs whole.
func heldModules(top, root, projectPath string, touch, whole []string) (held, unheld []string, read bool) {
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
	in, ok := execution.TestmonHeld(execution.TestmonData(top, projectPath), byTop)
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
