package ldd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sandbox"
	"github.com/jachavarro2004/jhon-agent/engine/internal/vcs"
)

// This file is the change's state record in Engram (T134, L1021 H4): at a
// slice-closing commit, a recorded light verify, and an archive, after the
// transition is accepted, the engine writes ONE record per change, `ldd
// state: <change>`, under the topic key `ldd-state/<change>`, which
// `engram save --topic` upserts: the change's phase, its slices done of the
// total, its last test run, its delivery, the decisions the handoff's search
// finds for it, and `Refs:` lines, `<kind> <name> @ <path>` from CodeGraph
// for what its diff touches, never a line number. The change's record
// (openspec/) stays the source; this is its copy.
//
// The write never fails or slows the transition: one deadline (StateDeadline)
// bounds the searches, CodeGraph, and the save; Engram not resolvable is no
// write and no note; any failure is one note line. Inside Jhon's sandbox,
// whose Engram folder stays read-only, the record is queued instead and
// saved by Jhon outside the sandbox (engramqueue.go).

// StateDeadline bounds a state write: decisions, refs, and the save.
const StateDeadline = 3 * time.Second

// StateType is the Engram type of a state record: not a decision, so the
// handoff's decision search never returns a change's state as a decision.
const StateType = "ldd-state"

// maxRefs is the most `Refs:` lines a state record carries.
const maxRefs = 20

// StateTitle is the title of change's state record.
func StateTitle(change string) string { return "ldd state: " + change }

// StateTopic is the topic key engram upserts change's state record by.
func StateTopic(change string) string { return "ldd-state/" + change }

// EngramBin is the Engram command: JHON_ENGRAM, else `engram` on PATH; ""
// when it does not resolve, which leaves Engram off.
func EngramBin() string {
	bin := os.Getenv("JHON_ENGRAM")
	if bin == "" {
		bin = "engram"
	}
	p, err := exec.LookPath(bin)
	if err != nil {
		return ""
	}
	return p
}

// A touch is one file a change's diff touches and the names it declares there.
type touch struct {
	File  string
	Names []string
}

// ChangeState is a change's state at a transition, read before it is written.
type ChangeState struct {
	Change    string
	Phase     string
	Done      int
	Total     int
	LastRun   string
	Delivery  string
	touched   []touch
	Decisions []Decision
	Refs      []string
}

// StateOf is change's state in the repository whose top level is top, at
// phase, with delivery as the transition states it. diff is the git diff the
// refs come from: the slice's commit for apply ("HEAD^", "HEAD"), the change
// since its base otherwise (nil). Read-only; for an archive, read it before
// the change's directory moves.
func StateOf(top, change, phase, delivery string, diff []string) ChangeState {
	s := ChangeState{Change: change, Phase: phase, Delivery: delivery, LastRun: "none on record"}
	lay := layout.For(top)
	if b, err := os.ReadFile(filepath.Join(top, lay.Artifact(change, lay.Tasks()))); err == nil {
		src := string(b)
		p := ProgressOf(lay, &src)
		for _, x := range p.Slices {
			if x.Total == 0 {
				continue
			}
			s.Total++
			if x.Completed == x.Total {
				s.Done++
			}
		}
		if s.Total == 0 && p.Total > 0 {
			s.Total = 1
			if p.AllComplete {
				s.Done = 1
			}
		}
	}
	if run, err := execution.ReadLastRun(top, change); err == nil && run != nil {
		verdict := "failed"
		if run.Passed {
			verdict = "passed"
		}
		s.LastRun = strings.TrimSpace(verdict + " " + run.Timestamp)
		if run.LightVerify != nil {
			s.LastRun += ", light verify " + run.LightVerify.Verdict
		}
	}
	if diff == nil {
		if base, _, ok := ChangeBase(top); ok {
			diff = []string{base}
		}
	}
	if diff != nil {
		s.touched = touchedBy(top, diff)
	}
	return s
}

var (
	hunkFile = lazyre.New(`^\+\+\+ b/(.+)$`)
	// declared is a declaration's name in a changed line or a hunk's context.
	declared = lazyre.New(`(?:^|[\s(])(?:func|def|class|function|type|interface|struct|enum|trait|fn)\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)`)
)

