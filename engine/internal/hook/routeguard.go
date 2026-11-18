package hook

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// RouteGuard holds every agent to LDD and the route its change takes
// (ldd/route.go) in a repository with LDD's layout: `route-guard --agent <owner>`.
// In a repository without one, a commit or a PR is refused until LDD is
// set up (ldd.SetUpRefusal, T72), and so is the first code edit, by a tool
// or by a Bash write (ldd.SetUpEditRefusal, T89): every file in the
// repository outside .git and the layout's directory is code.
//
//   - The record is never refused: a spec delta, tasks.md, a proposal, and
//     a design are written on any route, when the change needs one or the
//     owner asks (`jhon status` suggests a proposal and a design when the
//     route calls for them).
//   - A PR of a change that alters behaviour opens only once the branch
//     carries its RED before GREEN in `jhon executions`, and its record when
//     the change needs one (ldd.Recorded, ldd.NeedsOf), and on the standard
//     route a passing light verify of the tree its last run tested
//     (execution.LightVerified, T31.5), but for a small change (T146g):
//     tasks.md and a spec
//     delta only when needed; at risk passive or medium a tasks.md with an
//     `## Intent` and a `Delivers:` line is enough. A change with no folder,
//     the one its branch names, counts too.
//   - A bounded writer (the names route-guard is given) is launched only
//     with its `## Allowed edit surfaces`: without them scope-guard has no
//     scope to hold it to (pi's sub-agents refuse such a task too).
//   - The verify agent may always be launched, on any route. 4R reviewers run
//     without asking when 4R is due (risk at the verified tier, or
//     review_required naming 4r), which the owner approved; its correction
//     is a worker's. Judgment Day agents (jd-*, jd-fix-agent with them) run
//     without asking when review_required names jd. Otherwise a reviewer
//     is asked for at the top level, where the owner answers, and refused
//     from a sub-agent; no grant stands in for that answer, but the
//     owner's yolo or super-yolo mode passes it with a note (L154). A review_required entry jhon cannot read refuses the PR
//     until it is fixed. On the verified route (risk at the verified tier;
//     size alone never puts a change on it) a PR opens only once the branch's
//     change has its independent verification (ldd.BranchVerified), and a
//     commit that closes a slice before then passes with a note that verify
//     is due; on a risk-high route it waits for its 4R too, and on any route
//     for what review_required names (ldd.ReviewsOf).
//   - A commit that closes a slice adding more lines of code than the
//     advisory size (ldd.SliceLines) passes with a note that says so;
//     with verify due too, both are one note. One whose route now asks for
//     a proposal and a design the change lacks names the files to write
//     (ldd.DesignDueNotes), a note as `jhon status` says it (T101).
//   - With `delivery: change`, a commit that closes a slice commits the
//     tree the change's last test run tested (ldd.SliceCommitRefusal); a PR
//     opens once the branch's change has no unchecked task, and merges with
//     --rebase.
//   - A commit or a PR is judged at the repository it runs in: a cd, a
//     pushd, env -C or git -C to another worktree moves it there from the
//     session's root (deliveryRoots, T140); one the guard cannot place is
//     judged at the session's root.
//   - While the branch carries an active change, a whole test suite run by
//     hand passes with a one-line note: `jhon executions` runs the change's
//     tests and records them; a focused TDD run passes silently.
func RouteGuard(args []string, io IO) int {
	scoped, name, bounded := owner(args)
	var p Payload
	block := func(reason string) int {
		fmt.Fprintf(io.Err, "route-guard: %s\n%s", reason, staleNote(p))
		return 2
	}
	p, ok := read(io.In)
	if !ok {
		return block("hook payload is not valid JSON")
	}
	if othersCall(scoped, name, p) {
		return 0
	}
	// A bounded writer is launched with its scope, in any project.
	if kind := strings.TrimSpace(p.Obj("tool_input").Str("subagent_type")); p.Str("tool_name") == "Agent" && slices.Contains(bounded, kind) {
		if _, ok := guard.SurfacesOf(p.Obj("tool_input").Str("prompt")); !ok {
			return block("a " + kind + " is launched with its scope: list the files it may edit under a `## Allowed edit surfaces` heading, one path or narrow glob per line; without it nothing holds the writer to its task (contracts/efficiency.md, \"Writer scope\").")
		}
	}
	cwd := p.Cwd()
	top := project.Root(cwd)
	if !project.UsesLdd(top) {
		// Jhon always works under LDD (T72, L557): in a repository with no
		// layout nothing is refused but delivery, a commit or a PR, until LDD
		// is set up; a folder in no repository delivers nothing.
		if command, _ := p.Obj("tool_input")["command"].(string); top != "" && p.Str("tool_name") == "Bash" && deliveryOf(command).any() {
			return block(ldd.SetUpRefusal())
		}
		// The first code edit is refused too (T89, L646), so LDD is set up
		// before any code changes: a file the call writes in the repository,
		// outside .git and every layout's directory, is code.
		if top != "" && slices.ContainsFunc(writtenBy(p, cwd), func(f string) bool { return isCode(top, f) }) {
			return block(ldd.SetUpEditRefusal())
		}
		// A write whose file the parser cannot name (inline interpreter
		// code, git apply, patch, a variable target) counts as one in the
		// repository when it runs there (T94, L652, L656).
		if command, _ := p.Obj("tool_input")["command"].(string); top != "" && p.Str("tool_name") == "Bash" &&
			slices.ContainsFunc(guard.UnresolvedWrites(command, cwd), func(u guard.UnresolvedWrite) bool { return unresolvedCode(top, u) }) {
			return block(ldd.SetUpEditRefusal())
		}
		return 0
	}
	routeOf, reviewsOf := judged(top)
	// ask has the owner asked at the top level; elsewhere it refuses, as
	// only the owner answers it. The owner's mode for the project passes it
	// with a note, as safety-guard's asks.
	ask := func(cls, what string) int {
		if m, ok := modeFor(top, "", cls); ok {
			passByMode(io, p, "route-guard", []modePass{{m, cls, what}})
			return 0
		}
		if canAsk(p) {
			fmt.Fprintln(io.Out, askJSON(cls+": "+what))
			return 0
		}
		return block(what + ": launch it from the main session, where the owner is asked.")
	}

	switch tool := p.Str("tool_name"); tool {
	case "Agent":
		kind := strings.TrimSpace(p.Obj("tool_input").Str("subagent_type"))
		switch {
		case strings.HasPrefix(kind, "review-") && owes(reviewsOf(), ldd.ReviewFourR):
			// A 4R runs only the lenses its diff calls for (L46); another
			// lens is the owner's call.
			lens := strings.TrimPrefix(kind, "review-")
			if pertinent := routeOf().Lenses; slices.Contains(ldd.Lenses, lens) && !slices.Contains(pertinent, lens) {
				return ask("review", "the "+lens+" lens is not pertinent to this change: its diff calls for ("+strings.Join(pertinent, ", ")+") (contracts/review-4r.md, \"Lenses\")")
			}
			return 0
		case strings.HasPrefix(kind, "jd-") && owes(reviewsOf(), ldd.ReviewJD):
			// The owner approved 4R by risk (L9) and configured review_required;
			// jd-fix-agent serves Judgment Day only (L19).
			return 0
		case strings.HasPrefix(kind, "review-") || strings.HasPrefix(kind, "jd-"):
			return ask("review", "a "+kind+" reviewer runs only when the owner asks for a 4R review or a Judgment Day")
		}
		return 0
	case "Bash":
		command, _ := p.Obj("tool_input")["command"].(string)
		d := deliveryOf(command)
		// A commit or a PR is judged at the repository it runs in, which a
		// cd, a pushd or git -C may move from the session's root (T140);
		// one the guard cannot place is judged at the root, as before.
		type judging struct {
			route   func() ldd.Route
			reviews func() []ldd.Review
		}
		byRoot := map[string]judging{top: {routeOf, reviewsOf}}
		judgedAt := func(root string) (func() ldd.Route, func() []ldd.Review) {
			j, ok := byRoot[root]
			if !ok {
				j.route, j.reviews = judged(root)
				byRoot[root] = j
			}
			return j.route, j.reviews
		}
		prRoots := []string{top}
		if d.pr {
			prRoots = deliveryRoots(command, cwd, top, true)
		}
		// A repository without LDD's layout answers as a session rooted
		// there does: nothing is delivered until LDD is set up (T72).
		notSetUp := func(roots []string) bool {
			return slices.ContainsFunc(roots, func(r string) bool { return !project.UsesLdd(r) })
		}
		if notSetUp(prRoots) {
			return block(ldd.SetUpRefusal())
		}
		for _, root := range prRoots {
			r, v := judgedAt(root)
			if why := deliveryRefusal(root, command, r, v); why != "" {
				return block(layout.Words(root, why))
			}
		}
		var notes []string
		if note := suiteNote(top, command); note != "" {
			notes = append(notes, layout.Words(top, note))
		}
		if d.commit {
			roots := deliveryRoots(command, cwd, top, false)
			if notSetUp(roots) {
				return block(ldd.SetUpRefusal())
			}
			for _, root := range roots {
				if why := ldd.SliceCommitRefusal(root); why != "" {
					return block(layout.Words(root, why))
				}
			}
			// A slice closed on the verified route learns that verify is due,
			// and one over the advisory size that it is: one note, never a refusal.
			for _, root := range roots {
				if !ldd.ClosesSlice(root) {
					continue
				}
				r, _ := judgedAt(root)
				var due []string
				if why, ok := verifyDue(root, r()); ok {
					due = append(due, "route verified ("+why+"): verify is due before delivery; launch the verify agent and record verify-report.md before the PR")
				}
				due = append(due, ldd.DesignDueNotes(root, r())...)
				for _, s := range ldd.ClosedSliceLines(root, r()) {
					if s.Over() {
						due = append(due, s.Note())
					}
				}
				for _, n := range due {
					notes = append(notes, layout.Words(root, n))
				}
				// Accepted: once it lands, the change's state goes to Engram.
				startStateWrites(root)
			}
		}
		if len(notes) > 0 {
			fmt.Fprint(io.Out, marshal(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PreToolUse",
				"additionalContext": strings.Join(notes, "; ")}}))
		}
		return 0
	default:
		return 0
	}
}

