package ldd

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// An umbrella change (T37, L365-L368) is a starting point other changes
// come out of, an inventory, never the work in progress itself: its
// .openspec.yaml says `kind: umbrella`, or it holds an exploration.md and
// no other artifact (research.md, proposal.md, specs/, design.md,
// tasks.md) and no execution record of its own: a change whose tests ran
// is in progress. `jhon status` never selects one by default, lists it apart,
// and suggests start-change for it: split it into changes.

// umbrellaKind is the .openspec.yaml value that marks an umbrella.
const umbrellaKind = "umbrella"

// IsUmbrella is whether change, active under root, is an umbrella.
func IsUmbrella(root, change string) bool {
	l := layout.For(root)
	dir := filepath.Join(root, changesOf(root), change)
	if b, err := os.ReadFile(filepath.Join(dir, ".openspec.yaml")); err == nil && contract.Scalar(string(b), "kind") == umbrellaKind {
		return true
	}
	if !project.Exists(filepath.Join(dir, "exploration.md")) {
		return false
	}
	return !slices.ContainsFunc([]string{"research.md", "proposal.md", l.DeltaSpecs(), "design.md", l.Tasks(), filepath.Base(execution.RecordPath(root, change))}, func(f string) bool {
		return project.Exists(filepath.Join(dir, f))
	})
}

// splitUmbrellas parts the active changes into the umbrellas and the rest, each in active's order.
func splitUmbrellas(root string, active []string) (candidates, umbrellas []string) {
	candidates, umbrellas = []string{}, []string{}
	for _, c := range active {
		if IsUmbrella(root, c) {
			umbrellas = append(umbrellas, c)
		} else {
			candidates = append(candidates, c)
		}
	}
	return candidates, umbrellas
}

// umbrellaHow is what an umbrella's start-change asks for: its changes, or,
// for a new change that only has its exploration yet, its own record.
const umbrellaHow = "split it into changes, or write its proposal or tasks.md if it is one change"

// umbrellaNext is how an umbrella's next step reads.
func umbrellaNext(change string) string {
	return change + " (" + sv().startChange + ": " + umbrellaHow + ")"
}

// moved is the latest movement of change: its tasks.md or its execution
// record (execution.RecordFile), the zero time without either.
func moved(root, change string) time.Time {
	var latest time.Time
	for _, f := range []string{filepath.Join(root, changesOf(root), change, layout.For(root).Tasks()), execution.RecordFile(root, change)} {
		if st, err := os.Stat(f); err == nil && st.ModTime().After(latest) {
			latest = st.ModTime()
		}
	}
	return latest
}

// RankChanges orders changes by pending work: those with unchecked tasks
// or a slice still outlined first, then the latest movement (moved), then name.
func RankChanges(root string, changes []string) []string {
	type ranked struct {
		name    string
		pending bool
		at      time.Time
	}
	rs := make([]ranked, len(changes))
	l := layout.For(root)
	for i, c := range changes {
		var src *string
		if b, err := os.ReadFile(filepath.Join(root, changesOf(root), c, l.Tasks())); err == nil {
			src = new(string(b))
		}
		p := ProgressOf(l, src)
		rs[i] = ranked{c, p.Pending > 0 || len(p.Outlined()) > 0, moved(root, c)}
	}
	slices.SortStableFunc(rs, func(a, b ranked) int {
		switch {
		case a.pending != b.pending:
			if a.pending {
				return -1
			}
			return 1
		case !a.at.Equal(b.at):
			return b.at.Compare(a.at)
		}
		return js.Compare(a.name, b.name)
	})
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.name
	}
	return out
}

// umbrellaLine is the umbrellas as the human output lists them apart.
func umbrellaLines(umbrellas []string) []string {
	out := make([]string, len(umbrellas))
	for i, u := range umbrellas {
		out[i] = strings.TrimRight("umbrella    "+umbrellaNext(u), " ")
	}
	return out
}
