package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/discover"
	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/vcs"
)

const executionsVerifyUsage = "usage: jhon executions verify <change> --verdict pass|fail [--note <text>] [--json] [--root <project>]"

// runExecutionsVerify records a light verify (T31.5): `jhon executions
// verify <change> --verdict pass|fail [--note <text>]` appends a note with
// the time and HEAD's commit and tree to the change's execution record,
// beside its RED and GREEN runs. Exit 0 when recorded, 2 on a usage error,
// a change with no run on record, or a pass of a change whose runs show no
// RED before GREEN (the route is not read: every change).
func runExecutionsVerify(argv []string) (int, error) {
	if err := execution.Vocabulary(); err != nil {
		return 1, err
	}
	asJSON := false
	var verdict, note, rootArg *string
	var rest []string
	usage := func() (int, error) {
		fmt.Fprintln(os.Stderr, executionsVerifyUsage)
		return 2, nil
	}
	for i := 0; i < len(argv); i++ {
		switch a := argv[i]; a {
		case "--json":
			asJSON = true
		case "--verdict", "--note", "--root":
			i++
			if i >= len(argv) || a != "--note" && strings.HasPrefix(argv[i], "-") {
				return usage()
			}
			v := argv[i]
			switch a {
			case "--verdict":
				verdict = &v
			case "--note":
				note = &v
			default:
				rootArg = &v
			}
		default:
			rest = append(rest, a)
		}
	}
	if len(rest) != 1 || strings.HasPrefix(rest[0], "-") || verdict == nil || *verdict != execution.VerdictPass && *verdict != execution.VerdictFail {
		return usage()
	}
	root := discover.FindRoot(cwd())
	if rootArg != nil {
		root = project.Resolve(cwd(), *rootArg)
	}
	change := rest[0]
	if !activeChange(root, change) {
		fmt.Fprintf(os.Stderr, "jhon executions: %s/ does not exist or is not an active change under %s\n", filepath.Join(execution.ChangesOf(root), change), root)
		return 2, nil
	}
	if last, err := execution.ReadLastRun(root, change); err == nil && last != nil && *verdict == execution.VerdictPass && !last.RedBeforeGreen {
		fmt.Fprintf(os.Stderr, "jhon executions: no RED before GREEN on record for %s: a light verify passes a change whose new test failed first; %s\n", change, ldd.RedGreenHint(change))
		return 2, nil
	}
	c := vcs.CandidateOf(root, execution.ChangeRecordPaths(root, change))
	n := execution.VerifyNote{Timestamp: now(), Verdict: *verdict, Commit: c.Commit, Tree: c.Tree}
	if note != nil {
		n.Note = *note
	}
	if err := execution.AddLightVerify(root, change, n); errors.Is(err, execution.ErrNoRun) {
		fmt.Fprintf(os.Stderr, "jhon executions: no run on record for %s: a light verify judges a tested change; run `jhon executions run %s` first\n", change, change)
		return 2, nil
	} else if err != nil {
		return 1, err
	}
	// The verify is on record: the change's state goes to Engram (T134 H4);
	// a write that fails is one note on stderr, and the exit status stands.
	if engramOn() {
		saveState(root, ldd.StateOf(root, change, "verify", "light verify "+n.Verdict+" of tree "+n.Tree, nil))
	}
	if asJSON {
		return 0, printJSON(n)
	}
	fmt.Printf("light verify %s recorded for %s (tree %s) in %s\n", n.Verdict, change, n.Tree, execution.RecordShown(root, change))
	return 0, nil
}