// judged is the route and the reviews of the branch in the repository at
// top, each read once, when first asked for.
func judged(top string) (routeOf func() ldd.Route, reviewsOf func() []ldd.Review) {
	var route *ldd.Route
	routeOf = func() ldd.Route {
		if route == nil {
			r, err := ldd.RouteOf(top)
			if err != nil {
				r = ldd.Route{Level: ldd.RouteVerified, Design: true, Behaviour: true, Reasons: []string{"the config cannot be read: " + err.Error()}}
			}
			route = &r
		}
		return *route
	}
	var reviews *[]ldd.Review
	reviewsOf = func() []ldd.Review {
		if reviews == nil {
			v := branchReviews(top, routeOf())
			reviews = &v
		}
		return *reviews
	}
	return routeOf, reviewsOf
}

// writtenBy is every file the call writes, absolute: an Edit's, a Write's,
// or a NotebookEdit's path, or what a Bash command writes as the safety
// guard reads it (guard.WrittenFiles).
func writtenBy(p Payload, cwd string) []string {
	if p.Str("tool_name") == "Bash" {
		command, _ := p.Obj("tool_input")["command"].(string)
		var out []string
		for _, f := range guard.WrittenFiles(command, cwd) {
			out = append(out, homeTilde(f))
		}
		return out
	}
	_, paths, _ := writes(p)
	var out []string
	for _, f := range paths {
		out = append(out, project.Resolve(cwd, f))
	}
	return out
}

