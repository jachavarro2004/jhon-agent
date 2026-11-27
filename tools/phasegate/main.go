// Command phasegate is the phase gate (T269): `measure` records what the
// owner and the agent feel of a jhon build (speed, friction in real use,
// budgets, hygiene and correctness results) as one JSON file, and `compare`
// holds a new record to a base one and fails on a regression past its
// tolerance. See README.md.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

const usage = `usage:
  phasegate measure --label <name> --jhon <binary> [--projects-file <file>] [--sessions-since <YYYY-MM-DD>] [--out <dir>]
                    [--pi-sessions <dir>] [--claude-projects <dir>] [--repo <dir>] [--jhon-root <dir>]
                    [--suite-tmp <dir>] [--result <name>=pass|fail]...
  phasegate compare --base <label> --new <label> [--out <dir>] [--markdown]
`

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "measure":
		return measure(args[1:], stdout, stderr)
	case "compare":
		return compare(args[1:], stdout, stderr)
	}
	fmt.Fprint(stderr, usage)
	return 2
}

// labelName is a record's label: a file name, never a path.
var labelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// results is the repeated --result flag: name=pass|fail.
type results map[string]string

func (r results) String() string { return "" }

func (r results) Set(v string) error {
	name, value, ok := strings.Cut(v, "=")
	if !ok || !labelName.MatchString(name) || (value != "pass" && value != "fail") {
		return fmt.Errorf("want <name>=pass or <name>=fail, not %q", v)
	}
	r[name] = value
	return nil
}

// defaultOut is $XDG_STATE_HOME/jhon-phasegate, else
// ~/.local/state/jhon-phasegate.
func defaultOut() string {
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "jhon-phasegate")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "jhon-phasegate")
}

func measure(args []string, stdout, stderr io.Writer) int {
	fail := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "phasegate measure: "+format+"\n", a...)
		return 2
	}
	home, _ := os.UserHomeDir()
	fs := flag.NewFlagSet("measure", flag.ContinueOnError)
	fs.SetOutput(stderr)
	label := fs.String("label", "", "the record's name: <out>/<label>.json")
	bin := fs.String("jhon", "", "the jhon binary measured")
	projectsFile := fs.String("projects-file", "", "one absolute project path per line; no file, no speed measure")
	sinceFlag := fs.String("sessions-since", "", "count transcripts from this date (YYYY-MM-DD); default all")
	out := fs.String("out", defaultOut(), "the folder of the records")
	piSessions := fs.String("pi-sessions", filepath.Join(home, ".pi", "jhon", "sessions"), "pi's Jhon session transcripts")
	claudeProjects := fs.String("claude-projects", filepath.Join(home, ".claude", "projects"), "Claude Code's session transcripts")
	repo := fs.String("repo", repoRoot(), "the jhon checkout whose renderer tests give the budgets")
	jhonRoot := fs.String("jhon-root", "", "the jhon tree the binary reads (default: found from the binary, else --repo)")
	suiteTmp := fs.String("suite-tmp", "", "the TMPDIR a suite run was given; its leftover entries are counted")
	res := results{}
	fs.Var(res, "result", "a correctness result, <name>=pass|fail (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		return fail("unexpected argument %q", fs.Arg(0))
	}
	if !labelName.MatchString(*label) {
		return fail("--label takes a name of letters, digits, '.', '_' and '-', not %q", *label)
	}
	if st, err := os.Stat(*bin); *bin == "" || err != nil || st.IsDir() {
		return fail("--jhon names no binary: %q", *bin)
	}
	var since time.Time
	if *sinceFlag != "" {
		t, err := time.ParseInLocation(time.DateOnly, *sinceFlag, time.Local)
		if err != nil {
			return fail("--sessions-since takes YYYY-MM-DD, not %q", *sinceFlag)
		}
		since = t
	}
	var projects []string
	if *projectsFile != "" {
		var err error
		if projects, err = readProjects(*projectsFile); err != nil {
			return fail("%v", err)
		}
	}
	var leftovers []os.DirEntry
	if *suiteTmp != "" {
		var err error
		if leftovers, err = os.ReadDir(*suiteTmp); err != nil {
			return fail("--suite-tmp: %v", err)
		}
	}
	if *jhonRoot == "" {
		*jhonRoot = findJhonRoot(*bin, *repo)
	}
	env := append(os.Environ(), "JHON_TELEMETRY=off")

	r := &Record{Label: *label, Created: time.Now().UTC().Format(time.RFC3339), Skipped: map[string]string{}}
	r.Jhon = jhonVersion(*bin, env)
	if projects != nil {
		s, err := measureSpeed(speedConfig{bin: *bin, jhonRoot: *jhonRoot, home: home, projects: projects, env: env})
		if err != nil {
			return fail("%v", err)
		}
		r.Speed = s
	} else {
		r.Skipped["speed"] = "no --projects-file"
	}
	f, err := countFriction(map[string]string{"pi": *piSessions, "claude-code": *claudeProjects}, since)
	if err != nil {
		r.Skipped["friction"] = err.Error()
	} else {
		r.Friction = f
	}
	var skip string
	if r.Budgets, skip = measureBudgets(*repo, env); skip != "" {
		r.Skipped["budgets"] = skip
	}
	if *suiteTmp != "" {
		r.Hygiene = &Hygiene{SuiteTmpLeftovers: len(leftovers)}
	} else {
		r.Skipped["hygiene"] = "no --suite-tmp"
	}
	if len(res) > 0 {
		r.Results = res
	}
	if len(r.Skipped) == 0 {
		r.Skipped = nil
	}

	body, _ := json.MarshalIndent(r, "", "  ")
	path := filepath.Join(*out, *label+".json")
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return fail("%v", err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		return fail("%v", err)
	}
	grew := summarize(stdout, r)
	fmt.Fprintln(stdout, "phasegate: wrote", path)
	if grew {
		return 1
	}
	return 0
}

