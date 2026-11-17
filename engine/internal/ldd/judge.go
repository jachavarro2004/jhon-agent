package ldd

import (
	"slices"
	"strconv"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
)

// The tests to judge (T137, L1064 KB2, KB3): the tests the change's
// execution record marks as turned green by editing only their test
// (execution.MarkTurnedGreen) go with every launch of verify and of the 4R
// lenses for that change, as a `## Tests to judge` block after the launch's
// other blocks, so the validator looks at each and reports a weakened test
// as a finding. Nothing is refused; with nothing to judge there is no block.

// TestJudges are the agents launched with the tests to judge: verify and the 4R lenses.
var TestJudges = []string{"verify", "review-risk", "review-resilience", "review-readability", "review-reliability"}

const (
	// TestsToJudgeHeading opens the block.
	TestsToJudgeHeading = "## Tests to judge"
	// TestsToJudgeIntro is the block's line under its heading.
	TestsToJudgeIntro = "Added at launch from the change's execution record: each test below failed in one recorded run and passed in a later one while only its own test file changed in its project. Judge whether it was weakened to pass (an assertion dropped or loosened, a case removed, an expectation rewritten to what the code does); if it was, report it as a finding."
	// testsToJudgeLines is the most tests the block lists.
	testsToJudgeLines = 20
)

// TestsToJudge is the tests change's record marks as turned green by
// editing only their test; for no change, those of the change status
// selects at root. None when there is no change or no mark.
func TestsToJudge(root string, change *string) []execution.TurnedGreen {
	if change == nil {
		s, err := StatusOf(root, nil)
		if err != nil || s.Change == nil {
			return nil
		}
		change = s.Change
	}
	if err := execution.Vocabulary(); err != nil {
		return nil
	}
	return execution.TurnedGreenOf(root, *change)
}

// TestsToJudgeBlock is the block that lists marks, or "" with none.
func TestsToJudgeBlock(marks []execution.TurnedGreen) string {
	if len(marks) == 0 {
		return ""
	}
	lines := []string{TestsToJudgeHeading, TestsToJudgeIntro}
	for i, m := range marks {
		if i == testsToJudgeLines {
			lines = append(lines, "- … and "+strconv.Itoa(len(marks)-i)+" more in the change's execution record (turned_green)")
			break
		}
		lines = append(lines, "- `"+m.Test+"` ("+m.Project+", "+m.File+"): failed in run "+m.RedRun+", passed in run "+m.GreenRun)
	}
	return strings.Join(lines, "\n")
}

// JudgeContext is c with `tests_to_judge` (TestsToJudge) when there is one
// to judge: `jhon context --judge`.
func JudgeContext(c Context) Context {
	if marks := TestsToJudge(c.ProjectRoot, c.Change); len(marks) > 0 {
		return c.with(Field{"tests_to_judge", marks})
	}
	return c
}

// LaunchTestsToJudge is the block a launch of agent in cwd carries: the
// tests to judge of the change status selects at cwd's project root
// (ContextRoot), when agent is one of TestJudges; "" otherwise.
func LaunchTestsToJudge(agent, cwd string) string {
	if !slices.Contains(TestJudges, agent) {
		return ""
	}
	return TestsToJudgeBlock(TestsToJudge(ContextRoot(cwd), nil))
}
