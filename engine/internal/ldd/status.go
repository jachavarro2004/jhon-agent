package ldd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"

	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	"github.com/jachavarro2004/jhon-agent/engine/internal/discover"
	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/requirements"
	"github.com/jachavarro2004/jhon-agent/engine/internal/vcs"
)

// This file is `jhon status [change] [--json | --brief]`.
//
// It reports what a change directory holds: each artifact's state, task
// progress, the last recorded test run, the verification, notes, and one
// advisory `next` suggestion. It is informational: it never refuses or
// blocks a phase, and phases may be skipped freely. It is a reading of files
// and nothing more: the same tree always gives the same answer.
//
// Values (artifact states, next actions, the passing verdict) come from
// contracts/vocabulary.yaml by position, never spelled here, read on first
// use.

type statusVocabulary struct {
	missing, partial, done, pass string
	phases                       []string
	// routed are the phases status may suggest, those that are also
	// actions: propose, spec, design, tasks, apply, verify, archive.
	routed                    []string
	selectChange, startChange string
	invalidEvidence           string
}

var statusVocab = sync.OnceValues(func() (statusVocabulary, error) {
	states, err := contract.Vocabulary("artifact_state", 3)
	if err != nil {
		return statusVocabulary{}, err
	}
	verdicts, err := contract.Vocabulary("envelope_verdict", 2)
	if err != nil {
		return statusVocabulary{}, err
	}
	src, err := contract.Contract("vocabulary.yaml")
	if err != nil {
		return statusVocabulary{}, err
	}
	phases, actions := contract.List(src, "phases"), contract.List(src, "next_action")
	var routed, other []string
	for _, p := range phases {
		if slices.Contains(actions, p) {
			routed = append(routed, p)
		}
	}
	for _, a := range actions {
		if !slices.Contains(phases, a) {
			other = append(other, a)
		}
	}
	if len(other) != 2 {
		return statusVocabulary{}, errors.New("contracts/vocabulary.yaml: next_action must hold exactly two non-phase actions")
	}
	if len(routed) != 7 {
		return statusVocabulary{}, errors.New("contracts/vocabulary.yaml: expected seven routed phases, propose through archive")
	}
	evidence, err := contract.Vocabulary("strict_tdd_evidence_state", 3)
	if err != nil {
		return statusVocabulary{}, err
	}
	return statusVocabulary{states[0], states[1], states[2], verdicts[0], phases, routed, other[0], other[1], evidence[1]}, nil
})

// StatusVocabulary reads the values status needs, so a command fails up
// front when they are broken, before it reads its arguments.
func StatusVocabulary() error {
	_, err := statusVocab()
	return err
}

// sv is the vocabulary; a caller that has not checked StatusVocabulary
// panics on a broken one.
func sv() statusVocabulary {
	v, err := statusVocab()
	if err != nil {
		panic(err)
	}
	return v
}

// ── text helpers ────────────────────────────────────────────────────────────

func unfenced(src string) []requirements.Line {
	return slices.DeleteFunc(requirements.MarkdownLines(src), func(l requirements.Line) bool { return l.Fenced })
}

var placeholder = lazyre.New(`\b(?:TBD|TODO|FIXME)\b|<[^<>` + js.SpaceChars + `][^<>\n]*>`)

// emptySections are the level-two-and-deeper sections with no content line
// before the next heading of equal or higher level.
func emptySections(src string) []string {
	lines := requirements.MarkdownLines(src)
	var out []string
	for i, l := range lines {
		h := requirements.HeadingOf(l)
		if h == nil || h.Level < 2 {
			continue
		}
		content := false
		for _, next := range lines[i+1:] {
			n := requirements.HeadingOf(next)
			if n != nil && n.Level <= h.Level {
				break
			}
			if n == nil && js.Trim(next.Text) != "" {
				content = true
				break
			}
		}
		if !content {
			out = append(out, h.Text)
		}
	}
	return out
}

// section is the body of a level-two section by exact title, or nil.
func section(src, title string) []requirements.Line {
	lines := requirements.MarkdownLines(src)
	at := slices.IndexFunc(lines, func(l requirements.Line) bool {
		h := requirements.HeadingOf(l)
		return h != nil && h.Level == 2 && h.Text == title
	})
	if at < 0 {
		return nil
	}
	var out []requirements.Line
	for _, l := range lines[at+1:] {
		if h := requirements.HeadingOf(l); h != nil && h.Level <= 2 {
			break
		}
		out = append(out, l)
	}
	return out
}

var (
	itemLead     = lazyre.New(`^[-*][ \t]+`)
	continuation = lazyre.New(`^[ \t]+[^` + js.SpaceChars + `]`)
)

// listItems are the top-level list items of a section, each with its indented continuation lines.
func listItems(lines []requirements.Line) []string {
	var items []string
	for _, l := range lines {
		switch {
		case l.Fenced:
		case itemLead.MatchString(l.Text):
			items = append(items, l.Text)
		case len(items) > 0 && continuation.MatchString(l.Text):
			items[len(items)-1] += "\n" + l.Text
		}
	}
	return items
}

// ── artifact states (contracts/ldd.md, "Artifact states") ─────────────

var (
	codeSpan  = lazyre.New("`([^`]+)`")
	extension = lazyre.New(`\.[A-Za-z][A-Za-z0-9]{0,9}$`)
)

// ExplorationState is done when the exploration names a backticked
// repository path outside fenced code; src nil is a missing file.
func ExplorationState(src *string) string {
	v := sv()
	if src == nil {
		return v.missing
	}
	path := func(t string) bool {
		return strings.IndexFunc(t, js.IsSpace) < 0 && !strings.Contains(t, "://") && (strings.Contains(t, "/") || extension.MatchString(t))
	}
	for _, l := range unfenced(*src) {
		for _, m := range codeSpan.FindAllStringSubmatch(l.Text, -1) {
			if path(js.Trim(m[1])) {
				return v.done
			}
		}
	}
	return v.partial
}

var (
	sourceItem = lazyre.New(`^[-*][ \t]+\[(S\d+)\]`)
	sourceRef  = lazyre.New(`\[(S\d+)\]`)
)

// ResearchState is done when the research holds a claim and every claim
// cites a source its `## Sources` declares.
func ResearchState(src *string) string {
	v := sv()
	if src == nil {
		return v.missing
	}
	declared := map[string]bool{}
	for _, i := range listItems(section(*src, "Sources")) {
		if m := sourceItem.FindStringSubmatch(i); m != nil {
			declared[m[1]] = true
		}
	}
	claims := listItems(section(*src, "Claims"))
	resolves := func(c string) bool {
		return slices.ContainsFunc(sourceRef.FindAllStringSubmatch(c, -1), func(m []string) bool { return declared[m[1]] })
	}
	if len(claims) > 0 && !slices.ContainsFunc(claims, func(c string) bool { return !resolves(c) }) {
		return v.done
	}
	return v.partial
}

// ProposalState is done when the proposal has `## Why` and `## What
// Changes`, no placeholder outside fences, and no empty section.
func ProposalState(src *string) string {
	v := sv()
	if src == nil {
		return v.missing
	}
	lines := unfenced(*src)
	var titles []string
	for _, l := range lines {
		if h := requirements.HeadingOf(l); h != nil && h.Level == 2 {
			titles = append(titles, h.Text)
		}
	}
	required := slices.Contains(titles, "Why") && slices.Contains(titles, "What Changes")
	held := slices.ContainsFunc(lines, func(l requirements.Line) bool { return placeholder.MatchString(l.Text) }) || len(emptySections(*src)) > 0
	if required && !held {
		return v.done
	}
	return v.partial
}