// unresolvedCode is whether u, a write whose file is unknown, may write
// code in the repository at top: a file its code names is code, or, naming
// none, it runs inside the repository outside .git and the layouts.
func unresolvedCode(top string, u guard.UnresolvedWrite) bool {
	if len(u.Targets) > 0 {
		return slices.ContainsFunc(u.Targets, func(f string) bool { return isCode(top, homeTilde(f)) })
	}
	dir := project.Realish(u.Dir)
	return dir == "" || dir == top || isCode(top, dir)
}

// homeTilde is f with an unexpanded `~` the shell parser left joined to a
// directory (`<dir>/~/x`, from `> ~/x`) put back under HOME, so a write to
// the home directory is not read as one inside the repository (L652).
func homeTilde(f string) string {
	parts := strings.Split(filepath.ToSlash(f), "/")
	for i, part := range parts {
		if part != "~" {
			continue
		}
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return f
		}
		return filepath.Join(append([]string{home}, parts[i+1:]...)...)
	}
	return f
}

// isCode is whether f, a file written, is code in the repository at top:
// anything inside it but .git and the layouts' directories (openspec/,
// ldd/), which setting LDD up writes.
func isCode(top, f string) bool {
	f = project.Realish(f)
	if !project.Within(f, top) || f == top {
		return false
	}
	rel, err := filepath.Rel(top, f)
	if err != nil {
		return false
	}
	first := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
	if first == ".git" {
		return false
	}
	for _, l := range layout.Layouts {
		if first == l.Dir() {
			return false
		}
	}
	return true
}

