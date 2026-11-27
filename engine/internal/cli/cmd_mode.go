package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/grant"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/workspace"
)

// `jhon mode`: the owner's permission mode for one project (S31).
//
//	jhon mode [--root <path>] [--json]
//	jhon mode normal|yolo|super-yolo [--root <path>] [--json]
//
// yolo lets the guards pass every class they ask about but destroy without
// asking, super-yolo destroy too, and normal is the guards as they are; a
// refusal stays a refusal in every mode. A mode covers the project and its
// worktrees (the root resolves as a grant's does) and lasts until the owner
// sets another. Setting one is the owner's act, which the safety guard
// refuses from an agent; reading it is anyone's. Exit 0, 2 on a usage error
// (nothing written).
func init() { register("mode", runMode) }

var modeUsage = `usage: jhon mode [normal|yolo|super-yolo] [--root <path>] [--json]

  normal      the guards ask as they do
  yolo        nothing asks but destroy (a deletion that loses work) and a
              command the guard cannot read, which may hide one
  super-yolo  nothing asks, destroy included
  in every mode, writing Claude Code settings or a git config that changes
  what git runs still asks
  (either could switch the guards off), and a refusal stays one
  with no mode, prints the project's; a mode lasts until you set another
  --root      the project (default: the git toplevel of the cwd); a folder
              that is not a git repository takes no mode, and is normal
  --json      {"mode", "root", "since"}, and "from" when the mode is taken
              from the primary
  a workspace member the primary lists, joined, with no mode of its own
  takes the primary's: printed with "from <primary>"
`

func modeUsageError(msg string) (int, error) {
	fmt.Fprintf(os.Stderr, "jhon mode: %s\n\n%s", msg, modeUsage)
	return 2, nil
}

func runMode(argv []string) (int, error) {
	var mode, root *string
	asJSON := false
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		switch {
		case a == "--help" || a == "-h":
			fmt.Print(modeUsage)
			return 0, nil
		case a == "--json":
			asJSON = true
		case a == "--root":
			i++
			if i >= len(argv) || strings.HasPrefix(argv[i], "-") {
				return modeUsageError("--root requires a value")
			}
			root = &argv[i]
		case strings.HasPrefix(a, "-"):
			return modeUsageError("unknown option: " + a)
		case mode != nil:
			return modeUsageError("one mode at a time")
		case !slices.Contains(grant.Modes, a):
			return modeUsageError(fmt.Sprintf("unknown mode '%s'", a))
		default:
			mode = &argv[i]
		}
	}
	path := cwd()
	if root != nil {
		path = *root
	}
	top := grant.Root(project.Root(project.Resolve(cwd(), path)))
	if top == "" {
		return modeUsageError(path + " is not inside a project: " + noProject + "; a mode is a project's, and calls there are judged as in normal mode")
	}
	rec := grant.ModeOf(top)
	// A member with no mode of its own takes its primary's (T281).
	from := ""
	if mode != nil {
		var err error
		if rec, err = grant.SetMode(top, *mode); err != nil {
			return 1, err
		}
	} else if taken, primary := workspace.ModeOf(top, top); primary != "" {
		from = primary
		if asJSON {
			rec = taken
		} else {
			fmt.Printf("%s for %s, from %s since %s: a member of its workspace with no mode of its own takes the primary's (`jhon mode <mode>` here gives it one of its own)\n", taken.Mode, top, primary, taken.Since)
			return 0, nil
		}
	}
	if asJSON {
		// since is null for a project whose owner never set a mode.
		out := struct {
			Mode  string  `json:"mode"`
			Root  string  `json:"root"`
			Since *string `json:"since"`
			From  *string `json:"from,omitempty"`
		}{rec.Mode, top, nil, nil}
		if rec.Since != "" {
			out.Since = &rec.Since
		}
		if from != "" {
			out.From = &from
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		return 0, enc.Encode(out)
	}
	if rec.Since == "" {
		fmt.Printf("%s for %s\n", rec.Mode, rec.Root)
	} else {
		fmt.Printf("%s for %s since %s\n", rec.Mode, rec.Root, rec.Since)
	}
	return 0, nil
}
