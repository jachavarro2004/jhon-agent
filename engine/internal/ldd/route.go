package ldd

import (
	"cmp"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	"github.com/jachavarro2004/jhon-agent/engine/internal/discover"
	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/requirements"
)

// This file is the route a change takes, decided from the change itself
// (contracts/ldd.md, "Slices"). The record is never the route's: every
// change that alters behaviour leaves its record (Documented). What the
// route sets is the rigor. A change is `standard` by default, delivered with
// its affected tests; one at the verified tier (risk `high` by default) is
// `verified`, with an independent verification before delivery, and status
// suggests a proposal and a design when it spans more than one project. Size
// alone never makes a change verified: one adding more than the advisory
// size (400 lines of code by default: outside openspec/, tests, and
// documents) gets a note. The thresholds are the project's
// (`route_verified_tier`; `route_verified_lines`, read as the advisory size
// and no longer forcing verify; `route_design_lines` in
// openspec/config.yaml). The guards hold every agent to the route
// (hook/routeguard.go); review_required in openspec/config.yaml asks a
// change for verify on any route.

// The routes.
const (
	RouteStandard = "standard"
	RouteVerified = "verified"
)

// Route is a change's route and what decided it.
type Route struct {
	Level string `json:"level"`
	Tier  Level  `json:"tier"`
	// Lines are the lines the change adds in code: outside openspec/ and
	// the files `jhon risk` reads as passive (tests and documents). Only
	// they count toward VerifiedLines.
	Lines int `json:"lines"`
	// Projects are the projects the change touches, outside openspec/.
	Projects []string `json:"projects"`
	// Spans are the projects where the change adds at least DesignLines
	// lines outside its tests: a few seed rows in another project are a
	// touch, not a span.
	Spans []string `json:"spans"`
	// Design is whether the change spans enough to warrant a proposal and a design.
	Design bool `json:"design"`
	// Behaviour is whether the change alters behaviour: a file outside
	// openspec/ that `jhon risk` reads above passive (not a test or a
	// document). Such a change opens its PR only with its record.
	Behaviour    bool     `json:"behaviour"`
	Reasons      []string `json:"reasons"`
	VerifiedTier Level    `json:"verified_tier"`
	// VerifiedLines is the advisory size of a change and of a slice
	// (`route_verified_lines`, 400 by default): over it is a note, never
	// the verified route.
	VerifiedLines int `json:"verified_lines"`
	DesignLines   int `json:"design_lines"`
	// Facets are the Prism facets the diff calls for: a Prism review runs only these.
	Facets []string `json:"facets"`
	// Slice is the size of the slice in progress, set by status (SliceLinesOf).
	Slice *SliceLines `json:"slice,omitempty"`
	// Reviews are the reviews the selected change owes, set by status
	// (ReviewsOf): verify on this route, Prism review when its risk reaches the
	// verified tier, and what review_required names for it.
	Reviews []Review `json:"reviews,omitempty"`
	// risk is the reasons `jhon risk` gives the change's files, what
	// codeLines reads as passive.
	risk []Reason
	// files are the files the change touches outside openspec/, what a
	// review_required area matches.
	files []string
}

// Defaults of the route's thresholds.
const (
	defaultVerifiedTier  = High
	defaultVerifiedLines = 400
	defaultDesignLines   = 100
)

// RouteOf is the route of the change in the repository holding dir, against
// the default branch's merge base. The error is a config that exists and
// cannot be read.
func RouteOf(dir string) (Route, error) {
	return gitReads{}.routeOf(dir)
}

