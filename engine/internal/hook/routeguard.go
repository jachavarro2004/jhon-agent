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
	"github.com/jachavarro2004/jhon-agent/engine/internal/rename"
	"github.com/jachavarro2004/jhon-agent/engine/internal/workspace"
)

// RouteGuard holds every agent to LDD and the route its change takes
// (ldd/route.go) in a repository with LDD's layout: `route-guard --agent <owner>`.
// In a repository without one, a commit or a PR is refused until LDD is
// set up (ldd.SetUpRefusal, T72); a code edit, by a tool or by a Bash
// write, runs with a note to set it up (setUpEditNote; T89, a refusal until
// T153, L957: an edit is not destructive): every file in the repository
// outside .git and the layout's directory is code.
//
//   - The record is never refused: a spec delta, tasks.md, a proposal, and
//     a design are written on any route, when the change needs one or the
//     owner asks (`jhon status` suggests a proposal and a design when the
//     route calls for them).
//   - A PR of a change that alters behaviour opens only once the branch
//     carries its RED before GREEN in `jhon executions`, and its record when
//     the change needs one (ldd.Recorded, ldd.NeedsOf); on the standard
//     route one with no passing light verify of the tree its last run tested
//     (execution.LightVerified, T31.5) runs with a note (lightVerifyNote,
//     T154: none caught a defect on record), but for a small change (T146g):
//     tasks.md and a spec
//     delta only when needed; at risk passive or medium a tasks.md with an
//     `## Intent` and a `Delivers:` line is enough. A change with no folder,
//     the one its branch names, counts too.
//   - A bounded writer (the names route-guard is given) is launched only
//     with its `## Allowed edit surfaces`: without them scope-guard has no
//     scope to hold it to (pi's sub-agents refuse such a task too). Each
//     path listed matches something (guard.MissingSurfaces: a file or
//     folder there, a glob's match, or a new file's parent folder; T153,
//     L1364 b), so a wrong path is fixed before the launch.
//   - The jhon-checker agent may always be launched, on any route. Prism reviewers run
//     without asking when Prism review is due (risk at the verified tier, or
//     review_required naming 4r), which the owner approved; its correction
//     is a worker's. Otherwise a reviewer is asked for at the top level, where the owner answers, and refused
//     from a sub-agent; no grant stands in for that answer, but the
//     owner's yolo or super-yolo mode passes it with a note (L154). A review_required entry jhon cannot read refuses the PR
//     until it is fixed. On the verified route (risk at the verified tier;
//     size alone never puts a change on it) a PR opens only once the branch's
//     change has its independent verification (ldd.BranchVerified), and a
//     commit that closes a slice before then passes with a note that verify
//     is due; on a risk-high route it waits for its Prism review too, and on any route
//     for what review_required names (ldd.ReviewsOf).
//   - A commit that closes a slice adding more lines of code than the
//     advisory size (ldd.SliceLines) passes with a note that says so;
//     with verify due too, both are one note. One whose route now asks for
//     a proposal and a design the change lacks names the files to write
//     (ldd.DesignDueNotes), a note as `jhon status` says it (T101).
//   - With `delivery: change`, a commit that closes a slice commits the
//     tree the change's last test run tested (ldd.SliceCommitRefusal); a PR
//     opens once the branch's change has every slice done (ldd.SlicesLeft,
//     deliver check's words), and merges with --rebase.
//   - A command that moves the default branch to another commit is a merge,
//     and runs only after a ready `jhon deliver check <change> --action
//     merge` of that commit, on its own line before it (mergeGate, T219).
//   - A commit or a PR is judged at the repository it runs in: a cd, a
//     pushd, env -C or git -C to another worktree moves it there from the
//     session's root (deliveryRoots, T140); one the guard cannot place is
//     judged at the session's root.
//   - A joined member of a workspace (workspace.Of) needs no layout of its
//     own: its edits and commits pass as in a project under LDD, and its PR
//     is judged with the primary's record, the change its branch names
//     (memberRefusal, T57 slice 5). A listed sibling that has not joined is
//     a repository like any other. The primary's PR for a change with
//     members waits for each member's delivery and names each member's PR
//     (membersRefusal, T57 slice 7).
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
	for i, b := range bounded {
		bounded[i] = rename.AgentNow(b)
	}
	if kind := rename.AgentNow(strings.TrimSpace(p.Obj("tool_input").Str("subagent_type"))); p.Str("tool_name") == "Agent" && slices.Contains(bounded, kind) {
		surfaces, ok := guard.SurfacesOf(p.Obj("tool_input").Str("prompt"))
		if !ok {
			return block("a " + kind + " is launched with its scope: list the files it may edit under a `## Allowed edit surfaces` heading, one path or narrow glob per line; without it nothing holds the writer to its task (contracts/efficiency.md, \"Writer scope\").")
		}
		if why := missingSurfaces(kind, p.Cwd(), surfaces); why != "" {
			return block(why)
		}
	}
	cwd := p.Cwd()
	top := project.Root(cwd)
	// A command that moves the default branch is a merge: it runs after a
	// ready merge check of that commit, on its own line (T219).
	if command, _ := p.Obj("tool_input")["command"].(string); p.Str("tool_name") == "Bash" {
		// Git itself refuses what a line cannot show (S5): the merge gate is
		// installed on the first guarded command, and nothing turns it off.
		if top != "" && gatedRoot(top) {
			InstallMergeGate(top)
		}
		if why := gateBypass(command, cwd, top); why != "" {
			return block(why)
		}
		if why := mergeGate(command, cwd, top); why != "" {
			return block(why)
		}
	}
	if !project.UsesLdd(top) && memberPrimary(top) == "" {
		// Jhon always works under LDD (T72, L557): in a repository with no
		// layout nothing is refused but delivery, a commit or a PR, until LDD
		// is set up; a folder in no repository delivers nothing.
		if command, _ := p.Obj("tool_input")["command"].(string); top != "" && p.Str("tool_name") == "Bash" && deliveryOf(command).any() {
			return block(ldd.SetUpRefusal())
		}
		// A code edit runs with a note to set LDD up (T89, L646; T153): a
		// file the call writes in the repository, outside .git and every
		// layout's directory, is code.
		edits := top != "" && slices.ContainsFunc(writtenBy(p, cwd), func(f string) bool { return isCode(top, f) })
		// A write whose file the parser cannot name (inline interpreter
		// code, git apply, patch, a variable target) counts as one in the
		// repository when it runs there (T94, L652, L656).
		if command, _ := p.Obj("tool_input")["command"].(string); !edits && top != "" && p.Str("tool_name") == "Bash" {
			edits = slices.ContainsFunc(guard.UnresolvedWrites(command, cwd), func(u guard.UnresolvedWrite) bool { return unresolvedCode(top, u) })
		}
		if edits {
			fmt.Fprint(io.Out, noteJSON(setUpEditNote()))
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
		kind := rename.AgentNow(strings.TrimSpace(p.Obj("tool_input").Str("subagent_type")))
		switch {
		case strings.HasPrefix(kind, "prism-") && owes(reviewsOf(), ldd.ReviewPrism):
			// A Prism review runs only the facets its diff calls for (L46); another
			// facet is the owner's call.
			facet := strings.TrimPrefix(kind, "prism-")
			if pertinent := routeOf().Facets; slices.Contains(ldd.Facets, facet) && !slices.Contains(pertinent, facet) {
				return ask("review", "the "+facet+" facet is not pertinent to this change: its diff calls for ("+strings.Join(pertinent, ", ")+") (contracts/review-prism.md, \"Facets\")")
			}
			return 0
		case strings.HasPrefix(kind, "prism-"):
			return ask("review", "a "+kind+" reviewer runs only when the owner asks for a Prism review")
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
			return slices.ContainsFunc(roots, func(r string) bool { return !project.UsesLdd(r) && memberPrimary(r) == "" })
		}
		if notSetUp(prRoots) {
			return block(ldd.SetUpRefusal())
		}
		var notes []string
		for _, root := range prRoots {
			r, v := judgedAt(root)
			why, note := "", ""
			if primary := memberPrimary(root); primary != "" {
				why, note = memberRefusal(root, primary, command, r)
			} else if why, note = deliveryRefusal(root, command, r, v); why == "" {
				why = membersRefusal(root, cwd, command)
			}
			if why != "" {
				return block(layout.Words(root, why))
			}
			if note != "" {
				notes = append(notes, layout.Words(root, note))
			}
		}
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
					due = append(due, "route verified ("+why+"): verify is due before delivery; launch the jhon-checker agent and record verify-report.md before the PR")
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

// setUpEditNote is the note on a code edit in a repository with no layout:
// set LDD up by the init steps ldd.SetUpEditRefusal names, and start the
// edit's change before finishing. The edit has run; it is not refused.
func setUpEditNote() string {
	steps := ""
	if _, rest, ok := strings.Cut(ldd.SetUpEditRefusal(), "`"); ok {
		steps, _, _ = strings.Cut(rest, "`")
	}
	return modeNotePrefix + "LDD is not set up in this project yet: set it up by following `" + steps +
		"` (ask how to deliver and test with your ask tool, or use the recommended values), and before you finish, start this edit's change (tasks.md) and record a run (`jhon executions run`)"
}

// missingSurfaces is why a bounded writer of kind, launched from cwd, may not
// start: its surfaces name a path that matches nothing in the project (each
// of its roots, workspace.ProjectRoots), or in cwd when it is no project;
// "" when every path matches (guard.MissingSurfaces, T153, L1364 b).
func missingSurfaces(kind, cwd string, surfaces []string) string {
	roots := workspace.ProjectRoots(cwd)
	if len(roots) == 0 {
		roots = []string{project.Realish(cwd)}
	}
	missing := guard.MissingSurfaces(surfaces, roots)
	if len(missing) == 0 {
		return ""
	}
	var named []string
	for _, s := range missing {
		named = append(named, s+" (its folder "+guard.SurfaceParent(s)+" does not exist)")
	}
	verb := "matches"
	if len(missing) > 1 {
		verb = "match"
	}
	return "a " + kind + " is launched with surfaces that exist: its `## Allowed edit surfaces` names " + strings.Join(named, ", ") + ", which " + verb + " nothing in " + roots[0] +
		". A path matches when a file or folder exists there, a glob when it matches one, and a new path when its top-level folder exists. Fix the path in the brief before the launch: scope-guard holds the writer to the paths listed and refuses the real file a wrong one stands for (contracts/efficiency.md, \"Writer scope\")."
}

// memberPrimary is the primary whose record judges root, a joined member of
// its workspace (workspace.Of) when the primary uses LDD; "" for any other
// root.
func memberPrimary(root string) string {
	if root == "" {
		return ""
	}
	if ws := workspace.Of(root); ws.Role == workspace.Member && project.UsesLdd(ws.Primary) {
		return ws.Primary
	}
	return ""
}

// memberRefusal is why a PR command in member, a joined member of primary's
// workspace, is refused, "" when it may run (T57 slice 5, L1314), and the
// note it runs with. Its change is the primary's change its branch names
// (the branch's name, a slash a dash, as for a change with no folder), and
// the primary's record judges it: a change that alters behaviour in the
// member, by the member's route, opens its PR with that change's RED before
// GREEN, and on the verified route its independent verification; on the
// standard route one with no passing light verify runs with a note;
// `delivery: change` in the primary merges with rebase.
func memberRefusal(member, primary, command string, routeOf func() ldd.Route) (why, note string) {
	if prMerge.MatchString(command) && ldd.DeliveryUnit(primary) == "change" && !rebase.MatchString(command) {
		return "delivery: change merges with rebase, so each slice stays its own commit: run `gh pr merge <n> --rebase --delete-branch`.", ""
	}
	r := routeOf()
	if !deliveryOf(command).pr || !r.Behaviour {
		return "", ""
	}
	branch, _ := project.ReadGit(member, "branch", "--show-current")
	change := strings.ReplaceAll(branch, "/", "-")
	lead := "this change alters behaviour in " + member + ", a member of the workspace of " + primary + ", and "
	route := " Route: " + r.String() + "."
	if change == "" {
		return lead + "the member is on no branch, so it names no change of the primary: check out the branch named after the primary's change before the PR." + route, ""
	}
	lead += "the primary's change " + change + ", which the member's branch names, "
	switch {
	case !execution.RedBeforeGreen(primary, change):
		return lead + "has no RED before GREEN on record (LDD, contracts/ldd.md, TDD always): " + ldd.RedGreenHint(change) + " in " + primary + ", before the PR." + route, ""
	case r.Level == ldd.RouteStandard && !execution.LightVerified(primary, change):
		return "", lightVerifyNote(change)
	case r.Level == ldd.RouteVerified:
		if ldd.IndependentVerification(primary, change) == nil {
			return lead + "has no independent verification on record: the route is verified (" + strings.Join(r.Reasons, "; ") + "): launch the `jhon-checker` agent and record its verify-report.md (`verdict: pass`) for " + change + " in " + primary + " before the PR (contracts/ldd.md, \"Delivery\")." + route, ""
		}
	}
	return "", ""
}

// lightVerifyNote is the note a standard-route PR of change runs with when
// no passing light verify of the tree its last run tested is on record: an
// advisory since T154 (L1394), as `jhon audit` found no light verify that
// caught a defect the recorded runs missed.
func lightVerifyNote(change string) string {
	return modeNotePrefix + "the PR runs with no passing light verify of the tree its last run tested on record for " + change +
		" (contracts/ldd.md, \"Light verify\"): an advisory, as no light verify has caught a defect on record; the `jhon-checker` agent in light mode records one with `jhon executions verify " + change + " --verdict pass|fail`"
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
// anything inside it but .git, the layouts' directories (openspec/, ldd/),
// which setting LDD up writes, and jhon-studio's working output
// (layout.StudioOutput, T8g).
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
	if first == ".git" || layout.StudioOutput(rel) {
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

// deliveryRefusal is why a PR command is refused, "" when it may run, and
// the note it runs with.
func deliveryRefusal(top, command string, routeOf func() ldd.Route, reviewsOf func() []ldd.Review) (refusal, note string) {
	byChange := ldd.DeliveryUnit(top) == "change"
	if prMerge.MatchString(command) && byChange && !rebase.MatchString(command) {
		return "delivery: change merges with rebase, so each slice stays its own commit: run `gh pr merge <n> --rebase --delete-branch`.", ""
	}
	if !deliveryOf(command).pr {
		return "", ""
	}
	active, archived := ldd.BranchChanges(top)
	lay := layout.For(top)
	dir := func(c string) string { return layout.At(top, lay.ChangeDir(c)) }
	if byChange {
		// A slice not done, unchecked or outlined (T127), waits as
		// `jhon deliver check --action merge` waits, in its words (T154).
		for _, c := range active {
			if left := ldd.SlicesLeft(top, c); len(left) > 0 {
				return c + ": " + strings.Join(left, "; ") + ".", ""
			}
		}
	}
	if undocumented := ldd.UndocumentedProjects(top); len(undocumented) > 0 {
		return fmt.Sprintf("docs_with_code: %s change(s) code but not its docs/. Update the documentation the change makes stale before the PR, or state `No docs change: <why>` in the change's tasks.md.", strings.Join(undocumented, ", ")), ""
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
			return "this change alters behaviour, and its RED before GREEN is not on record (LDD, contracts/ldd.md, TDD always): " + hint + ", before the PR. Route: " + r.String() + ".", ""
		case documented:
		case ldd.OneLineRecord(r.Tier):
			return "this change alters behaviour, and the branch carries no change with its record (LDD, contracts/ldd.md): write its tasks.md under openspec/changes/<change>/ before the PR: at risk " + string(r.Tier) + " an `## Intent` line and a `Delivers:` line are enough, with a spec delta only when a main spec changes or is added. Route: " + r.String() + ".", ""
		default:
			return "this change alters behaviour, and the branch carries no change with its record (LDD, contracts/ldd.md): write its tasks.md and spec delta under openspec/changes/<change>/ before the PR, or state `No spec change: <why>` in tasks.md when its behaviour stays. Route: " + r.String() + ".", ""
		}
		// On the standard route the light verify at slice close stands in
		// for the verified route's verify-report (T31.5); with none the PR
		// runs with a note (T154).
		// A small change (no folder, its record needs no tasks.md) keeps no
		// light verify (T146g G3); a branch that also carries a change with a
		// folder, under any name, is not one (L1289).
		small := folderless != "" && len(active) == 0 && len(archived) == 0 && !n.Tasks
		if r.Level == ldd.RouteStandard && !small && !slices.ContainsFunc(changes, func(c string) bool { return execution.LightVerified(top, c) }) {
			note = lightVerifyNote(named)
		}
	}
	if why, due := verifyDue(top, routeOf()); due {
		return "the route is verified (" + why + "): verify is due before delivery: launch the `jhon-checker` agent and record its verify-report.md (`verdict: pass`) under openspec/changes/<change>/ before the PR (contracts/ldd.md, \"Delivery\").", ""
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
		if v.Review == ldd.ReviewPrism {
			why = strings.Replace(why, byRisk, fmt.Sprintf("%s reaches the verified tier (%s)", byRisk, r.VerifiedTier), 1)
		}
		switch v.Review {
		case ldd.ReviewUnreadable:
			return v.Line(), ""
		case ldd.ReviewChecker:
			return "checker is due before delivery (" + why + "): launch the `jhon-checker` agent and record its verify-report.md (`verdict: pass`) under openspec/changes/" + change + "/ before the PR (contracts/ldd.md, \"Delivery\").", ""
		case ldd.ReviewPrism:
			return "Prism review is due before delivery (" + why + "): run the pertinent facets (" + strings.Join(r.Facets, ", ") + ") with the native review (`jhon_review` in pi, `jhon review run` in Claude Code; the facet agents when it cannot run), only those its diff calls for (contracts/review-prism.md, \"Facets\"), and record openspec/changes/" + change + "/review-prism.md with the facets it ran (`facets: " + strings.Join(r.Facets, ", ") + "`) and `verdict: pass` before the PR.", ""
		}
	}
	return "", note
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