// touchedBy is every file outside the change's record that `git diff -U0
// <diff...>` touches, with the names declared in its changed lines and its
// hunks' context, in order.
func touchedBy(top string, diff []string) []touch {
	out, ok := riskGit(top, append([]string{"diff", "-U0", "--no-color", "--no-ext-diff", "--no-renames", "--src-prefix=a/", "--dst-prefix=b/"}, diff...)...)
	if !ok {
		return nil
	}
	var touched []touch
	for _, line := range strings.Split(out, "\n") {
		if m := hunkFile.FindStringSubmatch(line); m != nil {
			if layout.For(top).Ignored(m[1]) {
				touched = append(touched, touch{}) // a record file: its names are skipped
				continue
			}
			touched = append(touched, touch{File: m[1]})
			continue
		}
		if len(touched) == 0 || touched[len(touched)-1].File == "" {
			continue
		}
		var text string
		switch {
		case strings.HasPrefix(line, "@@"):
			if _, ctx, ok := strings.Cut(strings.TrimPrefix(line, "@@"), "@@"); ok {
				text = ctx
			}
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			text = line[1:]
		}
		t := &touched[len(touched)-1]
		for _, m := range declared.FindAllStringSubmatch(text, -1) {
			if !slices.Contains(t.Names, m[1]) {
				t.Names = append(t.Names, m[1])
			}
		}
	}
	return slices.DeleteFunc(touched, func(t touch) bool { return t.File == "" })
}

// CodeNames are the names a diff (`git diff <diff...>`) declares in its
// changed lines, outside the change's record.
func CodeNames(top string, diff []string) []string {
	return codeNames(touchedBy(top, diff))
}

func codeNames(touched []touch) []string {
	var out []string
	for _, t := range touched {
		for _, n := range t.Names {
			if !slices.Contains(out, n) {
				out = append(out, n)
			}
		}
	}
	return out
}

// handoffQuery is the decisions search the handoff and the state record make
// (H5): the change's name, the areas its files touch, then the code's names,
// the symbols declared and the files' base names, at most twelve of them.
func handoffQuery(change string, files, names []string) string {
	var words []string
	if change != "" {
		words = append(words, change)
	}
	reasons := make([]Reason, 0, len(files))
	for _, f := range files {
		reasons = append(reasons, Reason{File: f})
	}
	words = append(words, AreasOf(reasons)...)
	code := 0
	for _, f := range files {
		names = append(names, path.Base(f))
	}
	for _, w := range names {
		if code < 12 && w != "" && !slices.Contains(words, w) {
			words = append(words, w)
			code++
		}
	}
	return strings.Join(words, " ")
}

// codegraphNode is the part of `codegraph query --json` a ref reads.
type codegraphNode struct {
	Node struct {
		Kind     string `json:"kind"`
		Name     string `json:"name"`
		FilePath string `json:"filePath"`
	} `json:"node"`
}

// RefsOf are the `<kind> <name> @ <path>` lines CodeGraph gives for the
// files and names touched, each file's ref before its symbols', at most
// maxRefs; none when the repository has no index (.codegraph/) or CodeGraph
// fails. Never a line number: name and path survive an edit.
func refsOf(ctx context.Context, top string, touched []touch) []string {
	if fi, err := os.Stat(filepath.Join(top, ".codegraph")); err != nil || !fi.IsDir() {
		return nil
	}
	bin, err := exec.LookPath("codegraph")
	if err != nil {
		return nil
	}
	type ask struct{ file, name, kind string }
	var asks []ask
	for _, t := range touched {
		asks = append(asks, ask{t.File, path.Base(t.File), "file"})
		for _, n := range t.Names {
			asks = append(asks, ask{t.File, n, ""})
		}
	}
	asks = asks[:min(len(asks), maxRefs)]
	found := make([]string, len(asks))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, a := range asks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			args := []string{"query", a.name, "--json", "--limit", "10"}
			if a.kind != "" {
				args = append(args, "--kind", a.kind)
			}
			cmd := exec.CommandContext(ctx, bin, args...)
			cmd.Dir = top
			cmd.WaitDelay = 100 * time.Millisecond
			out, err := cmd.Output()
			var nodes []codegraphNode
			if err != nil || json.Unmarshal(out, &nodes) != nil {
				return
			}
			for _, n := range nodes {
				if n.Node.FilePath == a.file && n.Node.Name == a.name && n.Node.Kind != "" {
					found[i] = n.Node.Kind + " " + n.Node.Name + " @ " + n.Node.FilePath
					return
				}
			}
		}()
	}
	wg.Wait()
	var refs []string
	for _, r := range found {
		if r != "" && !slices.Contains(refs, r) {
			refs = append(refs, r)
		}
	}
	return refs
}