// summarize prints the record in a few lines; it says whether a project's
// objects grew.
func summarize(w io.Writer, r *Record) (grew bool) {
	fmt.Fprintf(w, "phasegate: jhon %s (%s)\n", r.Jhon.Version, r.Jhon.Commit)
	if r.Speed != nil {
		for _, p := range r.Speed.Projects {
			var parts []string
			for _, c := range speedCommands {
				if t, ok := p.Commands[c]; ok {
					parts = append(parts, fmt.Sprintf("%s %s ms (max %s)", c, num(t.MedianMS), num(t.MaxMS)))
				} else if why := p.Skipped[c]; why != "" {
					parts = append(parts, fmt.Sprintf("%s skipped: %s", c, why))
				}
			}
			fmt.Fprintf(w, "phasegate: p%d: %s\n", p.Index, strings.Join(parts, ", "))
			if p.ObjectsGrew {
				grew = true
				fmt.Fprintf(w, "phasegate: p%d: objects grew (project %d -> %d, common %d -> %d): a read-only command wrote into the repository\n",
					p.Index, p.Before.Project, p.After.Project, p.Before.Common, p.After.Common)
			}
		}
	}
	if f := r.Friction; f != nil {
		var runtimes []string
		for _, rt := range []string{"claude-code", "pi"} {
			if c := f.PerRuntime[rt]; c != nil {
				runtimes = append(runtimes, fmt.Sprintf("%s %d/%d", rt, c.Stops, c.ToolResults))
			}
		}
		fmt.Fprintf(w, "phasegate: friction since %s: %d stops in %d tool results (%s per 100), %d sessions (%s); left out: jhon %d tool results, scratch %d, bridge mirrors %d\n",
			f.Since, f.Stops, f.ToolResults, num(f.Per100), f.Sessions, strings.Join(runtimes, ", "),
			f.LeftOut["jhon"].ToolResults, f.LeftOut["scratch"].ToolResults, f.LeftOut[bridgeMirror].ToolResults)
	}
	if len(r.Budgets) > 0 {
		var keys []string
		for k := range r.Budgets {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		var parts []string
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s %d/%d", k, r.Budgets[k].Value, r.Budgets[k].Limit))
		}
		fmt.Fprintf(w, "phasegate: budgets: %s\n", strings.Join(parts, ", "))
	}
	if r.Hygiene != nil {
		fmt.Fprintf(w, "phasegate: suite TMPDIR leftovers: %d\n", r.Hygiene.SuiteTmpLeftovers)
	}
	keys := make([]string, 0, len(r.Skipped))
	for k := range r.Skipped {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		fmt.Fprintf(w, "phasegate: %s skipped: %s\n", k, r.Skipped[k])
	}
	return grew
}