func (g gitReads) routeOf(dir string) (Route, error) {
	risk, err := g.riskOf(dir, nil)
	if err != nil {
		return Route{}, err
	}
	top, _ := g.git(dir, "rev-parse", "--show-toplevel")
	top = js.Trim(top)
	config := ""
	if b, err := project.ReadLddConfig(top); err == nil {
		config = b
	}
	r := Route{Level: RouteStandard, Tier: risk.Tier, Projects: []string{}, Spans: []string{}, Reasons: []string{},
		VerifiedTier: defaultVerifiedTier, VerifiedLines: defaultVerifiedLines, DesignLines: defaultDesignLines}
	if t := Level(contract.Scalar(config, "route_verified_tier")); rank(t) >= 0 {
		r.VerifiedTier = t
	}
	if n, err := strconv.Atoi(contract.Scalar(config, "route_verified_lines")); err == nil && n > 0 {
		r.VerifiedLines = n
	}
	r.Behaviour = slices.ContainsFunc(risk.Reasons, func(x Reason) bool { return x.Level != Passive && !layout.For(top).Ignored(x.File) })
	if n, err := strconv.Atoi(contract.Scalar(config, "route_design_lines")); err == nil && n > 0 {
		r.DesignLines = n
	}
	r.risk = risk.Reasons
	r.Facets = risk.Facets
	var files []string
	added := map[string]int{}
	if base, _, ok := g.baseOf(top, nil); ok && top != "" {
		files, added = g.addedOutsideRecord(top, base)
		r.Lines = codeLines(added, r.risk)
		r.files = files
	}
	var projects []string
	if top != "" {
		if resolved, err := discover.ProjectsFor(top); err == nil {
			for _, p := range resolved.Projects {
				projects = append(projects, p.Path)
			}
		}
	}
	code := map[string]int{}
	for _, f := range files {
		p := deepest(f, projects)
		if p == "" {
			continue
		}
		if !slices.Contains(r.Projects, p) {
			r.Projects = append(r.Projects, p)
		}
		if !testRe.MatchString(f) {
			code[p] += added[f]
		}
	}
	slices.Sort(r.Projects)
	for _, p := range r.Projects {
		if code[p] >= r.DesignLines {
			r.Spans = append(r.Spans, p)
		}
	}
	if rank(r.Tier) >= rank(r.VerifiedTier) {
		r.Level = RouteVerified
		r.Reasons = append(r.Reasons, fmt.Sprintf("risk %s reaches the verified tier (%s)", r.Tier, r.VerifiedTier))
	}
	if r.Level == RouteStandard {
		r.Reasons = append(r.Reasons, fmt.Sprintf("risk %s is under the verified tier (%s)", r.Tier, r.VerifiedTier))
		if r.Lines > r.VerifiedLines {
			r.Reasons = append(r.Reasons, fmt.Sprintf("%d added lines of code are over the advisory size (%d): size alone asks for no verify", r.Lines, r.VerifiedLines))
		}
	}
	r.Design = r.Level == RouteVerified && len(r.Spans) > 1
	return r, nil
}

// addedOutsideRecord is the files changed against base outside openspec/,
// untracked ones included, and the lines added to each.
func addedOutsideRecord(top, base string) (files []string, added map[string]int) {
	return gitReads(nil).addedOutsideRecord(top, base)
}

func (g gitReads) addedOutsideRecord(top, base string) (files []string, added map[string]int) {
	added = map[string]int{}
	numstat, _ := g.git(top, "diff", "--numstat", "--no-renames", base)
	for _, l := range nonEmpty(numstat) {
		f := strings.SplitN(l, "\t", 3)
		if len(f) < 3 || layout.For(top).Ignored(f[2]) {
			continue
		}
		files = append(files, f[2])
		if n, err := strconv.Atoi(f[0]); err == nil {
			added[f[2]] += n
		}
	}
	untracked, _ := g.git(top, "ls-files", "--others", "--exclude-standard")
	for _, f := range nonEmpty(untracked) {
		if layout.For(top).Ignored(f) {
			continue
		}
		files = append(files, f)
		if b, err := os.ReadFile(filepath.Join(top, f)); err == nil && len(b) <= 1024*1024 {
			added[f] += strings.Count(string(b), "\n")
		}
	}
	return files, added
}

// codeLines are the lines of added (file: lines) in code: in the files
// reasons do not read as passive, and, for a file they do not name, the
// default rules do not.
func codeLines(added map[string]int, reasons []Reason) int {
	passive := map[string]bool{}
	for _, x := range reasons {
		passive[x.File] = x.Level == Passive
	}
	lines := 0
	for f, n := range added {
		if p, read := passive[f]; !p && (read || defaultLevel(Changed{File: f}).Level != Passive) {
			lines += n
		}
	}
	return lines
}

// deepest is the deepest project holding f, "" for none; "." holds every file.
func deepest(f string, projects []string) string {
	best := ""
	for _, p := range projects {
		if (p == "." || f == p || strings.HasPrefix(f, p+"/")) && (best == "" || best == "." || len(p) > len(best)) {
			best = p
		}
	}
	return best
}