var (
	prMerge = lazyre.New(`\bgh\s+pr\s+merge\b`)
	rebase  = lazyre.New(`(?:^|\s)(?:--rebase|-r)(?:\s|$)`)
)

// deliveryRefusal is why a PR command is refused, "" when it may run.
func deliveryRefusal(top, command string, routeOf func() ldd.Route, reviewsOf func() []ldd.Review) string {
	byChange := ldd.DeliveryUnit(top) == "change"
	if prMerge.MatchString(command) && byChange && !rebase.MatchString(command) {
		return "delivery: change merges with rebase, so each slice stays its own commit: run `gh pr merge <n> --rebase --delete-branch`."
	}
	if !deliveryOf(command).pr {
		return ""
	}
	active, archived := ldd.BranchChanges(top)
	lay := layout.For(top)
	dir := func(c string) string { return layout.At(top, lay.ChangeDir(c)) }
	if byChange {
		for _, c := range active {
			if left := ldd.Unchecked(lay, dir(c)); len(left) > 0 {
				return fmt.Sprintf("delivery: change opens one PR per change, after its last slice: %s has %d unchecked task(s) (%s). Commit each slice on the branch and open the PR when every task is done.", c, len(left), strings.Join(left[:min(3, len(left))], ", "))
			}
		}
		// An outlined slice is a slice not done (T127): the change's PR
		// waits for it as `jhon deliver check --action merge` does.
		for _, c := range active {
			if left := ldd.Outlined(lay, dir(c)); len(left) > 0 {
				return fmt.Sprintf("delivery: change opens one PR per change, after its last slice: %s has %d outlined slice(s) with no tasks yet (%s). Task and commit each slice on the branch and open the PR when every slice is done.", c, len(left), strings.Join(left[:min(3, len(left))], ", "))
			}
		}
	}
	if undocumented := ldd.UndocumentedProjects(top); len(undocumented) > 0 {
		return fmt.Sprintf("docs_with_code: %s change(s) code but not its docs/. Update the documentation the change makes stale before the PR, or state `No docs change: <why>` in the change's tasks.md.", strings.Join(undocumented, ", "))
	}
	if r := routeOf(); r.Behaviour {
		// The branch's changes: active, archived, and the one its name gives
		// when it has no folder (ldd.FolderlessChange).
		changes := slices.Clone(active)
		for _, c := range archived {
			changes = append(changes, filepath.Join("archive", c))
		}
		folderless := ldd.FolderlessChange(top)
		if folderless != "" {
			changes = append(changes, folderless)
		}
		n := ldd.NeedsOf(top, r)
		documented := slices.ContainsFunc(changes, func(c string) bool { return ldd.Recorded(top, c, dir(c), r, n) })
		redGreen := slices.ContainsFunc(changes, func(c string) bool { return execution.RedBeforeGreen(top, c) })
		named := "<change>"
		if folderless != "" {
			named = folderless
		} else if len(active) == 1 {
			named = active[0]
		}
		switch {
		case (documented || !n.Tasks && !n.Spec) && !redGreen:
			hint := ldd.RedGreenHint(named)
			if folderless != "" && !n.Tasks {
				// T146h L4: a small change's two runs are the bare one.
				hint = ldd.SmallRedGreen
			}
			return "this change alters behaviour, and its RED before GREEN is not on record (LDD, contracts/ldd.md, TDD always): " + hint + ", before the PR. Route: " + r.String() + "."
		case documented:
		case ldd.OneLineRecord(r.Tier):
			return "this change alters behaviour, and the branch carries no change with its record (LDD, contracts/ldd.md): write its tasks.md under openspec/changes/<change>/ before the PR: at risk " + string(r.Tier) + " an `## Intent` line and a `Delivers:` line are enough, with a spec delta only when a main spec changes or is added. Route: " + r.String() + "."
		default:
			return "this change alters behaviour, and the branch carries no change with its record (LDD, contracts/ldd.md): write its tasks.md and spec delta under openspec/changes/<change>/ before the PR, or state `No spec change: <why>` in tasks.md when its behaviour stays. Route: " + r.String() + "."
		}
		// On the standard route the light verify at slice close stands in
		// for the verified route's verify-report (T31.5).
		// A small change (no folder, its record needs no tasks.md) keeps no
		// light verify (T146g G3).
		small := folderless != "" && !n.Tasks
		if r.Level == ldd.RouteStandard && !small && !slices.ContainsFunc(changes, func(c string) bool { return execution.LightVerified(top, c) }) {
			return "this change alters behaviour, and no passing light verify of the tree its last run tested is on record (LDD, contracts/ldd.md, \"Light verify\"): launch the `verify` agent in light mode when the slice closes; it records `jhon executions verify " + named + " --verdict pass`, before the PR. Route: " + r.String() + "."
		}
	}
	if why, due := verifyDue(top, routeOf()); due {
		return "the route is verified (" + why + "): verify is due before delivery: launch the `verify` agent and record its verify-report.md (`verdict: pass`) under openspec/changes/<change>/ before the PR; a Judgment Day closed APPROVED counts too (contracts/ldd.md, \"Delivery\")."
	}
	change := "<change>"
	if active, _ := ldd.BranchChanges(top); len(active) == 1 {
		change = active[0]
	}
	r := routeOf()
	byRisk := "risk " + string(r.Tier)
	for _, v := range reviewsOf() {
		if v.Done {
			continue
		}
		why := strings.Join(v.By, "; ")
		if v.Review == ldd.ReviewFourR {
			why = strings.Replace(why, byRisk, fmt.Sprintf("%s reaches the verified tier (%s)", byRisk, r.VerifiedTier), 1)
		}
		switch v.Review {
		case ldd.ReviewUnreadable:
			return v.Line()
		case ldd.ReviewVerify:
			return "verify is due before delivery (" + why + "): launch the `verify` agent and record its verify-report.md (`verdict: pass`) under openspec/changes/" + change + "/ before the PR; a Judgment Day closed APPROVED counts too (contracts/ldd.md, \"Delivery\")."
		case ldd.ReviewFourR:
			return "4R is due before delivery (" + why + "): run the pertinent lenses (" + strings.Join(r.Lenses, ", ") + ") with the native review (`jhon_review` in pi, `jhon review run` in Claude Code; the lens agents when it cannot run), only those its diff calls for (contracts/review-4r.md, \"Lenses\"), and record openspec/changes/" + change + "/review-4r.md with the lenses it ran (`lenses: " + strings.Join(r.Lenses, ", ") + "`) and `verdict: pass` before the PR; a Judgment Day closed APPROVED counts too."
		case ldd.ReviewJD:
			return "a Judgment Day is due before delivery (" + why + "): openspec/config.yaml requires it; run it (contracts/judgment.md) and close it APPROVED under openspec/changes/" + change + "/judgments/ before the PR."
		}
	}
	return ""
}