// Content is the state record's body.
func (s ChangeState) Content() string {
	var b strings.Builder
	fmt.Fprintf(&b, "**What**: the LDD state of change %s, which jhon keeps at each slice commit, verify, and archive; the change's record is the source.\n", s.Change)
	fmt.Fprintf(&b, "change: %s\nphase: %s\nslices: %d of %d done\nlast test run: %s\ndelivery: %s\n", s.Change, s.Phase, s.Done, s.Total, s.LastRun, s.Delivery)
	if len(s.Decisions) == 0 {
		b.WriteString("decisions: none found\n")
	} else {
		b.WriteString("decisions:\n")
		for _, d := range s.Decisions {
			fmt.Fprintf(&b, "- #%v (%s) %s\n", d.ID, d.Type, d.Title)
		}
	}
	if len(s.Refs) > 0 {
		b.WriteString("Refs:\n")
		for _, r := range s.Refs {
			b.WriteString(r + "\n")
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// SaveState writes s to Engram as change's one state record, from top (so
// Engram resolves the project as the agents' saves do), within
// StateDeadline. It returns "" when written or when Engram does not resolve,
// and otherwise the one note line that says what became of the state.
//
// Inside Jhon's sandbox, where Engram's folder stays read-only, and where
// Engram fails on a read-only file system, the record is queued for Jhon to
// save outside the sandbox (engramqueue.go, T222 S1b), and the note says so;
// inside, Engram is not asked for the decisions either: it could not open
// its folder.
func SaveState(top string, s ChangeState) string {
	bin := EngramBin()
	if bin == "" {
		return ""
	}
	inside := sandbox.Inside()
	ctx, cancel := context.WithTimeout(context.Background(), StateDeadline)
	defer cancel()
	var files []string
	for _, t := range s.touched {
		files = append(files, t.File)
	}
	var wg sync.WaitGroup
	if !inside {
		wg.Go(func() {
			if d, ok := decisionsFor(ctx, bin, top, handoffQuery(s.Change, files, codeNames(s.touched))); ok {
				s.Decisions = d
			}
		})
	}
	wg.Go(func() { s.Refs = refsOf(ctx, top, s.touched) })
	wg.Wait()
	note := func(why string) string {
		return "jhon: the state of " + s.Change + " was not saved to Engram (" + why + "); the " + s.Phase + " stands"
	}
	if inside {
		return queueNote(top, s, note, "")
	}
	if ctx.Err() != nil {
		return note("no answer within " + StateDeadline.String())
	}
	stderr, err := engramSave(ctx, bin, top, StateTitle(s.Change), s.Content(), StateTopic(s.Change))
	if err != nil {
		why := saveFailure(ctx, err, stderr, StateDeadline)
		if ctx.Err() == nil && readOnlyFailure(stderr) {
			return queueNote(top, s, note, why)
		}
		return note(why)
	}
	return ""
}

// queueNote queues s in top's queue and says so; where the queue cannot be
// written, the note says why the state was not saved: why Engram failed,
// or inside the sandbox that it keeps Engram's folder read-only, then the
// queue's path and why it could not be written, both quoted.
func queueNote(top string, s ChangeState, note func(string) string, why string) string {
	queue, err := queueState(top, s)
	if err == nil {
		return "jhon: the state of " + s.Change + " was queued for Engram; Jhon saves it outside the sandbox"
	}
	// The queue's path and the error are quoted: a linked worktree's common
	// dir is a path a sandboxed command wrote (T227).
	where := fmt.Sprintf("the queue %q", queue)
	if queue == "" {
		where = "no queue"
	}
	failed := fmt.Sprintf(" could not be written: %q; fix: run jhon doctor", err.Error())
	if why == "" {
		return note("Jhon's sandbox keeps Engram's folder " + engramFolder() + " read-only, and " + where + failed)
	}
	return note(why + "; " + where + failed)
}

// ClosingChanges are the active changes of top's branch a commit there
// closes a slice of.
func ClosingChanges(top string) []string {
	active, _ := BranchChanges(top)
	return slices.DeleteFunc(slices.Clone(active), func(c string) bool { return len(closedSlices(top, c)) == 0 })
}

// HeadOf is HEAD's commit in top; ok is false when HEAD is not a commit.
func HeadOf(top string) (commit string, ok bool) {
	head := vcs.HeadSha(top)
	return head, head != ""
}

// AwaitSliceCommit waits up to wait for HEAD in top to move from after to a
// commit that closes a slice of change, and returns the slices it closed;
// nil when HEAD did not move, or moved to a commit that closes none.
func AwaitSliceCommit(top, change, after string, wait time.Duration) []string {
	lay := layout.For(top)
	rel := lay.Artifact(change, lay.Tasks())
	for end := time.Now().Add(wait); ; time.Sleep(200 * time.Millisecond) {
		if head := vcs.HeadSha(top); head != "" && head != after {
			now, _ := riskGit(top, "show", "HEAD:"+rel)
			before, _ := riskGit(top, "show", "HEAD^:"+rel)
			return newlyClosed(lay, before, now)
		}
		if time.Now().After(end) {
			return nil
		}
	}
}
