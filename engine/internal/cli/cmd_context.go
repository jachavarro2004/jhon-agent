package cli

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// `jhon context [<change>] --phase <phase|none> [--json] [--root <project>]
// [--handoff] [--judge]` (docket Y): the launch context of one
// delegation, as one line of JSON (internal/ldd, context.go). The context
// is information, not a gate: a root below `ready`, or no change, still
// yields one, with exit 0. Exit 2 is a usage error, an unknown phase
// included, and prints nothing on stdout.
//
// `--judge` adds `tests_to_judge` when the change (or, with none named, the
// change status selects) has tests its record marks as turned green by
// editing only their test (T137): what verify and the 4R lenses are
// launched with to judge (ldd.TestsToJudge). It asks no Engram.
//
// `--save-state --after <sha>` is not a context: route-guard starts it,
// detached, once it accepts a commit that closes a slice of <change>; it
// waits for that commit to land and writes the change's state to Engram
// (cmd_context_state.go, T134 H4).
func init() { register("context", runContext) }

func runContext(argv []string) (int, error) {
	phases := ldd.ContextPhases()
	usage := "usage: jhon context [<change>] --phase <" + strings.Join(phases, "|") + "> [--json] [--root <project>] [--handoff] [--judge]"
	var phase, root, after *string
	handoff, judge, saveState := false, false, false
	var rest []string
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		switch a {
		case "--json": // the output is always JSON; the flag is accepted for symmetry
		case "--handoff":
			handoff = true
		case "--judge":
			judge = true
		case "--save-state":
			saveState = true
		case "--phase", "--root", "--after":
			i++
			if i >= len(argv) || strings.HasPrefix(argv[i], "-") {
				fmt.Fprintln(os.Stderr, usage)
				return 2, nil
			}
			switch a {
			case "--phase":
				phase = &argv[i]
			case "--after":
				after = &argv[i]
			default:
				root = &argv[i]
			}
		default:
			rest = append(rest, a)
		}
	}
	if phase == nil || len(rest) > 1 || slices.ContainsFunc(rest, func(a string) bool { return strings.HasPrefix(a, "-") }) {
		fmt.Fprintln(os.Stderr, usage)
		return 2, nil
	}
	if !slices.Contains(phases, *phase) {
		fmt.Fprintf(os.Stderr, "jhon context: unknown phase '%s'\n%s\n", *phase, usage)
		return 2, nil
	}
	var dir string
	if root != nil {
		dir = project.Resolve(cwd(), *root)
	} else {
		dir = ldd.ContextRoot(cwd())
	}
	var change *string
	if len(rest) == 1 {
		change = &rest[0]
	}
	if saveState || after != nil {
		if !saveState || after == nil || change == nil {
			fmt.Fprintln(os.Stderr, usage)
			return 2, nil
		}
		saveStateAfterCommit(dir, *change, *phase, *after)
		return 0, nil
	}
	c, err := ldd.ResolveContext(dir, change, *phase)
	if err == nil && handoff {
		c, err = ldd.HandoffContext(c)
	}
	if err == nil && judge {
		c = ldd.JudgeContext(c)
	}
	if err != nil {
		return 1, err
	}
	out, err := js.StringifyCompact(c)
	if err != nil {
		return 1, err
	}
	fmt.Println(out)
	return 0, nil
}
