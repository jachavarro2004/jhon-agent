// Command bench is the maintainer's benchmark of Jhon (T39): it runs the
// synthetic tasks under tasks/ in its conditions (Claude Code with Jhon,
// clean Claude Code, pi with the Jhon shell, pi with no Jhon) and compares
// the results. It is
// not part of the release. See tools/bench/README.md.
//
//	go run ./tools/bench prepare [--home DIR] [--pi-profile FILE --profile NAME] [--pi-models FILE]
//	go run ./tools/bench login-check [--home DIR]
//	go run ./tools/bench run [--tasks a,b] [--variants plain,ldd,ldd-native,ldd-ask] [--conditions jhon,clean,pi-jhon,pi-clean] [--repeats N] [--model M] [--pi-model P/M[@T]] [--pi-env NAMES] [--pi-models FILE] [--project-setting KEY=VALUE[,KEY=VALUE]] [--budget-points P] [--parallel N] [--smoke] [--dry-run] [--debug-prompt]
//	go run ./tools/bench report <results dir>
//	go run ./tools/bench recheck [--home DIR] [--tasks-dir DIR] <results dir>
//	go run ./tools/bench value [--json] [--out DIR] [--tasks-dir DIR] <results dirs>
//	go run ./tools/bench certify --baseline <dirs> [--candidate <dirs>] [--gates <checkout>] [--replay-guard <jhon>] [--allow-model-change] [--json]
//	go run ./tools/bench certify --plan
//	go run ./tools/bench review-compare [--fixtures a,b] [--arms native,agents] [--repeats N] [--pi-model P/M[@T]] [--dry-run]
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const usage = `usage: bench prepare [--home DIR] [--checkout DIR] [--ref COMMIT] [--pi-profile PROFILES_JSON] [--profile NAME] [--pi-models MODELS_JSON]
       bench login-check [--home DIR]
       bench run [--tasks a,b] [--variants plain,ldd,ldd-native,ldd-ask] [--conditions jhon,clean,pi-jhon,pi-clean] [--repeats N] [--model M]
                 [--budget-points P] [--home DIR] [--results DIR] [--allowed-tools LIST] [--expect-commit COMMIT] [--dry-run] [--debug-prompt]
                 [--parallel N] [--smoke] [--pi-model PROVIDER/MODEL[@THINKING]] [--pi-env NAME[,NAME]] [--pi-models MODELS_JSON]
                 [--project-setting KEY=VALUE[,KEY=VALUE]]
       bench report <results dir>
       bench recheck [--home DIR] [--tasks-dir DIR] <results dir>
       bench value [--json] [--out DIR] [--tasks-dir DIR] RESULTS_DIRS
       bench certify --baseline DIRS [--candidate DIRS] [--gates CHECKOUT] [--replay-guard JHON] [--allow-model-change] [--allow-setting-change] [--out DIR] [--json]
       bench certify --plan [--results-root DIR]
       bench review-compare [--fixtures a,b] [--arms native,agents] [--repeats N] [--model M] [--pi-model PROVIDER/MODEL[@THINKING]]
                 [--pi-env NAMES] [--home DIR] [--results DIR] [--timeout D] [--max-turns N] [--dry-run] | --report DIR
`

func main() { os.Exit(cli(os.Args[1:], os.Stdout, os.Stderr, time.Now())) }

// source is this package's directory, where tasks/ lives.
func source() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return filepath.Dir(file)
}

// root is the bench's own data directory, ~/.local/share/jhon-bench.
func root() string { return filepath.Join(os.Getenv("HOME"), ".local", "share", "jhon-bench") }