// branchReviews is the reviews the branch's changes, active and archived,
// owe on route r (ldd.ReviewsOf).
func branchReviews(top string, r ldd.Route) []ldd.Review {
	active, archived := ldd.BranchChanges(top)
	changes := slices.Clone(active)
	for _, c := range archived {
		changes = append(changes, filepath.Join("archive", c))
	}
	return ldd.ReviewsOf(top, changes, r)
}

// owes is whether reviews include kind, due or done.
func owes(reviews []ldd.Review, kind string) bool {
	return slices.ContainsFunc(reviews, func(v ldd.Review) bool { return v.Review == kind })
}

// suiteNote is the note for command, a whole project's tests run by hand
// (guard.WholeSuiteByHand; a package or a directory is focused), while the
// branch carries an active change; "" for any other. It is never a refusal:
// it names the unit's run of `jhon executions`, which records the tests, and
// the wide one a slice's close takes.
func suiteNote(top, command string) string {
	if !guard.WholeSuiteByHand(command, top) {
		return ""
	}
	active, _ := ldd.BranchChanges(top)
	if len(active) == 0 {
		return ""
	}
	change := "<change>"
	if len(active) == 1 {
		change = active[0]
	}
	return "a whole test suite run by hand: `jhon executions run " + change + " --affected` runs the tests this unit's edits reach and records them (`--affected <base>` at the slice's close)"
}

// verifyDue is whether the branch's change takes the verified route, by its
// diff, and has no independent verification on record yet
// (ldd.BranchVerified); why is the route's reason. review_required naming
// verify asks for it on any route (ldd.ReviewsOf).
func verifyDue(top string, r ldd.Route) (why string, due bool) {
	if r.Level != ldd.RouteVerified {
		return "", false
	}
	return strings.Join(r.Reasons, "; "), !ldd.BranchVerified(top)
}
