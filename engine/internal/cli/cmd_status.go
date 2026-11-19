package cli

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"os"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/discover"
	"github.com/jachavarro2004/jhon-agent/engine/internal/grant"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lddswitch"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/workspace"
)

// `jhon status [change] [--json | --brief] [--root <project>]`:
// what a change directory holds and one advisory next
// phase (internal/ldd, status.go). Read-only. Exit 0 with a status, 1 when
// no change can be selected, 2 on a usage error.
func init() { register("status", runStatus) }

func runStatus(argv []string) (int, error) {
	// The vocabulary is read first, before the arguments.
	if err := ldd.StatusVocabulary(); err != nil {
		return 1, err
	}
	json := slices.Contains(argv, "--json")
	short := slices.Contains(argv, "--brief")
	rootIdx := slices.Index(argv, "--root")
	var root string
	if rootIdx > -1 {
		arg := "."
		if rootIdx+1 < len(argv) {
			arg = argv[rootIdx+1]
		}
		root = project.Resolve(cwd(), arg)
	} else {
		root = discover.FindRoot(cwd())
	}
	var rest []string
	for i, a := range argv {
		if a != "--json" && a != "--brief" && (rootIdx < 0 || (i != rootIdx && i != rootIdx+1)) {
			rest = append(rest, a)
		}
	}
	if len(rest) > 1 || slices.ContainsFunc(rest, func(a string) bool { return strings.HasPrefix(a, "-") }) || (json && short) {
		fmt.Fprintln(os.Stderr, "usage: jhon status [change] [--json | --brief] [--root <project>]")
		return 2, nil
	}
	var name *string
	if len(rest) == 1 {
		name = &rest[0]
	}
	s, err := ldd.StatusOf(root, name)
	if err != nil {
		return 1, err
	}
	s.Mode = grant.ModeOf(root).Mode
	// The notes and the strict_tdd reason name the project's own layout (L479).
	for i := range s.Notes {
		s.Notes[i] = layout.Words(root, s.Notes[i])
	}
	s.StrictTdd.Reason = layout.Words(root, s.StrictTdd.Reason)
	for i := range s.StrictTdd.Notes {
		s.StrictTdd.Notes[i] = layout.Words(root, s.StrictTdd.Notes[i])
	}
	// LDD is always on (T72): the line and its JSON keep their shape.
	s.Ldd = &ldd.LddState{State: lddswitch.On, Source: lddswitch.SourceDefault, Active: project.UsesLdd(root)}
	// The multi-repository workspace (T57 W2): reported, and its notes
	// added, only for a primary or a member; a pointer to a primary that
	// does not list this repository is a note.
	if root != "" {
		ws := workspace.Of(root)
		s.Notes = append(s.Notes, ws.Notes...)
		if ws.Role != workspace.None {
			s.Workspace = &ws
		}
		// A primary's change shows its members' deliveries (T57 slice 7).
		if s.Change != nil {
			s.Members = ldd.MemberDeliveries(root, *s.Change, ws)
		}
	}
	switch {
	case json:
		out, err := js.Stringify(s)
		if err != nil {
			return 1, err
		}
		fmt.Println(out)
	case short:
		fmt.Println(ldd.Brief(s))
	default:
		fmt.Println(ldd.Human(s))
	}
	if !s.Ok {
		return 1, nil
	}
	return 0, nil
}