// String is the route as a line reads it.
func (r Route) String() string {
	s := r.Level + ": " + strings.Join(r.Reasons, "; ")
	if r.Level == RouteVerified {
		s += "; an independent verification before delivery"
		if r.Design {
			s += fmt.Sprintf(", and it adds %d or more lines outside tests in %d projects (%s), so a proposal and a design are written too", r.DesignLines, len(r.Spans), strings.Join(r.Spans, ", "))
		} else {
			s += ", no proposal or design"
		}
	} else {
		s += "; its affected tests deliver it"
	}
	switch {
	case r.Behaviour && OneLineRecord(r.Tier):
		s += "; it alters behaviour, so its record goes with it: its RED and GREEN runs in `jhon executions`, tasks.md only when the work cannot be resumed from the diff (over the advisory lines of code, risk high, or more than one component; below high risk a one-line `## Intent` and `Delivers:` is enough), and a spec delta only when a main spec changes or is added"
	case r.Behaviour:
		s += "; it alters behaviour, so its record goes with it: its RED and GREEN runs in `jhon executions`, tasks per slice (risk high), and a spec delta only when a main spec changes or is added"
	}
	return s
}

// initSteps is the init steps under an install's root: the file a session
// asked for LDD follows by reading it, as it may have no Skill tool to run
// /jhon-ldd init (L506).
const initSteps = "prose/skills/ldd/references/init.md"

// InactiveLineFor is what a main session is told, on Claude Code and pi
// alike, where ProjectBrief has no rules: LDD is not set up there yet. Jhon
// always works under LDD (T72, L555, L559): read-only work sets up nothing;
// at the first code change the session sets it up, asking the owner once
// how to deliver and test through its ask tool only, never in prose (T74,
// L572), and route-guard refuses a commit or a PR until
// it is set up (L557). It names the init steps by their absolute path under
// root, the install the hook runs from, so no agent expands ~ to a home it
// guesses (L534); where root has no init.md it names them as all runtime
// prose does, under ~/.local/share/jhon/current.
func InactiveLineFor(root string) string {
	return "LDD is not set up in this project yet (`jhon status`); read-only work sets up nothing." +
		" At your first code change, set it up by following `" + initStepsFor(root) + "`: ask how to deliver and test by your ask tool only (else the recommended values, never asked in prose)," +
		" say in one line what you set up, then work under LDD on the change's own branch. Until it is set up, a commit or a PR is refused."
}

// SetUpRefusal is route-guard's refusal of a commit or a PR in a
// repository with no layout (T72, L557), naming the init steps of the
// running install (contract.Root) as InactiveLineFor does.
func SetUpRefusal() string {
	root, _ := contract.Root()
	return "LDD is not set up in this project: a commit or a PR delivers code only under LDD." +
		" Set it up first by following `" + initStepsFor(root) + "`, say in one line what you set up, then commit."
}

// SetUpEditRefusal is route-guard's refusal of the first code edit in a
// repository with no layout (T89, L646): any model sets LDD up before it
// changes code, whether or not it read the session line. It names the same
// init steps InactiveLineFor and SetUpRefusal do.
func SetUpEditRefusal() string {
	root, _ := contract.Root()
	return "refused: LDD is not set up in this project yet; set it up first by following `" + initStepsFor(root) +
		"` (ask how to deliver and test with your ask tool, or use the recommended values), then make this edit and, before you finish, start its change (tasks.md) and record a run (`jhon executions run`)"
}

// initStepsFor is the init steps' path under root, else the ~ text.
func initStepsFor(root string) string {
	if root != "" {
		if p := filepath.Join(root, initSteps); filepath.IsAbs(p) && isFile(p) {
			return p
		}
	}
	return "~/.local/share/jhon/current/" + initSteps
}

// OldSwitchNotice is what the sessions of a project the retired `jhon ldd
// off` left an off record for are told, once (T72, L555).
const OldSwitchNotice = "LDD is always on with Jhon now; the old switch-off no longer applies."

// OldSwitchUntold is whether the sessions working in dir are still to be
// told OldSwitchNotice, and the repository's toplevel, which
// project.TellOldSwitchOff takes once they are.
func OldSwitchUntold(dir string) (string, bool) {
	top, _ := riskGit(dir, "rev-parse", "--show-toplevel")
	top = js.Trim(top)
	return top, top != "" && project.OldSwitchOffUntold(top)
}

// InactiveFor is the line a session working in dir is told where
// ProjectBrief has no rules, naming the init steps of the running install
// (contract.Root: JHON_ROOT, else the release or checkout holding the binary).
func InactiveFor(dir string) string {
	root, _ := contract.Root()
	return InactiveLineFor(root)
}