// specFiles are every `spec.md` under the change's specs/ directory, as
// relative path and text, in JavaScript string order. As readdirSync's
// recursive listing, it does not descend into symlinked directories.
func specFiles(l layout.Layout, dir string) ([][2]string, error) {
	root := filepath.Join(dir, l.DeltaSpecs())
	if !project.Exists(root) {
		return nil, nil
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("ENOTDIR: not a directory, scandir '%s'", root)
	}
	// The trailing separator follows specs/ itself when it is a symlink, as readdirSync does.
	walk := root + string(filepath.Separator)
	var names []string
	err := filepath.WalkDir(walk, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != walk && d.Name() == "spec.md" {
			rel, _ := filepath.Rel(root, p)
			names = append(names, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(names, js.Compare)
	out := make([][2]string, 0, len(names))
	for _, f := range names {
		b, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			return nil, err
		}
		out = append(out, [2]string{f, string(b)})
	}
	return out, nil
}

// SpecsState is done when every spec holds requirements and each has a
// scenario, except those of a heading-only operation (a REMOVED one). The
// error is a contracts/spec-delta.md that cannot be read.
func SpecsState(l layout.Layout, specs []string) (string, error) {
	v := sv()
	if len(specs) == 0 {
		return v.missing, nil
	}
	for _, src := range specs {
		reqs, err := l.Scenarios(src, 0)
		if err != nil {
			return "", err
		}
		if len(reqs) == 0 {
			return v.partial, nil
		}
		if slices.ContainsFunc(reqs, func(r layout.Requirement) bool { return !r.HeadingOnly && len(r.Scenarios) == 0 }) {
			return v.partial, nil
		}
	}
	return v.done, nil
}

var letterOrDigit = lazyre.New(`[\p{L}\p{N}]`)

// DesignState is done when the design holds content beyond headings and placeholders.
func DesignState(src *string) string {
	v := sv()
	if src == nil {
		return v.missing
	}
	for _, l := range requirements.MarkdownLines(*src) {
		if requirements.HeadingOf(l) == nil && letterOrDigit.MatchString(placeholder.ReplaceAllString(l.Text, "")) {
			return v.done
		}
	}
	return v.partial
}

var (
	checkbox = lazyre.New(`^` + js.Space + `*-[ \t]+\[([ xX])\][ \t]?(` + js.Dot + `*)$`)
)

// SliceProgress is one slice of tasks.md and its checked tasks.
type SliceProgress struct {
	Name      string `json:"name"`
	Total     int    `json:"total"`
	Completed int    `json:"completed"`
	// Delivers is the slice's `Delivers:` line, the value it integrates; "" without one.
	Delivers string `json:"delivers,omitempty"`
	// Outline is a slice with no checkbox task yet: its heading and
	// Delivers: line, planned when its turn comes; it is not done.
	Outline bool `json:"outline"`
	// Tasks are the slice's checkboxes in order, so a surface shows where in
	// the slice the work is without reading tasks.md itself.
	Tasks []SliceTask `json:"tasks"`
}

// SliceTask is one checkbox of a slice: its text and whether it is checked.
type SliceTask struct {
	Text string `json:"text"`
	Done bool   `json:"done"`
}

// listItem is a markdown list item, which ends a wrapped Delivers: paragraph.
var listItem = lazyre.New(`^(?:[-*+]|\d+[.)])\s`)

// deliversText is a slice's `Delivers:` line and its text.
var deliversText = lazyre.New(`^\s*(?:[-*]\s+)?(?:\*\*)?Delivers:(?:\*\*)?\s*(\S.*?)\s*$`)

// TaskProgress is the checkboxes of tasks.md. Slices are in tasks.md
// order; Slice is the first with an unchecked task, nil when there is none
// or no slice heading. SlicesDone counts the slices with every task checked
// (an outline slice is not done); Current is the first slice not done, an
// outline one included, and Next the slice after it; nil when there is none.
type TaskProgress struct {
	Total       int             `json:"total"`
	Completed   int             `json:"completed"`
	Pending     int             `json:"pending"`
	AllComplete bool            `json:"allComplete"`
	Unchecked   []string        `json:"unchecked"`
	Slices      []SliceProgress `json:"slices"`
	Slice       *string         `json:"slice"`
	SlicesDone  int             `json:"slicesDone"`
	Current     *string         `json:"current"`
	Next        *string         `json:"next"`
}

// Finished is whether every task is checked and every slice done: an
// outline slice is work still to task, so the change is not finished.
func (t TaskProgress) Finished() bool { return t.AllComplete && t.Current == nil }

// Outlined is the names of the slices still outlined, with no task yet, in
// tasks.md order.
func (t TaskProgress) Outlined() []string {
	var out []string
	for _, x := range t.Slices {
		if x.Outline {
			out = append(out, x.Name)
		}
	}
	return out
}

// done is whether every task of a slice is checked; an outline is not.
func (x SliceProgress) done() bool { return x.Total > 0 && x.Completed == x.Total }

// sliceState is a slice's progress in the slices line.
func sliceState(x SliceProgress) string {
	if x.Outline {
		return x.Name + " (outline)"
	}
	return fmt.Sprintf("%s (%d/%d checked)", x.Name, x.Completed, x.Total)
}

// ProgressOf is the task progress of tasks.md (src nil: no file), fenced
// checkboxes ignored, x in either case.
func ProgressOf(lay layout.Layout, src *string) TaskProgress {
	var lines []requirements.Line
	if src != nil {
		lines = unfenced(*src)
	}
	t := noTasks()
	current := -1
	// A Delivers: paragraph wrapped over several lines is one value: its
	// lines up to a blank one, a heading, or a list item continue it.
	continuing := false
	for _, l := range lines {
		if h := lay.SliceHeading().FindStringSubmatch(l.Text); h != nil {
			t.Slices = append(t.Slices, SliceProgress{Name: h[1], Tasks: []SliceTask{}})
			current = len(t.Slices) - 1
			continuing = false
			continue
		}
		if d := deliversText.FindStringSubmatch(l.Text); d != nil && current >= 0 && t.Slices[current].Delivers == "" {
			t.Slices[current].Delivers = d[1]
			continuing = true
			continue
		}
		if continuing {
			text := strings.TrimSpace(l.Text)
			if text != "" && !strings.HasPrefix(text, "#") && !listItem.MatchString(text) {
				t.Slices[current].Delivers += " " + text
				continue
			}
			continuing = false
		}
		m := checkbox.FindStringSubmatch(l.Text)
		if m == nil {
			continue
		}
		t.Total++
		if m[1] == " " {
			t.Unchecked = append(t.Unchecked, js.Trim(m[2]))
		}
		if current >= 0 {
			t.Slices[current].Total++
			if m[1] != " " {
				t.Slices[current].Completed++
			}
			t.Slices[current].Tasks = append(t.Slices[current].Tasks, SliceTask{Text: js.Trim(m[2]), Done: m[1] != " "})
		}
	}
	t.Pending = len(t.Unchecked)
	t.Completed = t.Total - t.Pending
	t.AllComplete = t.Total > 0 && t.Pending == 0
	for i := range t.Slices {
		t.Slices[i].Outline = t.Slices[i].Total == 0
		if t.Slices[i].done() {
			t.SlicesDone++
		}
	}
	for _, x := range t.Slices {
		if x.Completed < x.Total {
			t.Slice = &x.Name
			break
		}
	}
	if i := currentSlice(t); i >= 0 {
		t.Current = &t.Slices[i].Name
		if i+1 < len(t.Slices) {
			t.Next = &t.Slices[i+1].Name
		}
	}
	return t
}

// noTasks is the task progress of no tasks.md.
func noTasks() TaskProgress { return TaskProgress{Unchecked: []string{}, Slices: []SliceProgress{}} }

// currentSlice is the index of the first slice not done, -1 when every
// slice is.
func currentSlice(t TaskProgress) int {
	return slices.IndexFunc(t.Slices, func(x SliceProgress) bool { return !x.done() })
}

// slicesLine is the slices in one line: how many are done and the current
// one's progress; "" without slices.
func slicesLine(t TaskProgress) string {
	if len(t.Slices) == 0 {
		return ""
	}
	line := fmt.Sprintf("slices      %d/%d done", t.SlicesDone, len(t.Slices))
	if i := currentSlice(t); i >= 0 {
		line += ", current " + sliceState(t.Slices[i])
		if i+1 < len(t.Slices) {
			line += ", next " + sliceState(t.Slices[i+1])
		}
	}
	return line
}

// TasksState is done when tasks.md holds at least one checkbox.
func TasksState(l layout.Layout, src *string) string {
	v := sv()
	switch {
	case src == nil:
		return v.missing
	case ProgressOf(l, src).Total > 0:
		return v.done
	}
	return v.partial
}

// VerifyState is done when the report states a passing verdict; anything else is partial.
func VerifyState(src *string) string {
	v := sv()
	switch {
	case src == nil:
		return v.missing
	case ReportVerdict(*src) == v.pass:
		return v.done
	}
	return v.partial
}

// ── out-of-repo edit targets ────────────────────────────────────────────────

var (
	urlScheme  = lazyre.New(`^[a-zA-Z][a-zA-Z0-9+.-]*://`)
	fileShaped = lazyre.New(`[^.]\.[\w-]+$`)
)

// EditCandidate is whether a backticked token is an edit-target candidate
// (docket AC1): a token the scope guard resolves at all. It is one when it
// starts with `~`, `./`, or `../`; when it is absolute and exists on disk
// (symlinks followed); or when it is relative and has file shape, a segment
// with an extension or a first segment that exists at the repository root.
// A URL, and an absolute token that is not on disk, such as the HTTP route
// `/health`, are not. The accepted limitation: an absolute target that does
// not exist yet is not detected. exists is the seam tests replace (nil:
// project.Exists).
func EditCandidate(token, repo string, exists func(string) bool) bool {
	if exists == nil {
		exists = project.Exists
	}
	if urlScheme.MatchString(token) {
		return false
	}
	if strings.HasPrefix(token, "~") || strings.HasPrefix(token, "./") || strings.HasPrefix(token, "../") {
		return true
	}
	if strings.HasPrefix(token, "/") {
		return exists(token)
	}
	segments := slices.DeleteFunc(strings.Split(token, "/"), func(s string) bool { return s == "" })
	return slices.ContainsFunc(segments, fileShaped.MatchString) || (len(segments) > 0 && exists(filepath.Join(repo, segments[0])))
}

// OutsideRepo is whether a candidate resolves outside the repository: `~`
// is the home directory, anything else is relative to repo.
func OutsideRepo(token, repo string) bool {
	var abs string
	if strings.HasPrefix(token, "~") {
		abs = filepath.Join(project.Home(), token[1:])
	} else {
		abs = project.Resolve(repo, token)
	}
	rel, err := filepath.Rel(repo, abs)
	return err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel)
}

var taskToken = lazyre.New("(?i)`([^`" + js.SpaceChars + "]+)`(" + js.Space + `*\(read-only\))?`)

// outOfRepo are the backticked edit-target candidates in a task line that
// resolve outside the repository, reported as a note (the write guard is
// the limit). A token followed by `(read-only)` is an input, not an edit
// target.
func outOfRepo(task, repo string) []string {
	var out []string
	for _, m := range taskToken.FindAllStringSubmatch(task, -1) {
		if m[2] != "" || !EditCandidate(m[1], repo, nil) {
			continue
		}
		if OutsideRepo(m[1], repo) {
			out = append(out, m[1])
		}
	}
	return out
}

// ── status ──────────────────────────────────────────────────────────────────

// ArtifactStatus is one artifact of a change, its path relative to the root, and its state.
type ArtifactStatus struct {
	Artifact string `json:"artifact"`
	State    string `json:"state"`
	Path     string `json:"path"`
}

// Status is what `jhon status` reports.
type Status struct {
	// Ok is false only when no change can be selected from the arguments given.
	Ok        bool
	Change    *string
	Directory *string
	// ActiveChanges is reported only when no change is selected (Change nil).
	ActiveChanges []string
	// RankedChanges are the active changes that are not umbrellas, ranked by
	// pending work (RankChanges); reported only when no change is selected.
	RankedChanges []string
	// Umbrellas are the active changes that only start others (IsUmbrella),
	// by name: never selected by default, listed apart.
	Umbrellas []string
	Artifacts []ArtifactStatus
	Tasks     TaskProgress
	// Next is one advisory suggestion (`next_action`); never a gate, and any
	// phase may run or be skipped.
	Next string
	// Command is the exact command the next step runs, the change's name
	// filled in; "" when the next step is no command. Human and Brief print it.
	Command string
	Notes   []string
	// Tests is the change's last recorded test run (`jhon executions run`), or nil.
	Tests *execution.LastRun
	// StrictTdd is the project's Strict TDD preference: what agents follow, never a gate.
	StrictTdd StrictTdd
	// Projects are the resolved projects: live discovery with the config's overrides applied.
	Projects []discover.ResolvedProject
	// Vcs is the root's VCS state, a fact; below `ready` it is noted.
	Vcs vcs.Vcs
	// Route is the selected change's route from its diff now, or nil when
	// none is selected or the config cannot be read.
	Route *Route
	// Mode is the owner's permission mode for the project (`jhon mode`),
	// which `jhon status` sets; "" leaves it out.
	Mode string
	// Ldd is LDD's state for the project, which `jhon status` sets; nil
	// leaves it out.
	Ldd *LddState
	// Needs is what the selected change's record needs (NeedsOf), or nil
	// without a route.
	Needs *Needs
	// Branch is the branch that names a change with no folder
	// (FolderlessChange); "" for a change with one.
	Branch string
	// WorktreePerChange is the project's `worktree_per_change` setting;
	// reported only when true.
	WorktreePerChange bool
	// Cleanup is what archived changes leave behind (ArchivedCleanup);
	// reported only when not empty.
	Cleanup []Cleanup
}

// LddState is LDD's state for the project: always on, from the default
// (T72: the retired `jhon ldd off` turns nothing off), and whether LDD is
// set up there, active: a layout's config is there (project.UsesLdd).
type LddState struct {
	State  string `json:"state"`
	Source string `json:"source"`
	Active bool   `json:"active"`
}

// object is the status as `--json` prints it, keys in their printed
// order: the idle state and a selected change build theirs differently.
func (s Status) object() object {
	o := s.fields()
	if s.Mode != "" {
		o = append(o, member{"mode", s.Mode})
	}
	if s.Ldd != nil {
		o = append(o, member{"ldd", s.Ldd})
	}
	if s.Change != nil {
		o = append(o, member{"needs", s.Needs}, member{"folder", s.Branch == ""})
	}
	if s.WorktreePerChange {
		o = append(o, member{"worktree_per_change", true})
	}
	if len(s.Cleanup) > 0 {
		o = append(o, member{"cleanup", s.Cleanup})
	}
	return o
}

func (s Status) fields() object {
	if s.Change == nil {
		return object{
			{"ok", s.Ok}, {"change", s.Change}, {"directory", s.Directory}, {"activeChanges", s.ActiveChanges},
			{"rankedChanges", s.RankedChanges}, {"umbrellas", s.Umbrellas}, {"artifacts", s.Artifacts}, {"vcs", s.Vcs}, {"tasks", s.Tasks}, {"next", s.Next}, {"notes", s.Notes},
			{"tests", s.Tests}, {"strict_tdd", s.StrictTdd}, {"projects", s.Projects},
		}
	}
	return object{
		{"ok", s.Ok}, {"change", s.Change}, {"directory", s.Directory}, {"artifacts", s.Artifacts}, {"tasks", s.Tasks},
		{"next", s.Next}, {"notes", s.Notes}, {"tests", s.Tests}, {"strict_tdd", s.StrictTdd}, {"projects", s.Projects},
		{"vcs", s.Vcs}, {"route", s.Route}, {"umbrellas", s.Umbrellas},
	}
}

// MarshalJSON writes the status with object's keys and order.
func (s Status) MarshalJSON() ([]byte, error) { return s.object().MarshalJSON() }

// A member is one key of an object.
type member struct {
	key   string
	value any
}

// object is a JSON object whose keys keep their order.
type object []member

// set replaces the value at key in place, or appends it, as assigning a property does.
func (o object) set(key string, value any) object {
	for i := range o {
		if o[i].key == key {
			o[i].value = value
			return o
		}
	}
	return append(o, member{key, value})
}

func (o object) get(key string) (any, bool) {
	for _, m := range o {
		if m.key == key {
			return m.value, true
		}
	}
	return nil, false
}

// MarshalJSON writes the members in order, without HTML escaping.
func (o object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	b.WriteByte('{')
	for i, m := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		if err := enc.Encode(m.key); err != nil {
			return nil, err
		}
		b.Truncate(b.Len() - 1)
		b.WriteByte(':')
		if err := enc.Encode(m.value); err != nil {
			return nil, err
		}
		b.Truncate(b.Len() - 1)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// changesOf is root's changes directory, relative to it.
func changesOf(root string) string { return filepath.FromSlash(layout.For(root).ChangesDir()) }

// evidenceFile is the change's optional TDD witness record
// (contracts/tdd.md): reported present or missing, never parsed.
const evidenceFile = "tdd-evidence.md"

func activeChanges(root string) ([]string, error) {
	dir := filepath.Join(root, changesOf(root))
	out := []string{}
	if !project.Exists(dir) {
		return out, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Name() == "archive" {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			out = append(out, e.Name())
		}
	}
	slices.SortFunc(out, js.Compare)
	return out, nil
}

// vcsNotes is a VCS state below ready, as a note: git is never a gate here.
func vcsNotes(v vcs.Vcs) []string {
	if v.State == vcs.Ready() {
		return nil
	}
	return []string{vcs.Note(v)}
}

// tddNotes is a Strict TDD preference the project cannot back, as a note.
func tddNotes(t StrictTdd) []string {
	if t.Value && t.Evidence == sv().invalidEvidence {
		return []string{fmt.Sprintf("strict_tdd is true but its evidence is %s (%s); correct openspec/config.yaml or re-run /jhon-ldd init", t.Evidence, t.Reason)}
	}
	return nil
}

// changeSet is the active changes as status reports them when it selects
// none: all of them by name, the candidates ranked, and the umbrellas.
type changeSet struct{ active, ranked, umbrellas []string }

func idle(ok bool, next, note string, set changeSet, tdd StrictTdd, resolved discover.ResolvedProjects, v vcs.Vcs) Status {
	notes := slices.Concat([]string{note}, vcsNotes(v), resolved.Notes, tdd.Notes, tddNotes(tdd))
	return Status{
		Ok: ok, ActiveChanges: set.active, RankedChanges: set.ranked, Umbrellas: set.umbrellas, Artifacts: []ArtifactStatus{}, Vcs: v,
		Tasks: noTasks(), Next: next, Notes: notes, StrictTdd: tdd, Projects: resolved.Projects,
	}
}

// StatusOf is the status of a change under root, selected per "Change
// selection": the change named (name nil: none named), else the umbrella
// the branch names, else the only active one that is no umbrella, then the
// branch, the edits, and the lane. Several left undecided are ranked
// (RankChanges) and none is selected: a hook acts on the selected change,
// so a guess would act on the wrong one. The error is a file that exists
// and cannot be read.
func StatusOf(root string, name *string) (Status, error) {
	s, err := selectStatus(root, name)
	if err != nil {
		return s, err
	}
	// The project's worktree_per_change (T85a): reported only when true, an
	// invalid value noted.
	on, note := worktreeOf(root)
	s.WorktreePerChange = on
	if note != "" {
		s.Notes = append(s.Notes, note)
	}
	// What archived changes leave behind (T80b): named, never removed.
	if s.Vcs.State == vcs.Ready() {
		s.Cleanup = ArchivedCleanup(root)
		for _, c := range s.Cleanup {
			s.Notes = append(s.Notes, c.Note())
		}
	}
	return s, nil
}

func selectStatus(root string, name *string) (Status, error) {
	v, err := statusVocab()
	if err != nil {
		return Status{}, err
	}
	active, err := activeChanges(root)
	if err != nil {
		return Status{}, err
	}
	tdd, err := StrictTddFor(root)
	if err != nil {
		return Status{}, err
	}
	resolved, err := discover.ProjectsFor(root)
	if err != nil {
		return Status{}, err
	}
	state := vcs.State(root)
	// One reading: the change's selection, its route, and its slice share the diff.
	g := gitReads{}
	candidates, umbrellas := splitUmbrellas(root, active)
	set := changeSet{active: active, ranked: []string{}, umbrellas: umbrellas}
	derive := func(change, branch string) (Status, error) {
		st, err := g.derive(root, change, branch, tdd, resolved, state)
		st.Umbrellas = umbrellas
		return st, err
	}
	branchNamed := ""
	if name == nil && len(umbrellas) > 0 {
		branchNamed = BranchChange(root, active)
	}
	switch {
	case name != nil:
		if !slices.Contains(active, *name) {
			// The change the branch names when it has no folder.
			if fl, branch := g.folderless(root); fl == *name {
				return derive(*name, branch)
			}
			return idle(false, v.selectChange, filepath.Join(changesOf(root), *name)+"/ does not exist or is not an active change", set, tdd, resolved, state), nil
		}
	case slices.Contains(umbrellas, branchNamed):
		// An umbrella is selected only when named: by the caller, or by the branch.
		name = &branchNamed
	case len(candidates) == 1:
		name = &candidates[0]
	case BranchChange(root, candidates) != "":
		// Several are active and the branch names one: the session's change,
		// the one the jhon shell, the dashboard, and an agent all mean.
		name = new(BranchChange(root, candidates))
	case EditedChange(root, candidates) != "":
		// A branch or worktree that names none, editing one: the change just
		// planned or checked off there, before any commit says so.
		name = new(EditedChange(root, candidates))
	case g.laneChange(root, candidates) != "":
		// A lane worktree (chore/tsr-common working on
		// test-suite-review-common): the branch names none, its own commits do.
		name = new(g.laneChange(root, candidates))
	case len(candidates) > 1:
		set.ranked = RankChanges(root, candidates)
		return idle(false, v.selectChange, fmt.Sprintf("%d active changes; name one: %s", len(candidates), strings.Join(set.ranked, ", ")), set, tdd, resolved, state), nil
	default:
		// No folder, on a branch with code changes: the branch names a small change.
		if fl, branch := g.folderless(root); fl != "" {
			return derive(fl, branch)
		}
		if len(umbrellas) > 0 {
			return idle(true, v.startChange, "no change in progress; umbrella: "+strings.Join(umbrellas, ", ")+" ("+v.startChange+": "+umbrellaHow+")", set, tdd, resolved, state), nil
		}
		// The branch would name a change with no file of its own yet (T84).
		if note := FolderlessPendingNote(root); note != "" {
			s := idle(true, v.startChange, "no active change under "+changesOf(root)+"/; before you finish, start one (tasks.md) and record a run (`jhon executions run`)", set, tdd, resolved, state)
			s.Notes = append(s.Notes, note)
			return s, nil
		}
		// On the default branch, the exact commands a small change starts
		// with; start-change is no command (T146g G1).
		s := idle(true, v.startChange, StartNote(changesOf(root)), set, tdd, resolved, state)
		s.Command = SmallStart
		return s, nil
	}
	return derive(*name, "")
}

// BranchChange is the active change the checked-out branch names, the
// longest when several do (feat/abc-process-exercises names
// abc-process-exercises over abc-process), or "" when none does or there is
// no branch. A slash names a dash: fix/tenant-caches names fix-tenant-caches.
func BranchChange(root string, active []string) string {
	out, err := project.ReadGitCmd(root, "branch", "--show-current").Output()
	branch := strings.TrimSpace(string(out))
	if err != nil || branch == "" {
		return ""
	}
	return branchNames(branch, active)
}

// branchNames is the change of changes branch names, by BranchChange's
// rule, or "".
func branchNames(branch string, changes []string) string {
	dashed := strings.ReplaceAll(branch, "/", "-")
	best := ""
	for _, c := range changes {
		if (strings.Contains(branch, c) || strings.Contains(dashed, c)) && len(c) > len(best) {
			best = c
		}
	}
	return best
}

// EditedChange is the one active change with uncommitted files (modified,
// deleted, or untracked under its directory), or "" when none or several
// have them, or git cannot say.
func EditedChange(root string, active []string) string {
	out, ok := riskGit(root, "ls-files", "--modified", "--others", "--exclude-standard", "--", changesOf(root))
	if !ok {
		return ""
	}
	edited := ""
	for _, f := range nonEmpty(out) {
		parts := strings.Split(strings.TrimPrefix(f, changesOf(root)+"/"), "/")
		if len(parts) < 2 || !slices.Contains(active, parts[0]) || parts[0] == edited {
			continue
		}
		if edited != "" {
			return ""
		}
		edited = parts[0]
	}
	return edited
}

// LaneChange is the active change the branch's own commits touched last,
// the commits since it left the default branch, or "" on the default branch
// itself or when they touched none.
func LaneChange(root string, active []string) string {
	return gitReads(nil).laneChange(root, active)
}

func (g gitReads) laneChange(root string, active []string) string {
	top, _ := g.git(root, "rev-parse", "--show-toplevel")
	base, label, ok := g.baseOf(js.Trim(top), nil)
	if !ok || label == "HEAD" {
		return ""
	}
	out, _ := g.git(root, "log", "--format=", "--name-only", base+"..HEAD", "--", changesOf(root))
	for _, f := range nonEmpty(out) {
		if parts := strings.Split(strings.TrimPrefix(f, changesOf(root)+"/"), "/"); len(parts) > 1 && slices.Contains(active, parts[0]) {
			return parts[0]
		}
	}
	return ""
}

// derive is the status of change; branch names it when it has no folder.
func (g gitReads) derive(root, change, branch string, tdd StrictTdd, resolved discover.ResolvedProjects, state vcs.Vcs) (Status, error) {
	v := sv()
	lay := layout.For(root)
	rel := filepath.Join(changesOf(root), change)
	dir := filepath.Join(root, rel)
	var readErr error
	read := func(f string) *string {
		p := filepath.Join(dir, f)
		if readErr != nil || !project.Exists(p) {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			readErr = err
			return nil
		}
		return new(string(b))
	}
	proposal := read("proposal.md")
	tasksSrc := read(lay.Tasks())
	verifySrc := read("verify-report.md")
	evidenceSrc := read(evidenceFile)
	if readErr != nil {
		return Status{}, readErr
	}
	specFileList, err := specFiles(lay, dir)
	if err != nil {
		return Status{}, err
	}
	specs := make([]string, len(specFileList))
	for i, f := range specFileList {
		specs[i] = f[1]
	}
	exploration, research := read("exploration.md"), read("research.md")
	if readErr != nil {
		return Status{}, readErr
	}
	specsState, err := SpecsState(lay, specs)
	if err != nil {
		return Status{}, err
	}
	design := read("design.md")
	if readErr != nil {
		return Status{}, readErr
	}
	evidenceState := v.done
	if evidenceSrc == nil {
		evidenceState = v.missing
	}
	artifacts := []ArtifactStatus{
		{Artifact: "exploration.md", State: ExplorationState(exploration)},
		{Artifact: "research.md", State: ResearchState(research)},
		{Artifact: "proposal.md", State: ProposalState(proposal)},
		{Artifact: "specs/", State: specsState},
		{Artifact: "design.md", State: DesignState(design)},
		{Artifact: "tasks.md", State: TasksState(lay, tasksSrc)},
		{Artifact: evidenceFile, State: evidenceState},
		{Artifact: "verify-report.md", State: VerifyState(verifySrc)},
	}
	for i := range artifacts {
		artifacts[i].Path = filepath.Join(rel, strings.TrimSuffix(artifacts[i].Artifact, "/"))
	}
	st := func(a string) string {
		return artifacts[slices.IndexFunc(artifacts, func(x ArtifactStatus) bool { return x.Artifact == a })].State
	}
	tasks := ProgressOf(lay, tasksSrc)
	notes := append([]string{}, slices.Concat(vcsNotes(state), resolved.Notes, tdd.Notes, tddNotes(tdd))...)
	directory, shown := &rel, artifacts
	if branch != "" {
		notes = append([]string{folderlessNote(change, branch)}, notes...)
		// No folder holds them: its record is its runs.
		directory, shown = nil, []ArtifactStatus{}
	}

	// A spec at another heading depth reads as partial; the note says why.
	for _, f := range specFileList {
		if m := requirements.HeadingDepthMismatch(f[1]); m != nil {
			notes = append(notes, fmt.Sprintf("%s line %d is a level %d %s: heading; the contract expects level %d, so compose will refuse it",
				filepath.Join(rel, lay.DeltaSpecs(), f[0]), m.Line, m.Found, m.Kind, m.Expected))
		}
	}

	// Out-of-repo edit targets in unchecked tasks: a note; the write guard asks.
	repo := project.Resolve("", root)
	for _, t := range tasks.Unchecked {
		for _, p := range outOfRepo(t, repo) {
			notes = append(notes, fmt.Sprintf("a task edits `%s`, outside the repository: \"%s\"", p, t))
		}
	}

	// What the adaptive flow asks of the intent, the tasks, the tests, and the verification.
	notes = append(notes, ProposalNotes(proposal)...)
	notes = append(notes, TasksNotes(lay, tasksSrc)...)
	if len(specs) > 0 {
		coverage, err := ScenarioCoverage(lay, specs, testFiles(root))
		if err != nil {
			return Status{}, err
		}
		if coverage.Covered < coverage.Total {
			quoted := make([]string, len(coverage.Uncovered))
			for i, n := range coverage.Uncovered {
				quoted[i] = strconv.Quote(n)
			}
			notes = append(notes, fmt.Sprintf("scenarios named by a test: %d/%d; not yet: %s", coverage.Covered, coverage.Total, strings.Join(quoted, ", ")))
			if note := copiesNote(root, specFileList, coverage); note != "" {
				notes = append(notes, note)
			}
		}
	}
	if verifySrc != nil {
		notes = append(notes, VerifyNotes(*verifySrc, recordedRuns(root, change))...)
	}

	// A verification that does not record a pass.
	if verifySrc != nil && st("verify-report.md") == v.partial {
		verdict := ReportVerdict(*verifySrc)
		if verdict == "" {
			verdict = "none"
		}
		notes = append(notes, "verify-report.md does not report a passing verdict (verdict "+verdict+")")
	}

	// Only a project that is its repository's top level has a route of its
	// own: one nested in another repository would read that one's diff.
	var route *Route
	if top := vcs.GitTopLevel(root); top != "" && project.Realish(top) == project.Realish(root) {
		if rt, err := g.routeOf(root); err == nil {
			route = &rt
		}
	}
	// The slice in progress over the advisory size is said, never refused.
	if route != nil {
		size := g.sliceLines(root, change, *route)
		route.Slice = &size
		if size.Over() {
			notes = append(notes, size.Note())
		}
		if n := g.dataReachNote(root, change); n != "" {
			notes = append(notes, n)
		}
		route.Reviews = ReviewsOf(root, []string{change}, *route)
	}
	var needs *Needs
	if route != nil {
		n := g.needsOf(root, *route)
		needs = &n
	}
	// Without a design on the route, the intent lives in tasks.md.
	if route != nil && !route.Design && tasksSrc != nil && section(*tasksSrc, "Intent") == nil {
		notes = append(notes, "tasks.md has no ## Intent: state why and the acceptance criteria verify checks against")
	}

	// The suggestion: the phase after the furthest one whose output exists.
	// Earlier gaps never hold it back.
	r := v.routed
	propose, spec, designPhase, tasksPhase, apply, verify, archive := r[0], r[1], r[2], r[3], r[4], r[5], r[6]
	// A design is optional: it is suggested only when the route asks for
	// one. A route that asks for none is not suggested a proposal either,
	// unless one exists (the guard refuses neither): they drop out of the
	// order.
	var unrouted []string
	if route == nil || !route.Design {
		unrouted = []string{designPhase}
	}
	if route != nil && !route.Design && (proposal == nil || needs != nil && !needs.Proposal) {
		unrouted = append(unrouted, propose)
	}
	// tasks.md and a spec delta are suggested only when the change needs
	// them (NeedsOf, T31), unless written. A change that needs no tasks.md
	// has applied once its RED before GREEN is on record: TDD always goes.
	tests, err := execution.ReadLastRun(root, change)
	if err != nil {
		return Status{}, err
	}
	redGreen := tests != nil && tests.RedBeforeGreen
	behaviour := route == nil || route.Behaviour
	tddOnly := needs != nil && !needs.Tasks && st("tasks.md") == v.missing
	// A change that alters no behaviour has applied once a file outside its
	// record differs from the base or is new (T96, L537): a record alone
	// (an exploration, a proposal) has applied nothing.
	appliedNothing := route != nil && !behaviour && !g.changedOutsideRecord(root)
	if needs != nil && !needs.Spec && st("specs/") == v.missing {
		unrouted = append(unrouted, spec)
	}
	if tddOnly {
		unrouted = append(unrouted, tasksPhase)
	}
	// Verify is suggested only when it is due: on the verified route, or
	// when review_required names it (ReviewsOf). Without a route, status
	// cannot tell, and suggests it as before.
	verifyDue := route == nil || slices.ContainsFunc(route.Reviews, func(x Review) bool { return x.Review == ReviewVerify })
	if !verifyDue {
		unrouted = append(unrouted, verify)
	}
	produced := map[string]bool{
		propose:     st("proposal.md") == v.done,
		spec:        st("specs/") == v.done,
		designPhase: st("design.md") == v.done,
		tasksPhase:  st("tasks.md") == v.done,
		apply:       st("tasks.md") == v.done && tasks.Finished() || tddOnly && (redGreen || !behaviour && !appliedNothing),
		verify:      st("verify-report.md") == v.done,
		archive:     false,
	}
	furthest := -1
	for i, p := range r {
		if produced[p] {
			furthest = i
		}
	}
	for furthest+1 < len(r)-1 && slices.Contains(unrouted, r[furthest+1]) {
		furthest++
	}
	// A failing report sends the suggestion back to verify; unchecked tasks
	// or an outline slice, to apply.
	next := r[min(furthest+1, len(r)-1)]
	if next == archive && verifyDue && !produced[verify] {
		next = verify
	}
	if (next == verify || next == archive) && st("tasks.md") == v.done && !tasks.Finished() {
		next = apply
	}
	// A delivered slice is followed by tasking the next one, with what it taught.
	if plan := SliceToPlan(tasks); next == apply && plan != nil {
		next = tasksPhase
		notes = append(notes, *plan+" is next: write its spec delta and tasks, and revise the slices after it")
	}
	// Never archive a change that alters behaviour before its RED before
	// GREEN; a change with no folder has nothing to archive: it is delivered.
	if next == archive && behaviour && !redGreen {
		next = apply
	}
	// A small change (T146 FA2): no folder, and its record needs no tasks.md
	// (one component, under the advisory size, not high risk).
	small := branch != "" && needs != nil && !needs.Tasks
	if next == apply && behaviour && !redGreen && (tddOnly || st("tasks.md") == v.done && tasks.Finished()) {
		hint := RedGreenHint(change)
		if small {
			hint = SmallRedGreen
		}
		notes = append(notes, "no RED before GREEN on record for "+change+": "+hint)
	}
	if next == apply && appliedNothing {
		notes = append(notes, "nothing applied yet for "+change+": no file outside "+layout.For(root).Dir()+"/ differs from the base or is new; apply the change (one that alters behaviour starts with its failing test)")
	}
	// A light verify at slice close (T31.5): asked on a route that alters
	// behaviour, short of verified, whose RED before GREEN run is newer than
	// its last light verify. A note; next stays as computed.
	// A small change keeps none (T146g G3).
	lightRoute := route != nil && route.Behaviour && route.Level != RouteVerified && !small
	if lightRoute && execution.LightVerifyDue(tests) {
		notes = append(notes, "light verify due for "+change+": its GREEN run is newer than its last light verify; launch the `verify` agent in light mode (read the diff and the tests, run the focused tests), which records `jhon executions verify "+change+" --verdict pass|fail`")
	}
	// A folder is archived only once a light verify passed the tested tree.
	if next == archive && branch == "" && lightRoute && !execution.LightVerified(root, change) {
		next = verify
	}
	// An umbrella is split into changes, never archived nor applied (T37).
	if branch == "" && IsUmbrella(root, change) {
		next = v.startChange
		notes = append([]string{change + " is an umbrella (" + v.startChange + "): " + umbrellaHow}, notes...)
	}
	if next == archive && branch != "" {
		next = Merge()
		notes = append(notes, "nothing to archive: "+change+" has no folder; deliver it with a PR or commit whose text says what was asked")
	}

	// The route rises with the diff: a proposal and a design it asks for
	// are due now, not when a later session reads the project's rules.
	if route != nil && route.Design && (st("proposal.md") != v.done || st("design.md") != v.done) {
		notes = append(notes, "the route asks for a proposal and a design now ("+strings.Join(route.Spans, ", ")+"): write them before the next slice")
	}

	// The exact next command, the name filled in (T146, L1211): the run that
	// records RED and GREEN, or the delivery check.
	command := ""
	switch {
	case next == apply && behaviour && !redGreen && small:
		// T146h L4: the small path's run is the bare one, twice.
		command = "jhon executions run"
	case next == apply && behaviour && !redGreen && (tddOnly || st("tasks.md") == v.done && tasks.Finished()):
		command = "jhon executions run " + change
		if base := g.baseName(root); base != "" {
			command += " --affected " + base
		}
	case next == Merge() && branch != "":
		command = "jhon deliver check " + change
		if vcs.CandidateOf(root, execution.ChangeRecordPaths(root, change)).Clean != true {
			command += "  (once the tested tree is committed)"
		}
	}
	if small {
		notes = append(notes, SmallChangeNote(change))
	} else {
		// T146h T1: a change that is not small reads the full context first.
		notes = append([]string{NotSmallNote()}, notes...)
	}

	return Status{
		Ok: true, Change: &change, Directory: directory, Artifacts: shown, Tasks: tasks, Next: next, Command: command, Notes: notes,
		Tests: tests, StrictTdd: tdd, Projects: resolved.Projects, Vcs: state, Route: route, Needs: needs, Branch: branch,
	}, nil
}

// baseName is how a command names the base the change in root is measured
// from: the ref baseOf names it by, else, when the branch has no commit of
// its own yet, the default branch at that commit, else the commit; "" when
// there is none.
func (g gitReads) baseName(root string) string {
	commit, label, ok := g.baseOf(root, nil)
	if !ok {
		return ""
	}
	if label != "HEAD" {
		return label
	}
	for _, ref := range g.defaultBranches(root) {
		if c, ok := g.git(root, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); ok && js.Trim(c) == commit {
			return ref
		}
	}
	return commit
}

// SmallRedGreen is how a small change records its RED and GREEN (T146h
// L4): the bare run twice, no unit's run between.
const SmallRedGreen = "run `jhon executions run` while its new test fails (RED), then `jhon executions run` again once it passes (GREEN): two runs, nothing between"

// Small is whether the status is a small change's (T146 FA2): no folder,
// and a record that needs no tasks.md.
func (s Status) Small() bool { return s.Branch != "" && s.Needs != nil && !s.Needs.Tasks }

// NotSmall is whether the repository holding dir has a change selected
// that is not small: a session there starts with the full context
// (T146h T2). No LDD config, no change, or a small one is false.
func NotSmall(dir string) bool {
	top, _ := riskGit(dir, "rev-parse", "--show-toplevel")
	top = js.Trim(top)
	if top == "" || !project.UsesLdd(top) {
		return false
	}
	s, err := StatusOf(top, nil)
	return err == nil && s.Change != nil && !s.Small()
}

// FullContextFile is the install's full context for runtime (T146h L2):
// <home>/.local/share/jhon/current/prose/full-<runtime>.md, the file the
// renderer writes and the lean text points to.
func FullContextFile(home, runtime string) string {
	return filepath.Join(home, ".local", "share", "jhon", "current", "prose", "full-"+runtime+".md")
}

// sessionRuntime is the runtime the calling session runs in, from its
// environment: pi's agent directory (jhon start pi), else Claude Code's
// marker; "" for neither.
func sessionRuntime() string {
	switch {
	case os.Getenv("PI_CODING_AGENT_DIR") != "":
		return "pi"
	case os.Getenv("CLAUDECODE") != "":
		return "claude-code"
	}
	return ""
}

// NotSmallNote is what status tells a change that is not small (T146h T1):
// read the full context of the session's runtime before the next edit;
// outside both runtimes it names Claude Code's file and pi's beside it.
func NotSmallNote() string {
	home := project.Home()
	rt := sessionRuntime()
	if rt == "" {
		return "not small: read " + FullContextFile(home, "claude-code") + " (pi: full-pi.md) before the next edit"
	}
	return "not small: read " + FullContextFile(home, rt) + " before the next edit"
}

// SmallChangeNote is what status tells a small change: no bookkeeping, and
// its GREEN run is its delivery evidence once the tree it tested is committed.
func SmallChangeNote(change string) string {
	return "small change (no folder, one component, under the advisory size, not high risk): it " + SmallKeepsNo + ", and saves no memory or session summary; the run that records its GREEN is its delivery evidence: commit the tree it tested, then `jhon deliver check " + change + "`, with no re-run"
}

// SmallKeepsNo is what a small change keeps none of, in the words the
// prose, the route text and pi's todo text share (T146g G3).
const SmallKeepsNo = "keeps no tasks.md, archive, light verify or todo list"

// SmallStart is the commands a small change starts with on the default
// branch (T146g G1): its own branch, then its run.
const SmallStart = "git switch -c <name>, then jhon executions run"

// StartNote is what status says with no change in progress under changes/
// on the default branch: how a small change starts, and today's words for
// one that is not small.
func StartNote(changes string) string {
	return "no active change under " + changes + "/: a small change (one component, under the advisory size, not high risk) starts on a branch of its own: `git switch -c <name>`, then `jhon executions run` while its new test fails (RED) and again once it passes (GREEN); a change that is not small (several components, over the advisory size, or high risk): before you finish, start one (tasks.md) and record a run (`jhon executions run`)"
}

// ── views ───────────────────────────────────────────────────────────────────

// shownArtifacts is what the view lists (L46, L48): an artifact written, or
// one the change owes: what its needs ask for (NeedsOf, T31), tasks.md, a
// spec delta, a proposal and a design; verify-report.md when verify is
// due. Without a route, as before: tasks.md and a spec delta. A change
// that needs none of them owes only its RED and GREEN runs. The JSON keeps
// every artifact.
func shownArtifacts(s Status) []ArtifactStatus {
	r := s.Route
	verifyDue := r == nil || slices.ContainsFunc(r.Reviews, func(x Review) bool { return x.Review == ReviewVerify })
	owed := map[string]bool{"tasks.md": true, "verify-report.md": verifyDue, "specs/": r == nil,
		"proposal.md": r != nil && r.Design, "design.md": r != nil && r.Design}
	if n := s.Needs; n != nil {
		owed["tasks.md"], owed["specs/"], owed["proposal.md"], owed["design.md"] = n.Tasks, n.Spec, n.Proposal, n.Design
	}
	return slices.DeleteFunc(slices.Clone(s.Artifacts), func(a ArtifactStatus) bool { return a.State == "missing" && !owed[a.Artifact] })
}

// prefix16 is s.slice(0, n): the first n UTF-16 code units, a split pair
// written as U+FFFD, as node writes a lone surrogate.
func prefix16(s string, n int) string {
	u := utf16.Encode([]rune(s))
	return string(utf16.Decode(u[:min(n, len(u))]))
}

// testsLine is the last test run in one line.
func testsLine(t *execution.LastRun) string {
	if t == nil {
		return "no recorded run"
	}
	result := "partial"
	if t.Passed {
		result = "pass"
	} else if t.Complete {
		result = "fail"
	}
	tree := prefix16(t.Tree, 12)
	if tree == "" {
		tree = "unknown"
	}
	dirty := ""
	if !t.Clean || t.Dirty {
		dirty = " (dirty)"
	}
	head := fmt.Sprintf("%s  %s  %s  tree %s%s", result, t.Timestamp, t.Scope, tree, dirty)
	if len(t.Failed) > 0 {
		return head + "  failed: " + strings.Join(t.Failed, ", ")
	}
	return head
}

// Human is the full report `jhon status` prints.
func Human(s Status) string {
	var out []string
	list := func(items []string) []string {
		l := make([]string, len(items))
		for i, x := range items {
			l[i] = "  - " + x
		}
		return l
	}
	tdd := fmt.Sprintf("strict_tdd  %t  (source %s, evidence %s)  %s", s.StrictTdd.Value, s.StrictTdd.Source, s.StrictTdd.Evidence, s.StrictTdd.Reason)
	w := 4
	for _, p := range s.Projects {
		w = max(w, js.Len(p.Path))
	}
	projects := []string{"projects"}
	for _, p := range s.Projects {
		command := ""
		if p.Command != nil && *p.Command != "" {
			source := "null"
			if p.CommandSource != nil {
				source = *p.CommandSource
			}
			command = *p.Command + "  (" + source + ")"
		}
		line := "  " + js.PadEnd(p.Path, w) + "  " + js.PadEnd(p.Testability, 19) + " " + command
		projects = append(projects, strings.TrimRightFunc(line, js.IsSpace))
	}
	vcsLine := fmt.Sprintf("vcs         %s  (%s)", s.Vcs.State, s.Vcs.Reason)
	if s.Ldd != nil {
		active := "inactive"
		if s.Ldd.Active {
			active = "active"
		}
		vcsLine += fmt.Sprintf("\nldd         %s  (%s; %s)", s.Ldd.State, s.Ldd.Source, active)
	}
	if s.WorktreePerChange {
		vcsLine += "\nworktree    per change  (worktree_per_change: true)"
	}
	if s.Change == nil {
		out = append(out, "change      none selected", vcsLine, tdd)
		out = append(out, projects...)
		if len(s.RankedChanges) > 0 {
			out = append(append(out, "active"), list(s.RankedChanges)...)
		} else if active := slices.DeleteFunc(slices.Clone(s.ActiveChanges), func(c string) bool { return slices.Contains(s.Umbrellas, c) }); len(active) > 0 {
			out = append(append(out, "active"), list(active)...)
		}
		out = append(out, umbrellaLines(s.Umbrellas)...)
		out = append(out, nextLines(s)...)
		if len(s.Notes) > 0 {
			out = append(append(out, "", "notes"), list(s.Notes)...)
		}
		return strings.Join(out, "\n")
	}
	where := "no folder: branch " + s.Branch + " names it"
	if s.Directory != nil {
		where = *s.Directory
	}
	out = append(out, fmt.Sprintf("change      %s  (%s)", *s.Change, where), vcsLine, "", "artifacts")
	for _, a := range shownArtifacts(s) {
		out = append(out, "  "+js.PadEnd(a.State, 8)+" "+js.PadEnd(a.Artifact, 18)+" "+a.Path)
	}
	out = append(out, "", fmt.Sprintf("tasks       %d/%d checked, %d unchecked", s.Tasks.Completed, s.Tasks.Total, s.Tasks.Pending))
	if sl := slicesLine(s.Tasks); sl != "" {
		out = append(out, sl)
	}
	for _, t := range s.Tasks.Unchecked {
		out = append(out, "  - [ ] "+t)
	}
	if s.Route != nil {
		out = append(out, "route       "+s.Route.String())
		if sl := s.Route.Slice; sl != nil {
			out = append(out, fmt.Sprintf("slice       slice %s adds %d lines of code (advisory %d)", sl.Slice, sl.Lines, sl.Advisory))
		}
		for _, v := range s.Route.Reviews {
			out = append(out, "review      "+v.Line())
		}
	}
	out = append(out, "", "tests       "+testsLine(s.Tests), "", tdd, "")
	out = append(append(out, projects...), "", "next        "+s.Next+"  (suggestion)")
	if s.Command != "" {
		out = append(out, "command     "+s.Command)
	}
	if len(s.Notes) > 0 {
		out = append(append(out, "", "notes"), list(s.Notes)...)
	}
	return strings.Join(out, "\n")
}

// nextLines is the suggestion and its command; with no change selected, a
// command stands alone: start-change is no command (T146g G1).
func nextLines(s Status) []string {
	if s.Change == nil && s.Command != "" {
		return []string{"command     " + s.Command}
	}
	out := []string{"next        " + s.Next + "  (suggestion)"}
	if s.Command != "" {
		out = append(out, "command     "+s.Command)
	}
	return out
}

// Brief is the short view: the change, the suggestion, tasks, the last test
// run, strict_tdd, and the tested tree.
func Brief(s Status) string {
	change := "none selected"
	if s.Change != nil {
		change = *s.Change
	}
	out := append([]string{"change      " + change}, nextLines(s)...)
	out = append(out, fmt.Sprintf("tasks       %d/%d checked", s.Tasks.Completed, s.Tasks.Total))
	if sl := slicesLine(s.Tasks); sl != "" {
		out = append(out, sl)
	}
	if s.Route != nil {
		out = append(out, "route       "+s.Route.String())
	}
	tree := "none"
	if s.Tests != nil && s.Tests.Tree != "" {
		tree = s.Tests.Tree
	}
	out = append(out, "tests       "+testsLine(s.Tests), fmt.Sprintf("strict_tdd  %t", s.StrictTdd.Value), "tree        "+tree)
	return strings.Join(out, "\n")
}

// RedGreenHint is how change's RED before GREEN is recorded, as docs/ldd.md
// says in "Test runs: the inner loop": the slice base for the RED record and
// the slice's close, a unit's run between, a full run when there is no base.
// The status note, the route guard's refusal, and a light verify's say it.
func RedGreenHint(change string) string {
	return "run `jhon executions run " + change + " --affected <base>` while its new test fails (RED), a unit's run (`--affected`) between, and `--affected <base>` again once it passes (GREEN), where <base> is the commit the slice started from (with none, a full `jhon executions run " + change + "`)"
}