// readProjects reads the projects file: one absolute path per line; blank
// lines and lines starting with # are skipped.
func readProjects(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("--projects-file: %v", err)
	}
	defer f.Close()
	projects := []string{}
	s := bufio.NewScanner(f)
	for n := 1; s.Scan(); n++ {
		l := strings.TrimSpace(s.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if !filepath.IsAbs(l) {
			return nil, fmt.Errorf("--projects-file line %d is not an absolute path", n)
		}
		projects = append(projects, filepath.Clean(l))
	}
	return projects, s.Err()
}

// jhonVersion is what `jhon version --json` says.
func jhonVersion(bin string, env []string) JhonInfo {
	dir, _ := os.Getwd()
	r := timed(dir, env, nil, callTimeout, "nice", "-n", callNice, bin, "version", "--json")
	var v JhonInfo
	json.Unmarshal([]byte(r.stdout), &v)
	return v
}

// findJhonRoot is the tree the binary reads: the nearest folder above it
// holding prose/contracts/vocabulary.yaml (an install's current/build/jhon
// reads current), else repo.
func findJhonRoot(bin, repo string) string {
	if real, err := filepath.EvalSymlinks(bin); err == nil {
		for d := filepath.Dir(real); filepath.Dir(d) != d; d = filepath.Dir(d) {
			if _, err := os.Stat(filepath.Join(d, "prose", "contracts", "vocabulary.yaml")); err == nil {
				return d
			}
		}
	}
	return repo
}

// repoRoot is the nearest directory above the working one holding go.mod.
func repoRoot() string {
	dir, _ := os.Getwd()
	for d := dir; filepath.Dir(d) != d; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
	}
	return dir
}

func compare(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	fs.SetOutput(stderr)
	baseLabel := fs.String("base", "", "the base record's label")
	newLabel := fs.String("new", "", "the new record's label")
	out := fs.String("out", defaultOut(), "the folder of the records")
	markdown := fs.Bool("markdown", false, "print a Markdown table")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var recs [2]*Record
	for i, label := range []string{*baseLabel, *newLabel} {
		if !labelName.MatchString(label) {
			fmt.Fprintf(stderr, "phasegate compare: --base and --new take a record's label, not %q\n", label)
			return 2
		}
		r, err := readRecord(filepath.Join(*out, label+".json"))
		if err != nil {
			fmt.Fprintf(stderr, "phasegate compare: %v\n", err)
			return 2
		}
		recs[i] = r
	}
	rows := compareRecords(recs[0], recs[1])
	writeTable(stdout, rows, *markdown)
	worse := 0
	for _, r := range rows {
		if r.worse {
			worse++
		}
	}
	fmt.Fprintf(stdout, "\nphasegate: %d worse (base %s, jhon %s; new %s, jhon %s)\n", worse, recs[0].Label, recs[0].Jhon.Version, recs[1].Label, recs[1].Jhon.Version)
	if worse > 0 {
		return 1
	}
	return 0
}

func readRecord(path string) (*Record, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Record
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	if r.Label == "" {
		return nil, errors.New(path + ": not a phasegate record")
	}
	return &r, nil
}
