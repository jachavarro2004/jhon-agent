package cli

import (
	"fmt"
	"os"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/vcs"
)

// `jhon review freeze [--base <ref>] [--json]`: a review's frozen candidate
// (contracts/review-prism.md, "Freeze"): the merge base of HEAD and the base
// ref (HEAD by default), the working tree as a git tree, and the subject
// hash of the diff between them. It depends only on the bytes, so every facet
// freezes for itself and the same working state gives the same subject. It
// writes the working files' blobs to the object store, as `git add` would,
// and nothing else. Exit 0, or 2 on a usage error or a state it cannot
// freeze; an empty diff is such a state (T87a, L629): it would review
// nothing, so it is refused with the repository's worktrees that hold
// changes.
//
// `start`, `refute`, `validate` and `record` are the native Prism review
// (cmd_review_native.go); `run` is all of it through `claude -p`, Claude
// Code's variant (cmd_review_claude.go).
func init() { register("review", runReview) }

const reviewUsage = "usage: jhon review freeze [--base <ref>] [--json]\n" +
	"       jhon review start [--base <ref>] [--change <name>] [--json]\n" +
	"       jhon review refute --subject <hash> [--base <ref>] [--change <name>] < facet-results.json\n" +
	"       jhon review validate --subject <hash> [--base <ref>] [--change <name>] < refuted.json\n" +
	"       jhon review record --subject <hash> [--base <ref>] [--change <name>] < results.json\n" +
	"       jhon review run [--base <ref>] [--change <name>] [--timeout <seconds>]"

func runReview(argv []string) (int, error) {
	if len(argv) > 0 {
		switch argv[0] {
		case "start", "refute", "validate", "record":
			return runReviewNative(argv[0], argv[1:])
		case "run":
			return runReviewClaude(argv[1:])
		}
	}
	return runReviewFreeze(argv)
}

func runReviewFreeze(argv []string) (int, error) {
	if len(argv) == 0 || argv[0] != "freeze" {
		fmt.Fprintln(os.Stderr, reviewUsage)
		return 2, nil
	}
	base, json := "HEAD", false
	for i := 1; i < len(argv); i++ {
		switch {
		case argv[i] == "--json":
			json = true
		case argv[i] == "--base" && i+1 < len(argv):
			i++
			base = argv[i]
		default:
			fmt.Fprintln(os.Stderr, reviewUsage)
			return 2, nil
		}
	}
	f, err := vcs.Freeze(cwd(), base)
	if err != nil {
		fmt.Fprintf(os.Stderr, "jhon review freeze: %v\n", err)
		return 2, nil
	}
	if json {
		fmt.Println(js.StringifyValue(js.Obj("base", f.Base, "candidate", f.Candidate, "subject", f.Subject)))
		return 0, nil
	}
	fmt.Printf("BASE=%s\nCANDIDATE=%s\nSUBJECT=%s\n", f.Base, f.Candidate, f.Subject)
	return 0, nil
}