// isFile is whether p names a regular file.
func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// ProjectBrief is the rules a session working in the repository holding
// dir is held to, read from its openspec/config.yaml and its change; "" when
// it has no config.
func ProjectBrief(dir string) string {
	top, _ := riskGit(dir, "rev-parse", "--show-toplevel")
	top = js.Trim(top)
	b, err := project.ReadLddConfig(top)
	if err != nil {
		return ""
	}
	config := b
	lines := []string{"## This project's rules (from " + layout.For(top).Config() + "; the guards enforce them)", ""}
	if DeliveryUnit(top) == "change" {
		lines = append(lines, "- Delivery: `delivery: change`. One PR per change: each slice is a commit on the change's branch; the PR is opened after the last slice, and merged with `gh pr merge <n> --rebase --delete-branch`. A PR with unchecked tasks, or a merge without --rebase, is refused.")
	} else {
		lines = append(lines, "- Delivery: each slice is delivered on its own.")
	}
	if r, err := RouteOf(top); err == nil {
		record := "its record, its RED and GREEN runs in `jhon executions`, tasks per slice, and a spec delta only when a main spec changes or is added,"
		if OneLineRecord(r.Tier) {
			record = "its record (its RED and GREEN runs in `jhon executions`; a small change (`jhon status` says so) " + SmallKeepsNo + "; else tasks.md only when the work cannot be resumed from the diff, below high risk a one-line `## Intent` and `Delivers:`; a spec delta only when a main spec changes or is added),"
		}
		lines = append(lines, "- Route, from the change's diff now: "+r.String()+". The method is LDD (contracts/ldd.md): every change that alters behaviour carries "+record+" and a PR without it is refused; the route sets only the rigor, which rises with the diff's risk (size alone is a note); a proposal or design is written when the change needs one or the owner asks, verify may always run, and a Prism review the route does not ask for is asked of the owner.")
	}
	lines = append(lines,
		"- Reviews by risk (contracts/review-prism.md): a change at the verified tier gets jhon-checker and an automatic Prism review with only the facets it touches, recorded in review-prism.md (`facets:`, `verdict: pass`); `review_required` in this file always wins.",
		"- This file and the framework's contracts outrank every memory: a memory that contradicts them is stale; correct it, never follow it.")
	if v := contract.Scalar(config, "executions_inert"); v != "" {
		lines = append(lines, "- executions_inert: "+v)
	}
	if v := contract.Scalar(config, "executions_always"); v != "" {
		lines = append(lines, "- executions_always: "+v)
	}
	if v := contract.Scalar(config, "executions_data_access"); v != "" {
		lines = append(lines, "- executions_data_access: "+v+"; executions_database_tests: "+cmp.Or(contract.Scalar(config, "executions_database_tests"), "(none)")+". A slice commit that changes a file under a data-access path needs its recorded run to have executed a real-database test whose pytest-testmon record reaches that file; a test that compiles the query against a fake session does not.")
	}
	if contract.Scalar(config, "docs_with_code") == "true" {
		lines = append(lines, "- docs_with_code: a project with docs/ updates them with its code before the PR, or its tasks.md states `No docs change: <why>`.")
	}
	if on, _ := WorktreePerChange(config, layout.For(top).Config()); on {
		lines = append(lines, "- Worktree per change: `worktree_per_change: true`. Each change works in its own git worktree outside the repository (`<repo>-worktrees/<change>`) on its branch: create it once, then start each command with `cd <its absolute path> &&` (the shell may reset the directory between calls) and nothing else repeated; run RED once. A change building on another's undelivered work branches from that change's branch, and the final message names the branch holding the whole delivery. Delivery puts the work on the default branch (merged locally when there is no remote or PR flow); after delivery and archive, `git worktree remove <that path>` and delete the merged branch: never remove the worktrees' container or anything outside the change's own worktree. Handoff files go outside the repository.")
	}
	return strings.Join(lines, "\n")
}

// BranchChanges are the changes the branch in the repository holding dir
// carries against the default branch's merge base: active ones (under
// openspec/changes/<change>/) and archived ones (their archive directory's
// name), each once, in path order.
func BranchChanges(dir string) (active, archived []string) {
	top, _ := riskGit(dir, "rev-parse", "--show-toplevel")
	top = js.Trim(top)
	base, _, ok := baseOf(top, nil)
	if top == "" || !ok {
		return nil, nil
	}
	changes := layout.For(top).ChangesDir()
	names, _ := riskGit(top, "diff", "--name-only", "--no-renames", base, "--", changes)
	untracked, _ := riskGit(top, "ls-files", "--others", "--exclude-standard", "--", changes)
	for _, f := range append(nonEmpty(names), nonEmpty(untracked)...) {
		parts := strings.Split(strings.TrimPrefix(f, changes+"/"), "/")
		switch {
		case len(parts) > 2 && parts[0] == "archive":
			if !slices.Contains(archived, parts[1]) {
				archived = append(archived, parts[1])
			}
		case len(parts) > 1 && parts[0] != "archive":
			if !slices.Contains(active, parts[0]) {
				active = append(active, parts[0])
			}
		}
	}
	return active, archived
}

