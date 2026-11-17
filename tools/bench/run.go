package main

import (
	"crypto/rand"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Options are one `bench run`.
type Options struct {
	Tasks        []Task
	Variants     []string // empty: every variant
	Conditions   []string
	Repeats      int
	Model        string // empty: each task's own
	BudgetPoints float64
	Home         string // the bench HOME
	Results      string // this batch's results directory
	AllowedTools string
	DryRun       bool
	DebugPrompt  bool
	// Parallel is how many runs execute at once; 0 and 1 are one after
	// another (T83).
	Parallel int
	// Smoke keeps one plain run, repeat 1, per task and Jhon condition of
	// Conditions; Execute fails when one fails its hidden check or the
	// plain set-up rule (T83).
	Smoke bool
	// PiModel is the pi conditions' provider/model[@thinking]; "" is
	// claude-bridge on Model (T77). PiEnv are the environment variables,
	// by name, the pi runs need (a provider's key): checked set, recorded
	// by name, their values never written anywhere.
	PiModel string
	PiEnv   []string
	// Profile is the bench HOME's active Jhon profile, "" for default
	// (T77b); Plan reads it. Without --pi-model, profileMain, the
	// profile's pi main model, is the pi conditions' model.
	Profile     string
	profileMain string
	// ExpectCommit is the jhon commit the bench HOME must have; "" checks
	// nothing. Every run records the jhon it ran either way.
	ExpectCommit string
	// ProjectSettings are KEY=VALUE settings written into each run's LDD
	// config before the agent starts (T86); only variants with a config.
	ProjectSettings []string
	Out             io.Writer
	NewID           func() string // a session id; a random UUID when nil
	claudePath      string
	piPath          string
}

// claude is the claude program the clean condition runs: the one on PATH.
func (o *Options) claude() string {
	if o.claudePath == "" {
		o.claudePath = "claude"
		if p, err := exec.LookPath("claude"); err == nil {
			o.claudePath = p
		}
	}
	return o.claudePath
}

// pi is the pi program pi-clean runs: the one on PATH.
func (o *Options) pi() string {
	if o.piPath == "" {
		o.piPath = "pi"
		if p, err := exec.LookPath("pi"); err == nil {
			o.piPath = p
		}
	}
	return o.piPath
}

// Run is one task × variant × condition × repeat.
type Run struct {
	N, Of     int
	Task      Task
	Variant   string
	Cond      Condition
	Repeat    int
	Model     string
	SessionID string
	Dir       string // the run's directory under the results
	Repo      string // the agent's working copy of the template
	Cache     string // its own XDG_CACHE_HOME
	Timeout   time.Duration
	Argv      []string
	// Next are the commands that send a multi-message run's later messages
	// on the same session (Claude Code); RPC is whether its messages go to
	// Argv's stdin instead, the first one too (pi).
	Next [][]string
	RPC  bool
	Env  []string // set over the bench env for the agent only
	// PassEnv are the names of the parent's variables a pi run gets (--pi-env).
	PassEnv []string
	// Profile is the Jhon profile of a Jhon condition's run (T77b).
	Profile string
	// ProjectSettings are written into the repo's LDD config (T86).
	ProjectSettings []string
}

// promptDump is where a --debug-prompt run keeps its prompt dump.
func (r *Run) promptDump() string { return filepath.Join(r.Dir, "prompt-dump") }

// Name is the run's directory name.
func (r *Run) Name() string {
	return fmt.Sprintf("%s-%s-%s-%d", r.Task.ID, r.Variant, r.Cond.Name(), r.Repeat)
}

// uuid4 is a random version 4 UUID.
func uuid4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// variants are o's variants, every one when none is named.
func (o *Options) variants() []string {
	if len(o.Variants) == 0 {
		return DefaultVariants
	}
	return o.Variants
}

// Plan is every run of o, repeat-major: repeat 1 of every task × variant ×
// condition, then repeat 2, so a budget stop still leaves balanced data.
// clean runs only the plain variant.
func Plan(o *Options) ([]*Run, error) {
	if o.Repeats < 1 {
		return nil, fmt.Errorf("--repeats must be 1 or more")
	}
	if o.Parallel < 0 {
		return nil, fmt.Errorf("--parallel must be 1 or more")
	}
	if len(o.Tasks) == 0 || len(o.Conditions) == 0 {
		return nil, fmt.Errorf("no tasks or no conditions")
	}
	if o.Smoke {
		// One plain run, repeat 1, per task and Jhon condition (L601).
		o.Conditions = slices.DeleteFunc(slices.Clone(o.Conditions), func(c string) bool { return !isJhon(c) })
		if len(o.Conditions) == 0 {
			return nil, fmt.Errorf("--smoke runs the Jhon conditions (jhon, pi-jhon): name one in --conditions")
		}
		o.Variants, o.Repeats = []string{"plain"}, 1
	}
	if o.PiModel != "" {
		if err := parsePiModel(o.PiModel); err != nil {
			return nil, err
		}
	}
	if len(o.ProjectSettings) > 0 {
		if err := checkProjectSettings(o.ProjectSettings); err != nil {
			return nil, err
		}
		if err := refuseWithoutConfig(o.variants()); err != nil {
			return nil, err
		}
	}
	for _, n := range o.PiEnv {
		if !envName.MatchString(n) {
			return nil, fmt.Errorf("--pi-env %q: give environment variable names, comma-separated", n)
		}
	}
	if o.Profile == "" {
		o.Profile = activeProfile(o.Home)
	}
	if o.Profile != "" && o.PiModel == "" && slices.ContainsFunc(o.Conditions, isPi) {
		main, err := profileMainModel(o.Home)
		if err != nil {
			return nil, err
		}
		o.profileMain = main
	}
	newID := o.NewID
	if newID == nil {
		newID = uuid4
	}
	for _, v := range o.variants() {
		if err := checkVariant(v); err != nil {
			return nil, err
		}
	}
	var runs []*Run
	for rep := 1; rep <= o.Repeats; rep++ {
		for _, t := range o.Tasks {
			for _, v := range o.variants() {
				for _, name := range o.Conditions {
					c, err := conditionNamed(name)
					if err != nil {
						return nil, err
					}
					if !runsVariant(name, v) {
						continue
					}
					task := t
					if v == "ldd-ask" {
						task = asked(t)
					}
					r := &Run{Task: task, Variant: v, Cond: c, Repeat: rep, Model: cmpOr(o.Model, t.Model), SessionID: newID(), Timeout: t.Timeout}
					if isPi(name) {
						r.Model = cmpOr(o.PiModel, o.profileMain, bridgeModel(r.Model))
						r.PassEnv = o.PiEnv
					}
					if isJhon(name) {
						r.Profile = o.Profile
					}
					r.ProjectSettings = o.ProjectSettings
					r.Dir = filepath.Join(o.Results, "runs", r.Name())
					r.Repo, r.Cache = filepath.Join(r.Dir, "repo"), filepath.Join(r.Dir, "cache")
					r.Argv = c.Argv(r, o)
					for n := 2; n <= len(t.Prompts); n++ {
						if next := c.Resume(r, o, n); next != nil {
							r.Next = append(r.Next, next)
						} else {
							r.RPC = true
						}
					}
					if o.DebugPrompt && name == "pi-jhon" {
						r.Env = []string{"CLAUDE_BRIDGE_DEBUG=1", "CLAUDE_BRIDGE_DEBUG_PATH=" + filepath.Join(r.promptDump(), "claude-bridge.log")}
					}
					if pc, ok := c.(piClean); ok {
						r.Env = append(r.Env, pc.env(r)...)
					}
					runs = append(runs, r)
				}
			}
		}
	}
	if len(runs) == 0 {
		return nil, fmt.Errorf("no runs: clean and pi-clean run the plain variant only")
	}
	for i, r := range runs {
		r.N, r.Of = i+1, len(runs)
	}
	return runs, nil
}

// envName is an environment variable's name.
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// passEnv is NAME=value of each of names from the parent environment: only
// the agent's environment gets it, never a file or the output.
func passEnv(names []string) []string {
	var env []string
	for _, n := range names {
		env = append(env, n+"="+os.Getenv(n))
	}
	return env
}

func cmpOr(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// stripped are the variables a run never inherits: they would point it at
// the developer's checkout, settings, Claude config or pi agent dir, or turn
// on the bridge's debug log outside --debug-prompt. Every ENGRAM_ variable
// goes too: one could name the owner's store, port or cloud.
var stripped = []string{"JHON_ROOT", "JHON_SETTINGS", "CLAUDE_CONFIG_DIR", "PI_CODING_AGENT_DIR", "PI_CODING_AGENT_SESSION_DIR",
	"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "CLAUDE_BRIDGE_DEBUG", "CLAUDE_BRIDGE_DEBUG_PATH"}

// benchEnv is base with the bench HOME in place of the owner's: HOME, the
// XDG directories under it (the cache given), the bench HOME's bin first on
// PATH, engram's store in the bench HOME, and nothing that names the
// owner's Jhon, Claude or engram configuration.
func benchEnv(base []string, home, cache string) []string {
	var env []string
	path := ""
	for _, kv := range base {
		k, v, _ := strings.Cut(kv, "=")
		switch {
		case k == "PATH":
			path = v
		case k == "HOME" || slices.Contains(stripped, k) || strings.HasPrefix(k, "ENGRAM_"):
		default:
			env = append(env, kv)
		}
	}
	return append(env, "HOME="+home, "PATH="+filepath.Join(home, ".local", "bin")+":"+path,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME="+filepath.Join(home, ".local", "state"), "XDG_CACHE_HOME="+cache, "ENGRAM_DATA_DIR="+engramStore(cache))
}

// shellQuote is s quoted for a POSIX shell when it needs it.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./,=:@") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func commandLine(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = shellQuote(a)
	}
	return strings.Join(q, " ")
}

// printPlan writes what Execute would do.
func printPlan(o *Options, runs []*Run) {
	fmt.Fprintf(o.Out, "plan: %d runs, budget: %g points of weekly utilization, results: %s\n", len(runs), o.BudgetPoints, o.Results)
	if pis := slices.DeleteFunc(slices.Clone(o.Conditions), func(c string) bool { return !isPi(c) }); len(pis) > 0 && (o.PiModel != "" || len(o.PiEnv) > 0) {
		fmt.Fprintf(o.Out, "pi model: %s (%s); passed from the environment: %s\n", cmpOr(o.PiModel, BridgeProvider+" on --model"), strings.Join(pis, ", "), cmpOr(strings.Join(o.PiEnv, ", "), "none"))
	}
	printProfile(o)
	if len(o.ProjectSettings) > 0 {
		fmt.Fprintf(o.Out, "project settings: %s (written to each run's LDD config before the agent starts: %s)\n", strings.Join(o.ProjectSettings, ","), strings.Join(o.variants(), ", "))
	}
	if o.Parallel > 1 {
		fmt.Fprintf(o.Out, "parallel: up to %d runs at once; each run's lines are whole, their order follows the runs' finishes\n", o.Parallel)
	}
	if o.Smoke {
		fmt.Fprintln(o.Out, "smoke: one plain run per task and Jhon condition, repeat 1; it fails when a run fails its hidden check or does not end with ldd/ and a record")
	}
	fmt.Fprintf(o.Out, "variants: %s; repeat-major, so a budget stop leaves balanced data\n", strings.Join(o.variants(), ", "))
	if slices.Contains(o.Conditions, "clean") && slices.ContainsFunc(o.variants(), func(v string) bool { return !runsVariant("clean", v) }) {
		fmt.Fprintln(o.Out, "clean runs the plain variant only (it ignores openspec)")
	}
	for _, r := range runs {
		fmt.Fprintf(o.Out, "\nrun %d/%d %s (timeout %s, max turns %d)\n  dir: %s\n  env: %s\n  cwd: %s\n  $ %s\n  then: %s in the repo; jhon audit <transcript> --json\n",
			r.N, r.Of, r.Name(), r.Timeout, r.Task.MaxTurns, r.Dir,
			strings.Join(append([]string{"HOME=" + o.Home, "XDG_CACHE_HOME=" + r.Cache, "ENGRAM_DATA_DIR=" + engramStore(r.Cache)}, r.Env...), " "),
			r.Repo, commandLine(r.Argv), r.Task.Check)
		if len(r.PassEnv) > 0 {
			fmt.Fprintf(o.Out, "  pass env: %s (names only, from the parent environment)\n", strings.Join(r.PassEnv, ","))
		}
		if n := len(r.Task.Prompts); n > 1 {
			fmt.Fprintf(o.Out, "  messages: %d; the timeout and max turns cover them all (a later call's --max-turns becomes the turns left)\n", n)
		}
		if r.RPC {
			fmt.Fprintf(o.Out, "  messages 1-%d: a prompt record each on stdin, the next after agent_settled\n", len(r.Task.Prompts))
		}
		for i, next := range r.Next {
			fmt.Fprintf(o.Out, "  message %d: $ %s\n", i+2, commandLine(next))
		}
		for i, c := range r.Task.MessageChecks {
			if c != "" {
				fmt.Fprintf(o.Out, "  check %d: %s, after message %d and each later one, on a copy with the hidden test\n", i+1, c, i+1)
			}
		}
	}
}

// points is a utilization change in points, to two decimals.
func points(from, to float64) float64 { return math.Round((to-from)*100*100) / 100 }

// Execute runs the plan, or prints it on a dry run. It starts no run once
// the weekly utilization has risen more than BudgetPoints since the first
// reading of the batch; runs already started finish.
//
// With Parallel N > 1, up to N runs execute at once. What the runs share is
// written before the first one starts (the engram MCP config) or only read
// (the bench HOME's jhon, settings and pi agent dir); each run has its own
// directory, repo, cache, engram store and session id, and Claude Code's and
// pi's session files are named by the run's repo and session id. Output
// lines are written whole under one lock, so they never interleave; a run's
// result line names it, since results come in the order runs finish.
func Execute(o *Options) error {
	runs, err := Plan(o)
	if err != nil {
		return err
	}
	if o.DryRun {
		printPlan(o, runs)
		return nil
	}
	for _, n := range o.PiEnv {
		if _, ok := os.LookupEnv(n); !ok {
			return fmt.Errorf("--pi-env %s is not set in the environment", n)
		}
	}
	if slices.Contains(o.Conditions, "jhon") {
		if err := writeEngramMCPConfig(o.Home); err != nil {
			return err
		}
	}
	jhon, err := checkCommit(o.Home, o.ExpectCommit)
	if err != nil {
		return err
	}
	printProfile(o)
	if len(o.ProjectSettings) > 0 {
		fmt.Fprintf(o.Out, "project settings: %s (written to each run's LDD config before the agent starts: %s)\n", strings.Join(o.ProjectSettings, ","), strings.Join(o.variants(), ", "))
	}
	parallel := max(o.Parallel, 1)
	var (
		mu          sync.Mutex
		wg          sync.WaitGroup
		first, last *float64
		runErr      error
		done        = make([]*Metrics, len(runs))
		slots       = make(chan struct{}, parallel)
	)
	finish := func(r *Run, m *Metrics, err error) {
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			if runErr == nil {
				runErr = fmt.Errorf("%s: %v", r.Name(), err)
			}
			return
		}
		m.JhonCommit, m.JhonVersion = jhon.Commit, jhon.Version
		if m.Utilization != nil {
			if last != nil {
				p := points(*last, *m.Utilization)
				m.QuotaPoints = &p
			}
			if first == nil {
				first = m.Utilization
			}
			last = m.Utilization
		}
		if err := writeJSON(filepath.Join(r.Dir, "metrics.json"), m); err != nil {
			if runErr == nil {
				runErr = err
			}
			return
		}
		done[r.N-1] = m
		result := fmt.Sprintf("success=%t turns=%d output_tokens=%g wall=%s%s\n", m.Success, m.Turns, m.OutputTokens,
			time.Duration(m.WallMs)*time.Millisecond, problems(m))
		if parallel > 1 {
			fmt.Fprintf(o.Out, "run %d/%d %s: %s", r.N, r.Of, r.Name(), result)
		} else {
			fmt.Fprint(o.Out, "  "+result)
		}
	}
launch:
	for _, r := range runs {
		slots <- struct{}{}
		mu.Lock()
		switch {
		case runErr != nil:
			mu.Unlock()
			break launch
		case first != nil && last != nil && points(*first, *last) > o.BudgetPoints:
			fmt.Fprintf(o.Out, "budget: weekly utilization rose %g points since the first run (%g%% to %g%%), over the %g-point limit; stopping before run %d of %d\n",
				points(*first, *last), math.Round(*first*100), math.Round(*last*100), o.BudgetPoints, r.N, r.Of)
			mu.Unlock()
			break launch
		}
		fmt.Fprintf(o.Out, "run %d/%d %s ...\n", r.N, r.Of, r.Name())
		mu.Unlock()
		wg.Add(1)
		go func(r *Run) {
			defer wg.Done()
			m, err := execute(o, r)
			finish(r, m, err)
			<-slots
		}(r)
	}
	wg.Wait()
	if runErr != nil || !o.Smoke {
		return runErr
	}
	return smokeVerdict(runs, done)
}

// smokeVerdict is nil when every smoke run passed its hidden check and, a
// Jhon plain run, ended with ldd/ and a record (the certification's "plain
// sets up ldd with a record"); else the runs that did not, and why.
func smokeVerdict(runs []*Run, done []*Metrics) error {
	var failed []string
	ran := 0
	for i, r := range runs {
		m := done[i]
		if m == nil {
			continue
		}
		ran++
		var why []string
		if !m.Success {
			why = append(why, "hidden check failed")
		}
		if setsUpLDD(r.Cond.Name(), r.Variant) && (m.LddLayout == nil || m.LddLayout.Layout != "ldd" || m.LddLayout.Record == nil || !*m.LddLayout.Record) {
			why = append(why, "plain set-up: no ldd/ with a record")
		}
		if len(why) > 0 {
			failed = append(failed, fmt.Sprintf("%s (%s)", r.Name(), strings.Join(why, "; ")))
		}
	}
	switch {
	case len(failed) > 0:
		return fmt.Errorf("smoke: %d of %d runs failed: %s", len(failed), ran, strings.Join(failed, ", "))
	case ran < len(runs):
		return fmt.Errorf("smoke: only %d of %d runs ran (budget)", ran, len(runs))
	}
	return nil
}

func problems(m *Metrics) string {
	if len(m.Problems) == 0 {
		return ""
	}
	return " problems: " + strings.Join(m.Problems, "; ")
}

// writeJSON writes v indented to file.
func writeJSON(file string, v any) error {
	b, err := jsonIndent(v)
	if err != nil {
		return err
	}
	return os.WriteFile(file, b, 0o644)
}