func split(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// resultsName is a batch's default results directory name: the time, the
// pi model when one is named, and the project settings (T86).
func resultsName(now time.Time, piModel string, settings ...string) string {
	name := now.UTC().Format("20060102T150405Z")
	r := strings.NewReplacer("/", "-", "@", "-", " ", "-", "=", "-")
	if piModel != "" {
		name += "-" + r.Replace(piModel)
	}
	for _, s := range settings {
		name += "-" + r.Replace(s)
	}
	return name
}

func cli(args []string, out, errOut io.Writer, now time.Time) int {
	if len(args) == 0 {
		fmt.Fprint(errOut, usage)
		return 2
	}
	fail := func(err error) int {
		fmt.Fprintf(errOut, "bench: %v\n", err)
		return 1
	}
	fs := flag.NewFlagSet("bench "+args[0], flag.ContinueOnError)
	fs.SetOutput(errOut)
	home := fs.String("home", filepath.Join(root(), "home"), "the bench HOME")
	switch args[0] {
	case "prepare":
		checkout := fs.String("checkout", filepath.Join(source(), "..", ".."), "the Jhon checkout whose release is installed")
		ref := fs.String("ref", "", "install this commit's build (a clone of the checkout at it, built in scratch) in place of the checkout's build/jhon")
		piProfile := fs.String("pi-profile", "", "a profiles.json (yours, e.g. ~/.config/jhon/profiles.json) copied to the bench HOME's ~/.config/jhon/profiles.json")
		profile := fs.String("profile", "", "the Jhon profile the bench HOME's `jhon profile use` activates after the install (every agent's model, sub-agents included)")
		piModels := fs.String("pi-models", "", "a pi models.json (yours, e.g. ~/.pi/jhon/models.json, for a provider such as NaN) copied, mode 0600, to the bench HOME's ~/.pi/jhon/models.json, which pi-jhon and its sub-agents read; never printed")
		if fs.Parse(args[1:]) != nil {
			return 2
		}
		if *piProfile != "" && *profile == "" {
			fmt.Fprintln(errOut, "bench prepare: --pi-profile needs --profile NAME, the profile to activate")
			return 2
		}
		if *piModels != "" {
			// Checked before the install, so a bad file costs nothing.
			if _, err := readPiModels(*piModels); err != nil {
				return fail(err)
			}
		}
		if err := Prepare(*home, os.Getenv("HOME"), os.Environ(), releaseInstaller{checkout: filepath.Clean(*checkout), ref: *ref}, out); err != nil {
			return fail(err)
		}
		if *piModels != "" {
			abs, _ := filepath.Abs(*home)
			if err := PutPiModels(abs, *piModels, out); err != nil {
				return fail(err)
			}
		}
		if *profile != "" {
			abs, _ := filepath.Abs(*home)
			if err := UseProfile(abs, *piProfile, *profile, os.Environ(), out); err != nil {
				return fail(err)
			}
		}
		return 0
	case "login-check":
		if fs.Parse(args[1:]) != nil {
			return 2
		}
		if ok, err := LoginCheck(*home, out); err != nil {
			return fail(err)
		} else if !ok {
			return 1
		}
		return 0
	case "certify":
		return certifyCLI(args[1:], out, errOut, nil)
	case "value":
		return valueCLI(args[1:], out, errOut)
	case "review-compare":
		return reviewCompareCLI(args[1:], out, errOut, now)
	case "report":
		if len(args) != 2 {
			fmt.Fprint(errOut, usage)
			return 2
		}
		if err := Report(args[1], out); err != nil {
			return fail(err)
		}
		return 0
	case "recheck":
		tasksDir := fs.String("tasks-dir", filepath.Join(source(), "tasks"), "where the tasks are")
		if fs.Parse(args[1:]) != nil || fs.NArg() != 1 {
			fmt.Fprint(errOut, usage)
			return 2
		}
		if err := Recheck(fs.Arg(0), *home, *tasksDir, out); err != nil {
			return fail(err)
		}
		return 0
	case "run":
		tasks := fs.String("tasks", "", "comma-separated task ids (default: all)")
		variants := fs.String("variants", strings.Join(DefaultVariants, ","), "comma-separated task variants: "+strings.Join(AllVariants, ", ")+" (clean runs plain only)")
		conds := fs.String("conditions", strings.Join(AllConditions, ","), "comma-separated conditions: "+strings.Join(KnownConditions, ", ")+" (pi-clean only when named)")
		repeats := fs.Int("repeats", 0, "runs per task and condition (default: 5 for a small task, 3 for a large one, 1 for the one-time set-up)")
		model := fs.String("model", "", "the model (default: each task's)")
		budget := fs.Float64("budget-points", 10, "stop when the weekly utilization rises more than this many points")
		results := fs.String("results", "", "this batch's results directory (default: under ~/.local/share/jhon-bench/results, named for the time and the --pi-model)")
		piModel := fs.String("pi-model", "", "pi-jhon's and pi-clean's provider/model[@thinking], e.g. zai/glm-5.3 (default: claude-bridge on --model)")
		piEnv := fs.String("pi-env", "", "comma-separated environment variable names the pi runs need (a provider's key, e.g. ZAI_API_KEY): passed by name, values never written")
		piModels := fs.String("pi-models", "", "a pi models.json copied, mode 0600, into each pi-clean run's agent dir before pi starts; pi-jhon needs the same file in the bench HOME (bench prepare --pi-models); never printed")
		var settings projectSettingsFlag
		fs.Var(&settings, "project-setting", "KEY=VALUE written into each run's LDD config before the agent starts (repeatable or comma-separated; only the variants with a config at the start: "+strings.Join(configVariants(), ", ")+"; keys: worktree_per_change)")
		allowed := fs.String("allowed-tools", DefaultAllowedTools, "--allowedTools for the Claude Code conditions")
		tasksDir := fs.String("tasks-dir", filepath.Join(source(), "tasks"), "where the tasks are")
		dry := fs.Bool("dry-run", false, "print the plan and start nothing")
		expect := fs.String("expect-commit", "", "refuse to run unless the bench HOME's jhon is this commit (7 characters or more)")
		parallel := fs.Int("parallel", 1, "how many runs execute at once (1: one after another)")
		smoke := fs.Bool("smoke", false, "one plain run, repeat 1, per task and Jhon condition of --conditions; exit 1 when one fails its hidden check or does not end with ldd/ and a record")
		debugPrompt := fs.Bool("debug-prompt", false, "keep each Jhon run's prompt dump in its prompt-dump/ (pi: CLAUDE_BRIDGE_DEBUG=1; Claude Code: --debug-file)")
		if fs.Parse(args[1:]) != nil {
			return 2
		}
		if *parallel < 1 {
			return fail(fmt.Errorf("--parallel must be 1 or more"))
		}
		if *results == "" {
			*results = filepath.Join(root(), "results", resultsName(now, *piModel, settings...))
		}
		ids := split(*tasks)
		if len(ids) == 0 {
			// A full run measures the one-time LDD set-up apart, once per
			// Jhon runtime (T146 FB2).
			all, err := listTaskIDs(*tasksDir)
			if err != nil {
				return fail(err)
			}
			ids = append(all, SetupTaskID)
		}
		ts, err := LoadTasks(*tasksDir, ids)
		if err != nil {
			return fail(err)
		}
		o := &Options{Tasks: ts, Variants: split(*variants), Conditions: split(*conds), Repeats: *repeats, Model: *model, BudgetPoints: *budget,
			Home: *home, Results: *results, AllowedTools: *allowed, DryRun: *dry, DebugPrompt: *debugPrompt, ExpectCommit: *expect, Out: out,
			PiModel: *piModel, PiEnv: split(*piEnv), PiModels: *piModels, Parallel: *parallel, Smoke: *smoke, ProjectSettings: settings}
		if _, err := Plan(o); err != nil {
			return fail(err)
		}
		if !*dry {
			if _, err := os.Stat(benchJhon(*home)); err != nil {
				return fail(fmt.Errorf("no jhon in the bench HOME %s: run `bench prepare` first", *home))
			}
			if _, err := os.Stat(*results); err == nil {
				return fail(fmt.Errorf("%s already exists", *results))
			}
		}
		if err := Execute(o); err != nil {
			if *smoke && !*dry && strings.HasPrefix(err.Error(), "smoke: ") {
				fmt.Fprintf(out, "\nresults in %s; bench report %s\n", *results, *results)
			}
			return fail(err)
		}
		if !*dry {
			fmt.Fprintf(out, "\nresults in %s; bench report %s\n", *results, *results)
		}
		return 0
	}
	fmt.Fprint(errOut, usage)
	return 2
}