// Documented is whether a change directory holds its record for a change
// at risk tier: tasks.md and a spec delta, or tasks.md stating why no spec
// changes (a line starting `No spec change:`, for a change whose behaviour
// stays, such as a refactor). Below risk high (OneLineRecord) tasks.md with
// an `## Intent` line and a `Delivers:` line is complete too: a spec delta
// is due only when a scenario changes.
func Documented(l layout.Layout, changeDir string, tier Level) bool {
	b, err := os.ReadFile(filepath.Join(changeDir, l.Tasks()))
	if err != nil {
		return false
	}
	if st, err := os.Stat(filepath.Join(changeDir, l.DeltaSpecs())); err == nil && st.IsDir() {
		return true
	}
	text := string(b)
	return noSpecChange.MatchString(text) || (OneLineRecord(tier) && oneLine(text))
}

// Recorded is whether change, its directory changeDir, carries its record
// on route r with needs n (NeedsOf, T31): a change that needs neither
// tasks.md nor a spec delta is recorded by its RED before GREEN in
// `jhon executions` (execution.RedBeforeGreen), a change with no folder
// included; any other by Documented.
func Recorded(top, change, changeDir string, r Route, n Needs) bool {
	if !n.Tasks && !n.Spec {
		return execution.RedBeforeGreen(top, change)
	}
	return Documented(layout.For(top), changeDir, r.Tier)
}

// OneLineRecord is whether a change at risk tier (passive or medium) may
// keep its record in one line of tasks.md, `## Intent` and `Delivers:`.
func OneLineRecord(tier Level) bool { return rank(tier) >= 0 && rank(tier) < rank(High) }

// oneLine is whether tasks.md src has an `## Intent` heading and a
// `Delivers:` line naming what the change delivers.
func oneLine(src string) bool {
	intent, delivers := false, false
	for _, l := range unfenced(src) {
		if h := requirements.HeadingOf(l); h != nil && h.Level == 2 && h.Text == "Intent" {
			intent = true
		}
		delivers = delivers || deliversText.MatchString(l.Text)
	}
	return intent && delivers
}

var noSpecChange = lazyre.New(`(?m)^\s*(?:[-*]\s+)?No spec change:\s*\S`)

// UndocumentedProjects are the projects of the change in the repository
// whose top level is top that change code but not their own docs/, when
// openspec/config.yaml sets `docs_with_code: true`: a project that keeps
// its documentation in docs/ updates it with the code it documents
// (contracts/ldd.md, "Delivery"). A project with no docs/ directory, a
// change to tests or documents alone, and a change whose tasks.md states
// `No docs change: <why>` are left out.
func UndocumentedProjects(top string) []string {
	b, err := project.ReadLddConfig(top)
	if err != nil || contract.Scalar(b, "docs_with_code") != "true" {
		return nil
	}
	base, _, ok := baseOf(top, nil)
	if !ok {
		return nil
	}
	active, _ := BranchChanges(top)
	l := layout.For(top)
	for _, c := range active {
		if t, err := os.ReadFile(layout.At(top, l.Artifact(c, l.Tasks()))); err == nil && noDocsChange.Match(t) {
			return nil
		}
	}
	resolved, err := discover.ProjectsFor(top)
	if err != nil {
		return nil
	}
	var projects []string
	for _, p := range resolved.Projects {
		projects = append(projects, p.Path)
	}
	files, _ := addedOutsideRecord(top, base)
	code, documented := map[string]bool{}, map[string]bool{}
	for _, f := range files {
		p := deepest(f, projects)
		if p == "" || p == "." {
			continue
		}
		switch {
		case strings.HasPrefix(f, p+"/docs/"):
			documented[p] = true
		case !testRe.MatchString(f) && defaultLevel(Changed{File: f}).Level != Passive:
			code[p] = true
		}
	}
	var out []string
	for _, p := range projects {
		if st, err := os.Stat(filepath.Join(top, p, "docs")); code[p] && !documented[p] && err == nil && st.IsDir() {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}

var noDocsChange = lazyre.New(`(?m)^\s*(?:[-*]\s+)?No docs change:\s*\S`)
