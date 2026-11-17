package audit

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/isodate"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	lddlayout "github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	grantstore "github.com/jachavarro2004/jhon-agent/engine/internal/grant"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
)

// delivered-untested (contracts/ldd.md, "Delivery"), and the test
// runs, ready checks, and risk reads the outcomes also count.

// defaultBranches are read as the default branch when a command switches to one before merging.
var defaultBranches = []string{"main", "master"}

var remotePrefix = lazyre.New(`^(?:origin|upstream)/`)

// MergesToDefault is whether a command line merges into the default branch:
// `gh pr merge`, or a `git merge` of another branch after `git checkout` or
// `git switch` to `main` or `master` earlier in the same line. A merge of
// the default branch into the current one syncs a branch; `git merge-base`
// merges nothing.
func MergesToDefault(command string) bool {
	onDefault := false
	for _, x := range codeless(plain(command)) {
		if x.Program == "gh" && len(x.Args) > 1 && x.Args[0] == "pr" && x.Args[1] == "merge" {
			return true
		}
		if x.Program != "git" {
			continue
		}
		var operands []string
		for _, a := range x.Args[indexOf(x.Args, x.Sub)+1:] {
			if !strings.HasPrefix(a, "-") {
				operands = append(operands, a)
			}
		}
		switch {
		case x.Sub == "checkout" || x.Sub == "switch":
			onDefault = slices.ContainsFunc(operands, func(a string) bool { return slices.Contains(defaultBranches, a) })
		case x.Sub == "merge" && onDefault && slices.ContainsFunc(operands, func(a string) bool {
			return !slices.Contains(defaultBranches, remotePrefix.ReplaceAllString(a, ""))
		}):
			return true
		}
	}
	return false
}

// pushesDefault is whether a git push updates the default branch: a refspec
// whose destination is it, or no refspec while on it.
func pushesDefault(x Invocation, onDefault bool) bool {
	var operands []string
	for _, a := range x.Args[indexOf(x.Args, x.Sub)+1:] {
		if !strings.HasPrefix(a, "-") {
			operands = append(operands, a)
		}
	}
	if len(operands) < 2 {
		return onDefault
	}
	for _, spec := range operands[1:] {
		dest := strings.TrimPrefix(spec, "+")
		if _, after, ok := strings.Cut(dest, ":"); ok {
			dest = after
		}
		if slices.Contains(defaultBranches, strings.TrimPrefix(dest, "refs/heads/")) {
			return true
		}
	}
	return false
}

var (
	deployName  = lazyre.New(ci(`deploy`))
	deployFlows = []string{"serverless", "sls", "fly", "flyctl"}
)

// infraDeploy is whether x is an infrastructure deploy, whose artifact is the commit it deploys from.
func infraDeploy(x Invocation) bool {
	ws := append([]string{x.Program}, x.Args...)
	cdk := slices.Contains(ws, "cdk") || slices.Contains(ws, "aws-cdk")
	return (cdk && slices.Contains(ws, "deploy")) || ((x.Program == "terraform" || x.Program == "tofu") && x.Sub == "apply")
}

// InfraDeploy is whether a command line runs an infrastructure deploy: `cdk
// deploy`, `terraform apply`, `tofu apply`.
func InfraDeploy(line string) bool {
	return slices.ContainsFunc(codeless(plain(line)), infraDeploy)
}

// DeliveryCommand is the delivery a command line performs besides a merge:
// `push` for a `git push` (not `--dry-run`) into the default branch, named
// by its refspec or, with none, after a `git checkout` or `git switch` to it
// earlier in the line, and for `jhon publish` (a change branch's push is
// review, not delivery: the owner's decision, docket AY B6); `deploy` for
// `cdk deploy`, `terraform apply`, `tofu apply`, `aws ecs update-service`, `aws lambda
// update-function-code`, a program whose name holds `deploy`,
// `serverless`/`sls`/`fly`/`flyctl deploy`, `kubectl apply|rollout|set`,
// `helm upgrade|install`, `gh workflow run` of a deploy workflow, and
// `docker push`; `release` for `gh release create` and a package publish.
// "" otherwise.
func DeliveryCommand(command string) string {
	onDefault := false
	for _, x := range codeless(plain(command)) {
		if x.Program == "git" && (x.Sub == "checkout" || x.Sub == "switch") {
			onDefault = slices.ContainsFunc(x.Args, func(a string) bool { return slices.Contains(defaultBranches, a) })
		}
		ws := append([]string{x.Program}, x.Args...)
		hasAll := func(w ...string) bool {
			for _, v := range w {
				if !slices.Contains(ws, v) {
					return false
				}
			}
			return true
		}
		arg := func(i int) string { a, _ := at(x.Args, i); return a }
		switch {
		case x.Program == "git" && x.Sub == "push" && !slices.ContainsFunc(x.Args, func(a string) bool { return a == "--dry-run" || a == "-n" }):
			if pushesDefault(x, onDefault) {
				return "push"
			}
		case x.Program == "jhon" && x.Sub == "publish":
			return "push"
		case infraDeploy(x):
			return "deploy"
		case x.Program == "aws" && (hasAll("ecs", "update-service") || hasAll("lambda", "update-function-code")):
			return "deploy"
		// A word with a glob in it is a pattern (a `case` branch the splitter
		// reads as a pipeline), never a program.
		case deployName.MatchString(baseName(x.Program)) && !strings.ContainsAny(x.Program, "*?["):
			return "deploy"
		case slices.Contains(deployFlows, x.Program) && arg(0) == "deploy":
			return "deploy"
		case x.Program == "kubectl" && slices.Contains([]string{"apply", "rollout", "set"}, x.Sub):
			return "deploy"
		case x.Program == "helm" && (x.Sub == "upgrade" || x.Sub == "install"):
			return "deploy"
		case x.Program == "docker" && x.Sub == "push":
			return "deploy"
		case x.Program == "gh" && arg(0) == "workflow" && arg(1) == "run" && len(x.Args) > 2 && slices.ContainsFunc(x.Args[2:], deployName.MatchString):
			return "deploy"
		case (x.Program == "gh" && arg(0) == "release" && arg(1) == "create") || (slices.Contains([]string{"npm", "pnpm", "yarn", "cargo"}, x.Program) && x.Sub == "publish"):
			return "release"
		}
	}
	return ""
}

// testRun is a test run of a change, read from a `jhon executions run`
// result, the change's execution record, or a direct test command.
type testRun struct {
	change, at string
	// passed is nil when the output is not in the transcript: redirected to a file, or unreadable.
	passed       *bool
	commit, tree string
	statuses     []string
	// durationMs is from the call to its result; nil for a run started in the background.
	durationMs *float64
	source     string
	// why a run that did not pass did not: its tests, or its tree (RunVerdict).
	why string
	// direct is, for a direct test command (directTest) with no recorded tree, the transcript it ran in.
	direct string
	// next is, for a record that failed only for starting uncommitted, the
	// commit that then committed the tree it tested (committedAs), and
	// nextTree that tree; "" when none did.
	next, nextTree string
}

// asNext is r as the passing run of the commit that committed the tree it
// tested, when one did; else r.
func (r testRun) asNext() testRun {
	if r.next == "" {
		return r
	}
	passed := true
	r.passed, r.why, r.commit, r.tree = &passed, "", r.next, r.nextTree
	return r
}

// readyCheck is a ready delivery check of a change, read from a `jhon
// deliver check` result or the change's delivery.json: HEAD's commit and
// tree when it ran, and the tree it found tested ("" when not shown).
// risked: the check read the change's risk tier.
type readyCheck struct {
	change, at, action string
	artifact           *string
	source             string
	commit, tree       string
	testedTree         string
	risked             bool
}

// riskRead is a `jhon risk` result, attributed to the change its transcript works on.
type riskRead struct{ change, at, tier, source string }

var jhonFlagValue = lazyre.New(`^--(?:affected|timeout|root|action|artifact)$`)

// jhonOperand is the <change> operand of a `jhon <sub> <verb>` invocation
// in command, or ""; a `--dry-run` runs nothing and is none.
func jhonOperand(command, sub, verb string) string {
	for _, x := range codeless(plain(command)) {
		if x.Program != "jhon" || x.Sub != sub {
			continue
		}
		rest := x.Args[indexOf(x.Args, sub)+1:]
		if len(rest) == 0 || rest[0] != verb || slices.Contains(rest, "--dry-run") {
			continue
		}
		all := rest[1:]
		k := slices.IndexFunc(all, func(a string) bool { return !strings.HasPrefix(a, "-") })
		for k > 0 && jhonFlagValue.MatchString(all[k-1]) {
			k = indexAfter(all, k+1, func(a string) bool { return !strings.HasPrefix(a, "-") })
		}
		if k >= 0 && all[k] != "" {
			return all[k]
		}
	}
	return ""
}

// jhonRisk is whether command runs `jhon risk`.
func jhonRisk(command string) bool {
	return slices.ContainsFunc(codeless(plain(command)), func(x Invocation) bool {
		return x.Program == "jhon" && x.Sub == "risk"
	})
}

var riskLine = lazyre.New(`(?m)` + js.LineStart + `risk:` + space + `*(passive|medium|high)\b`)

// riskTier is the tier a `jhon risk` output reports, as JSON or as its `risk: <tier>` line; "" when none.
func riskTier(text string) string {
	if t, ok := strProp(jsonObject(text), "tier"); ok {
		return t
	}
	if m := riskLine.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return ""
}

// RunVerdict is whether a recorded run passed, and why not: a failed or
// timed-out project, an incomplete run, a start on a tree that was not
// clean, or a tree changed during the run. Its tests may all have passed in
// the last three. why is "" for a pass.
func RunVerdict(statuses []string, clean, dirty, complete bool) (passed bool, why string) {
	var bad []string
	for _, x := range statuses {
		if x != "pass" {
			bad = append(bad, x)
		}
	}
	switch {
	case len(statuses) == 0:
		why = "it ran no project"
	case len(bad) > 0:
		why = fmt.Sprintf("%d of %d project(s) %s", len(bad), len(statuses), strings.Join(uniq(bad), " or "))
	case !complete:
		why = "it did not complete"
	case !clean:
		why = "its tests passed, but it started on a working tree with uncommitted changes, so the tested tree is not a commit's"
	case dirty:
		why = "its tests passed, but the tree changed during the run"
	}
	return why == "", why
}

// executionStatuses is each execution's status: its `status`, else pass for exit code 0.
func executionStatuses(executions []any) []string {
	out := make([]string, len(executions))
	for i, e := range executions {
		st := prop(e, "status")
		if js.Nullish(st) {
			if code, ok := prop(e, "exit_code").(float64); ok && code == 0 {
				st = "pass"
			} else {
				st = "fail"
			}
		}
		out[i] = js.String(st)
	}
	return out
}

// Executions is what a `jhon executions run` output reports.
type Executions struct {
	Passed       bool
	Why          string
	Commit, Tree string
	Statuses     []string
}

var (
	candidateLine = lazyre.New(`(?m)` + js.LineStart + `candidate` + space + `+(` + nonSpace + `+)` + space + `+tree` + space + `+(` + nonSpace + `+)` + space + `+clean` + space + `+(` + nonSpace + `+)` + space + `*$`)
	statusLine    = lazyre.New(`(?m)` + js.LineStart + `(pass|fail|timeout)` + space + `{2,}` + nonSpace)
	dirtyLine     = lazyre.New(`(?m)` + js.LineStart + `dirty` + space)
)

// ReadExecutions is what a `jhon executions run` output reports: its JSON
// summary, or the human form's `candidate <commit> tree <tree> clean
// <bool>` line, its `dirty` line, and one `<status> <project>` line per
// execution; nil when neither is there. Passing is at least one execution,
// every one `pass`, a clean start, and not dirty.
func ReadExecutions(text string) *Executions {
	if j := jsonObject(text); j != nil {
		if list, ok := prop(j, "executions").([]any); ok {
			statuses := executionStatuses(list)
			passed, why := RunVerdict(statuses, isTrue(j, "working_tree_clean"), isTrue(j, "dirty"), true)
			return &Executions{passed, why, orString(prop(j, "candidate_commit"), ""), orString(prop(j, "candidate_tree"), ""), statuses}
		}
	}
	cand := candidateLine.FindStringSubmatch(text)
	if cand == nil {
		return nil
	}
	statuses := []string{}
	for _, m := range statusLine.FindAllStringSubmatch(text, -1) {
		statuses = append(statuses, m[1])
	}
	passed, why := RunVerdict(statuses, cand[3] == "true", dirtyLine.MatchString(text), true)
	return &Executions{passed, why, cand[1], cand[2], statuses}
}

// DeliverCheck is what a ready `jhon deliver check` output reports.
type DeliverCheck struct {
	Action                   string
	Artifact                 *string
	Commit, Tree, TestedTree string
	Risked                   bool
}

var (
	readyLine  = lazyre.New(`(?m)` + js.LineStart + `(merge|deploy|release)` + space + `+ready(?:` + space + `+artifact` + space + `+(` + nonSpace + `+))?` + space + `*$`)
	headLine   = lazyre.New(`(?m)` + js.LineStart + `head` + space + `+(` + nonSpace + `+)` + space + `+tree` + space + `+(` + nonSpace + `+)` + space + `*$`)
	testedLine = lazyre.New(`(?m)` + js.LineStart + `tested` + space + `+` + nonSpace + `+` + space + `+tree` + space + `+(` + nonSpace + `+)` + space + `*$`)
	riskedLine = lazyre.New(`(?m)` + js.LineStart + `risk` + space + `+(?:passive|medium|high)\b`)
)

// ReadDeliverCheck is what a `jhon deliver check` output reports when it
// is ready: its action and artifact; nil when not ready or unreadable.
func ReadDeliverCheck(text string) *DeliverCheck {
	if j := jsonObject(text); j != nil {
		if ready, ok := prop(j, "ready").(bool); ok {
			if !ready {
				return nil
			}
			str := func(k string) string { s, _ := strProp(j, k); return s }
			_, risked := prop(prop(j, "risk"), "tier").(string)
			return &DeliverCheck{orString(prop(j, "action"), "merge"), optString(prop(j, "artifact")), str("head_commit"), str("head_tree"), str("tested_tree"), risked}
		}
	}
	m := readyLine.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	c := &DeliverCheck{Action: m[1], Risked: riskedLine.MatchString(text)}
	if m[2] != "" {
		c.Artifact = &m[2]
	}
	if h := headLine.FindStringSubmatch(text); h != nil {
		c.Commit, c.Tree = h[1], h[2]
	}
	if t := testedLine.FindStringSubmatch(text); t != nil {
		c.TestedTree = t[1]
	}
	return c
}

var (
	pythonProgram = lazyre.New(`^python[\d.]*$`)
	pytestRunner  = lazyre.New(`^(?:pytest|py\.test)$`)
)

// DirectTest is whether a command line runs a project's tests directly,
// with a common runner: `npm|pnpm|yarn|bun test` (or `run test`), `pytest`,
// `python -m pytest`, `uv|poetry|pipenv|hatch run pytest`, `go test`,
// `cargo test`, `deno test`, or `node --test`. The project's own discovered
// command is not read here: discovery runs git, and the audit runs nothing.
func DirectTest(command string) bool {
	for _, x := range codeless(plain(command)) {
		a := x.Args
		after := func(w string) string { s, _ := at(a, indexOf(a, w)+1); return s }
		arg := func(i int) string { s, _ := at(a, i); return s }
		switch {
		case slices.Contains([]string{"npm", "pnpm", "yarn", "bun"}, x.Program) && (x.Sub == "test" || (x.Sub == "run" && after("run") == "test")):
			return true
		case x.Program == "pytest" || x.Program == "py.test":
			return true
		case pythonProgram.MatchString(x.Program) && arg(0) == "-m" && arg(1) == "pytest":
			return true
		case slices.Contains([]string{"uv", "poetry", "pipenv", "hatch"}, x.Program) && x.Sub == "run" && pytestRunner.MatchString(after("run")):
			return true
		case slices.Contains([]string{"go", "cargo", "deno"}, x.Program) && x.Sub == "test":
			return true
		case x.Program == "node" && slices.Contains(a, "--test"):
			return true
		}
	}
	return false
}

// statusOperand is the <change> operand of a `jhon status <change>` invocation in command, or "".
func statusOperand(command string) string {
	for _, x := range codeless(plain(command)) {
		if x.Program != "jhon" || x.Sub != "status" {
			continue
		}
		rest := x.Args[indexOf(x.Args, "status")+1:]
		k := slices.IndexFunc(rest, func(a string) bool { return !strings.HasPrefix(a, "-") })
		for k > 0 && rest[k-1] == "--root" {
			k = indexAfter(rest, k+1, func(a string) bool { return !strings.HasPrefix(a, "-") })
		}
		if k >= 0 && rest[k] != "" {
			return rest[k]
		}
	}
	return ""
}

// namedBy is the change one tool call names: a `jhon status|deliver
// check|executions run <change>` command, or the one
// `openspec/changes/<change>` path its input touches (the archive aside); a
// delegation is attributed by its own launch (changeOf), never here. ""
// when it names none.
func namedBy(u ToolUse) string {
	if u.Name == "Agent" || u.Name == "Task" {
		return ""
	}
	if c, ok := strProp(u.Input, "command"); ok && u.Name == "Bash" {
		for _, named := range []string{jhonOperand(c, "executions", "run"), jhonOperand(c, "deliver", "check"), statusOperand(c)} {
			if named != "" {
				return named
			}
		}
	}
	if paths := changesNamed(js.StringifyValueCompact(withoutBodies(u.Input))); len(paths) == 1 {
		return paths[0]
	}
	return ""
}

// bodyKeys are the tool inputs that carry a file's text, not a path the call
// works on: what it writes or replaces. A path in a test fixture there
// names no change the session works on.
var bodyKeys = []string{"content", "old_string", "new_string", "edits", "oldText", "newText"}

// withoutBodies is a tool's input without the text it writes: the body keys
// dropped, and a command's heredocs emptied, so only paths and commands name
// a change.
func withoutBodies(input any) any {
	o, ok := input.(*js.Object)
	if !ok {
		return input
	}
	c := o.Clone()
	for _, k := range bodyKeys {
		c.Delete(k)
	}
	if cmd, ok := strProp(c, "command"); ok {
		c.Set("command", WithoutHeredocs(cmd))
	}
	return c
}

// exists is existsSync.
func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// changeDirs is the directories a change's records may be in under root: the active change, and each archived copy of it.
func changeDirs(root, change string) []string {
	trackChange(root, change)
	l := lddlayout.For(root)
	out := []string{lddlayout.At(root, l.ChangeDir(change))}
	archive := lddlayout.At(root, l.ArchiveDir())
	if entries, err := os.ReadDir(archive); err == nil {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		for _, d := range names {
			if d == change || strings.HasSuffix(d, "-"+change) {
				out = append(out, filepath.Join(archive, d))
			}
		}
	}
	return slices.DeleteFunc(out, func(d string) bool { return !exists(d) })
}

// readJSON is a file read as one JSON object, nil when absent or unreadable.
func readJSON(file string) *js.Object {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	return jsonObject(js.DecodeUTF8(b))
}

// vocab is contracts/vocabulary.yaml, read once, when a check first needs it.
var vocab = sync.OnceValue(func() string { return contract.MustContract("vocabulary.yaml") })

// diskEvidence is a change's execution record and delivery.json under each of roots, read from disk.
func diskEvidence(change string, roots []string) (runs []testRun, checks []readyCheck) {
	_, m := contract.Map(vocab(), "execution_record")
	recordFile := m["file"]
	if recordFile == "" {
		recordFile = "execution-record.json"
	}
	for _, root := range roots {
		for _, dir := range changeDirs(root, change) {
			r := readJSON(filepath.Join(dir, recordFile))
			list, ok := prop(r, "executions").([]any)
			if ts, isStr := strProp(r, "timestamp"); r != nil && ok && isStr {
				statuses := executionStatuses(list)
				total := 0.0
				for _, e := range list {
					total += count(prop(e, "duration_ms"))
				}
				complete := prop(r, "complete") != any(false)
				passed, why := RunVerdict(statuses, isTrue(r, "working_tree_clean"), isTrue(r, "dirty"), complete)
				commit, tree := orString(prop(r, "candidate_commit"), ""), orString(prop(r, "candidate_tree"), "")
				run := testRun{change: change, at: ts, commit: commit, tree: tree,
					statuses: statuses, passed: &passed, why: why, durationMs: &total, source: filepath.Join(dir, recordFile)}
				// A run that failed only for starting uncommitted passes as the run of
				// the commit that then committed the tree it tested (committedAs),
				// for a delivery of that commit only (asNext).
				if onClean, _ := RunVerdict(statuses, true, isTrue(r, "dirty"), complete); !passed && onClean {
					if wt := orString(prop(r, "working_tree"), ""); wt != "" {
						if next := committedAs(dir, commit, wt); next != "" {
							run.next, run.nextTree = next, wt
						}
					}
				}
				runs = append(runs, run)
			}
			d := readJSON(filepath.Join(dir, "delivery.json"))
			ts, ok1 := strProp(d, "timestamp")
			action, ok2 := strProp(d, "action")
			if ok1 && ok2 {
				str := func(k string) string { s, _ := strProp(d, k); return s }
				_, risked := prop(d, "risk").(string)
				checks = append(checks, readyCheck{change: change, at: ts, action: action, artifact: optString(prop(d, "artifact")), source: filepath.Join(dir, "delivery.json"),
					commit: str("head_commit"), tree: str("tested_tree"), testedTree: str("tested_tree"), risked: risked})
			}
		}
	}
	return runs, checks
}

// evidence is every `jhon executions run` and ready `jhon deliver check`
// result in the session, with the change it names; every direct test
// command (DirectTest), attributed to the agent's change or the change its
// transcript last named; every `jhon risk` tier; and the time of every
// successful write by a write tool, per transcript.
type evidence struct {
	runs   []testRun
	checks []readyCheck
	risks  []riskRead
	writes map[string][]string
	// ci is every all-green CI report of a PR; pushes, the time of every
	// successful git push, which may change what a PR's CI tested.
	ci     []ciGreen
	pushes []string
	// pushedTo is every successful git push of a change that shows the commit it pushed.
	pushedTo []pushOfChange
	// refs is the session's PR references (readPRRefs).
	refs prRefs
}

// pushOfChange is a git push of change, at at, of commit sha.
type pushOfChange struct{ change, at, sha string }

// A finished is how a background command ended, from the runtime's task
// notification: its exit code (nil when the summary gives none), its output
// file, and the notification's time and line.
type finished struct {
	exit   *int
	output string
	at     string
	line   int
}

var (
	noteToolUse = lazyre.New(`<tool-use-id>([^<]+)</tool-use-id>`)
	noteOutput  = lazyre.New(`<output-file>([^<]+)</output-file>`)
	// "completed (exit code 0)", "failed with exit code 1".
	noteExit = lazyre.New(`\bexit code (-?\d+)\b`)
)

// backgroundEnds are the background commands a transcript's task
// notifications report ended, by the tool call that started them.
func backgroundEnds(t *Transcript) map[string]finished {
	out := map[string]finished{}
	for _, e := range t.Entries {
		note := e.note()
		if e.Role != "user" || !strings.Contains(note, "<task-notification>") {
			continue
		}
		id := noteToolUse.FindStringSubmatch(note)
		if id == nil {
			continue
		}
		f := finished{at: e.Timestamp, line: e.Line}
		if m := noteExit.FindStringSubmatch(note); m != nil {
			n, _ := strconv.Atoi(m[1])
			f.exit = &n
		}
		if m := noteOutput.FindStringSubmatch(note); m != nil {
			f.output = m[1]
		}
		out[id[1]] = f
	}
	return out
}

// backgroundOutput is a background command's output file: where the runtime
// wrote it, else the same name under the session's tasks/ beside its
// transcript, where an archived session or a fixture keeps it; "" when
// neither can be read.
func (s *Session) backgroundOutput(path string) string {
	if path == "" {
		return ""
	}
	for _, p := range []string{path, filepath.Join(s.Dir, s.SessionID, "tasks", filepath.Base(path))} {
		readTree(p)
		if b, err := os.ReadFile(p); err == nil && (p == path || s.Dir != "") {
			return string(b)
		}
	}
	return ""
}

func transcriptEvidence(s *Session) evidence {
	ev := evidence{writes: map[string][]string{}, refs: readPRRefs(s)}
	for i, t := range s.transcripts() {
		own := s.own(i)
		ends := backgroundEnds(t)
		type use struct {
			u      ToolUse
			e      *Entry
			change string
		}
		uses := map[string]use{}
		named := ""
		for _, e := range t.Entries {
			// A background job's notice that reports its PR's CI passing (pi).
			if e.job != nil {
				if pr := ev.refs.NoticeCIGreen(*e.job); pr != "" {
					ev.ci = append(ev.ci, ciGreen{pr, e.Timestamp})
				}
			}
			for _, u := range e.ToolUses {
				if n := u.named; n != "" {
					named = n
				}
				change := own
				if change == "" {
					change = named
				}
				uses[u.ID] = use{u, e, change}
			}
		}
		var wrote []string
		for _, e := range t.Entries {
			for _, r := range e.ToolResults {
				u, ok := uses[r.ID]
				if _, w := writeToolKey[u.u.Name]; ok && w && !r.IsError {
					wrote = append(wrote, e.Timestamp)
				}
				command, isCmd := strProp(u.u.Input, "command")
				if !ok || u.u.Name != "Bash" || !isCmd {
					continue
				}
				source := fmt.Sprintf("%s:%d", t.File, e.Line)
				background := isTrue(u.u.Input, "run_in_background")
				elapsed := dateParse(e.Timestamp) - dateParse(u.e.Timestamp)
				if elapsed < 0 { // Math.max(0, d): NaN stays NaN
					elapsed = 0
				}
				ran := jhonOperand(command, "executions", "run")
				// A background run ends in its task notification, not in this result.
				end, ended := ends[r.ID]
				failed, at, text := r.IsError, e.Timestamp, r.Text
				if background && ended {
					at, source = end.at, fmt.Sprintf("%s:%d", t.File, end.line)
					failed = end.exit == nil || *end.exit != 0
					text = s.backgroundOutput(end.output)
				}
				pr := CIGreen(command, text, !failed)
				if pr == "" && background && ended {
					// Read as pi's job notice is: the command, its exit, and its output.
					pr = ev.refs.NoticeCIGreen(jobNotice{command, text, end.exit})
				}
				if pr != "" && (!background || ended) {
					ev.ci = append(ev.ci, ciGreen{pr, at})
				}
				if !failed && slices.ContainsFunc(codeless(plain(command)), func(x Invocation) bool { return x.Program == "git" && x.Sub == "push" }) {
					ev.pushes = append(ev.pushes, at)
					if sha := pushedCommit(text); sha != "" && u.change != "" {
						ev.pushedTo = append(ev.pushedTo, pushOfChange{u.change, at, sha})
					}
				}
				if ran == "" && u.change != "" && DirectTest(command) && (!background || ended && end.exit != nil) {
					passed := !failed
					status := "pass"
					if failed {
						status = "fail"
					}
					run := testRun{change: u.change, at: at, passed: &passed, statuses: []string{status}, source: source, direct: t.File}
					if !background {
						run.durationMs = &elapsed
					}
					ev.runs = append(ev.runs, run)
				}
				if ran != "" {
					run := testRun{change: ran, at: at, statuses: []string{}, source: source}
					if x := ReadExecutions(text); x != nil {
						run.passed, run.why, run.commit, run.tree, run.statuses = &x.Passed, x.Why, x.Commit, x.Tree, x.Statuses
					} else if background && ended && end.exit != nil {
						// No output left to read: its exit code still says whether it passed.
						passed := !failed
						run.passed = &passed
					}
					// A run started in the background returns at once: its call measures nothing.
					if !background {
						run.durationMs = &elapsed
					}
					ev.runs = append(ev.runs, run)
				}
				if checked := jhonOperand(command, "deliver", "check"); checked != "" && !r.IsError {
					if c := ReadDeliverCheck(r.Text); c != nil {
						ev.checks = append(ev.checks, readyCheck{change: checked, action: c.Action, artifact: c.Artifact, commit: c.Commit, tree: c.Tree,
							testedTree: c.TestedTree, risked: c.Risked, at: e.Timestamp, source: source})
					}
				}
				if u.change != "" && !r.IsError && jhonRisk(command) {
					if tier := riskTier(r.Text); tier != "" {
						ev.risks = append(ev.risks, riskRead{u.change, e.Timestamp, tier, source})
					}
				}
			}
		}
		ev.writes[t.File] = wrote
	}
	return ev
}

// changeRoots is the roots a change ran in: its delegations' roots, and the
// main thread's directory, each with the same directory in the other
// worktrees of its repository, where a lane may keep a change no other tree
// has.
func changeRoots(s *Session, change string) []string {
	var roots []string
	for _, a := range s.Subagents {
		if a.change == change && a.root != "" {
			addOnce(&roots, a.root)
		}
	}
	for _, e := range s.Main.Entries {
		if e.Cwd != "" {
			addOnce(&roots, normalizeRoot(e.Cwd))
		}
	}
	var out []string
	for _, r := range roots {
		for _, w := range worktreesOf(r) {
			addOnce(&out, w)
		}
	}
	return out
}

// deliveredTree is the tree a delivery delivers: the tree a run or a check
// recorded for the pushed commit sha; else the last tree a run or a check
// observed before it; ok is false when none is known.
func deliveredTree(sha string, runs []testRun, checks []readyCheck) (string, bool) {
	if sha != "" {
		for _, r := range runs {
			if r.commit != "" && r.tree != "" && strings.HasPrefix(r.commit, sha) {
				return r.tree, true
			}
		}
		for _, c := range checks {
			if c.commit != "" && strings.HasPrefix(c.commit, sha) && (c.tree != "" || c.testedTree != "") {
				return or(c.tree, c.testedTree), true
			}
		}
	}
	type seen struct {
		at   float64
		tree string
	}
	var all []seen
	for _, r := range runs {
		if r.tree != "" {
			all = append(all, seen{dateParse(r.at), r.tree})
		}
	}
	for _, c := range checks {
		if c.tree != "" || c.testedTree != "" {
			all = append(all, seen{dateParse(c.at), or(c.tree, c.testedTree)})
		}
	}
	slices.SortStableFunc(all, func(p, q seen) int { return sign(p.at - q.at) })
	if len(all) == 0 {
		return "", false
	}
	return all[len(all)-1].tree, true
}

// or is a || b for strings.
func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// sign is a comparator's result for a numeric difference; NaN compares equal, as Array.prototype.sort reads it.
func sign(d float64) int {
	switch {
	case d < 0:
		return -1
	case d > 0:
		return 1
	}
	return 0
}

var pushed = lazyre.New(`\b[0-9a-f]{7,40}\.\.\.?([0-9a-f]{7,40})` + space)

// pushedCommit is the commit a push result reports it updated the remote to, or "".
func pushedCommit(text string) string {
	if m := pushed.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return ""
}

// grantCovers is the grant classes that authorize each delivery kind, as the safety guard reads them.
var grantCovers = map[string][]string{"push": {"push"}, "merge": {"gh-write", "push"}, "deploy": {"deploy"}, "release": {"publish", "gh-write"}}

var (
	bashStdout = lazyre.New(`<bash-stdout>((?s:.)*?)</bash-stdout>`)
	grantedRe  = lazyre.New(`(?m)` + js.LineStart + `granted ([\w-]+) for (/` + nonSpace + `+) until (` + nonSpace + `+)(?:$|[\r\x{2028}\x{2029}])`)
)

// grant is one grant the owner ran with `! jhon grant`, or a class the
// owner's mode (`jhon mode`) covered while it held: mode names it.
type grant struct{ cls, root, until, at, mode string }

// ownerGrants is the grants the owner ran with `! jhon grant`: those in the
// main thread, from their recorded output (one grant of several classes
// prints one line per class), and every one in the grants' history, run
// in another session or terminal.
func ownerGrants(s *Session) []grant {
	var out []grant
	readTree(grantstore.HistoryFile())
	for _, r := range grantstore.History() {
		out = append(out, grant{r.Class, r.Root, r.ExpiresAt, r.GrantedAt, ""})
	}
	// A yes once covers the call it answered, which runs right after it: a
	// grant of its classes, for any project, for onceWindow from its answer.
	for _, a := range s.Authorizations {
		if a.Scope != "once" {
			continue
		}
		t := dateParse(a.At)
		if math.IsNaN(t) {
			continue
		}
		until := isodate.Format(time.UnixMilli(int64(t) + onceWindow.Milliseconds()))
		for _, c := range a.Classes {
			out = append(out, grant{c, "", until, a.At, ""})
		}
	}
	for _, e := range s.Main.Entries {
		if e.Role != "user" {
			continue
		}
		prompt := ""
		if e.Prompt != nil {
			prompt = *e.Prompt
		}
		for _, block := range bashStdout.FindAllStringSubmatch(strings.Join(append([]string{prompt}, e.Texts...), "\n"), -1) {
			for _, m := range grantedRe.FindAllStringSubmatch(block[1], -1) {
				out = append(out, grant{m[1], m[2], m[3], e.Timestamp, ""})
			}
		}
	}
	return out
}

// describe is the grant as a note names it: a grant with its project and
// expiry, the owner's mode for its project since it was set, or the
// owner's yes once to the guard's question.
func (g grant) describe() string {
	if g.mode != "" {
		return "the owner's " + g.mode + " mode for " + g.root + " since " + g.at
	}
	if g.root == "" {
		return "the owner's yes once to the guard's question for " + g.cls + " at " + g.at
	}
	return "the owner's " + g.cls + " grant for " + g.root + " until " + g.until
}

// forever is the end of a mode the owner has not turned off: a mode has no expiry.
const forever = "9999-12-31T23:59:59.999Z"

// modeGrants is each class a mode covered for its project, from when the
// owner set it until the next mode set for that project: the modes'
// history read as grants, so for refusal-bypass a call a mode let through
// is the owner's decision as a granted one is. delivered-untested does not
// read them: a mode passes a push without asking, never untested.
func modeGrants() []grant {
	readTree(grantstore.ModeHistoryFile())
	history := grantstore.ModeHistory()
	var out []grant
	for i, c := range history {
		until := forever
		for _, next := range history[i+1:] {
			if next.Root == c.Root {
				until = next.At
				break
			}
		}
		for _, cls := range grantstore.Classes {
			if grantstore.ModeCovers(c.Mode, cls) {
				out = append(out, grant{cls, c.Root, until, c.At, c.Mode})
			}
		}
	}
	return out
}

// ModeAllowed counts the calls each mode let through since since (the
// zero time: every one), from the modes' log.
func ModeAllowed(since time.Time) map[string]int {
	readTree(grantstore.AllowedFile())
	out := map[string]int{}
	for _, a := range grantstore.AllowedCalls() {
		if t, ok := isodate.Parse(a.At); since.IsZero() || ok && !t.Before(since) {
			out[a.Mode]++
		}
	}
	return out
}

// onceWindow is how long after the owner's yes once its call may run.
const onceWindow = 2 * time.Minute

// liveGrant is the owner's grant of one of classes live at `at` for the
// project, or its worktrees, that cwd ("" for any) lies in; nil when none is.
func liveGrant(grants []grant, classes []string, at, cwd string) *grant {
	t := dateParse(at)
	for _, g := range grants {
		if !slices.Contains(classes, g.cls) || !(dateParse(g.at) <= t && t < dateParse(g.until)) {
			continue
		}
		if cwd == "" || slices.ContainsFunc([]string{g.root, g.root + "-worktrees"}, func(r string) bool { return cwd == r || strings.HasPrefix(cwd, r+"/") }) {
			return &g
		}
	}
	return nil
}

// verifiedBefore is whether a change was independently verified by `at`: a
// verify agent of it had finished, or its verify report or approved
// Judgment Day record was written.
func verifiedBefore(s *Session, change, at string) bool {
	t := dateParse(at)
	for _, a := range s.Subagents {
		if a.AgentType != "verify" || a.change != change || len(a.Entries) == 0 {
			continue
		}
		if end := a.Entries[len(a.Entries)-1].Timestamp; end != "" && dateParse(end) <= t {
			return true
		}
	}
	for _, root := range changeRoots(s, change) {
		trackChange(root, change)
		v, _ := ldd.IndependentVerification(root, change, nil)
		if v == nil {
			continue
		}
		if st, err := os.Stat(v.File); err == nil && float64(st.ModTime().UnixNano())/1e6 <= t {
			return true
		}
	}
	return false
}

// Publishes is whether command runs `jhon publish`, which refuses to push
// anything but the release `jhon sync` built and tested from HEAD.
func Publishes(command string) bool {
	return slices.ContainsFunc(codeless(plain(command)), func(x Invocation) bool { return x.Program == "jhon" && x.Sub == "publish" })
}

// checkDeliveredUntested is delivered-untested (contracts/ldd.md,
// "Delivery"). A push, a merge into the default branch (MergesToDefault),
// or a deploy or a release (DeliveryCommand) whose result is not an error,
// delivers change X: the agent's change, or for a call with none, the
// change the agent's last `jhon executions run` or `jhon deliver check`
// named since the session was last resumed (Session.Resumes). Against X's test runs and ready checks recorded before it, in the
// transcript or on disk:
//
// Identity is the tree. The delivered tree is the pushed commit's, when a
// run or a check before it names that commit; else the last tree a run or
// a check observed before it. A ready check or a passing run counts only
// for that tree, and a failed run of that tree voids every earlier one. A
// direct test command that exited 0 counts as a passing run of an unknown
// tree, unless a write tool changed its transcript's tree after it.
//
//   - no passing run and no ready check is the breach (delivery/tested-revision);
//   - a deploy or a release with no ready check that names an immutable
//     artifact by digest is the breach too;
//   - a passing run, with no ready check and no pushed commit that is the
//     run's commit, is a suspect (delivery/same-tree, heuristic);
//   - a delivery that `jhon risk` read as high before it, with no independent
//     verification by then and no ready check that read the tier, is a
//     suspect (delivery/high-risk-verified, heuristic).
//
// A `gh pr merge <pr>` with no agent's change is the change its PR's branch
// or worktree names (prRefs.changeOf, then the worktree the merge itself
// cds into), among the changes the session names; with no such evidence,
// the change last named. A merge whose PR's CI reported green before it,
// with no push after, is tested whether or not a change is tied to it; any
// other delivery no change can be tied to is indeterminate. One finding per agent, change, and action. A delivery the owner authorized, under a live
// grant of its class for its project, is a note, not a finding.
func checkDeliveredUntested(s *Session, seen evidence, emit emitFunc, notes *[]string) {
	grants := ownerGrants(s)
	// rewritten is whether a direct run's transcript was written by a write tool after it, up to until.
	rewritten := func(r testRun, until float64) bool {
		return slices.ContainsFunc(seen.writes[r.direct], func(w string) bool { return dateParse(w) > dateParse(r.at) && dateParse(w) <= until })
	}
	type disk struct {
		runs   []testRun
		checks []readyCheck
	}
	cache := map[string]disk{}
	var changes []string
	for _, t := range s.transcripts() {
		for _, e := range t.Entries {
			for _, u := range e.ToolUses {
				if u.named != "" {
					addOnce(&changes, u.named)
				}
			}
		}
	}
	for _, a := range s.Subagents {
		if a.change != "" {
			addOnce(&changes, a.change)
		}
	}
	evidenceOf := func(change string) ([]testRun, []readyCheck) {
		d, ok := cache[change]
		if !ok {
			d.runs, d.checks = diskEvidence(change, changeRoots(s, change))
			cache[change] = d
		}
		var runs []testRun
		for _, r := range seen.runs {
			if r.change == change {
				runs = append(runs, r)
			}
		}
		var checks []readyCheck
		for _, c := range seen.checks {
			if c.change == change {
				checks = append(checks, c)
			}
		}
		return append(runs, d.runs...), append(checks, d.checks...)
	}
	for i, t := range s.transcripts() {
		own := s.own(i)
		type done struct {
			r ToolResult
			e *Entry
		}
		results := map[string]done{}
		for _, e := range t.Entries {
			for _, r := range e.ToolResults {
				results[r.ID] = done{r, e}
			}
		}
		reportedKey := map[string]bool{}
		named := ""
		resumes := 0
		for _, e := range t.Entries {
			// A resume ends the sitting the change was named in.
			for ; i == 0 && resumes < len(s.Resumes) && s.Resumes[resumes] < e.Line; resumes++ {
				named = ""
			}
			for _, u := range e.ToolUses {
				command, ok := strProp(u.Input, "command")
				if u.Name != "Bash" || !ok {
					continue
				}
				if n := u.named; n != "" {
					named = n
				}
				kind := "merge"
				if !MergesToDefault(command) {
					kind = DeliveryCommand(command)
				}
				d, ok := results[u.ID]
				if kind == "" || !ok || d.r.IsError {
					continue
				}
				// `jhon publish` pushes only the active release, which `jhon sync`
				// built from the committed HEAD after its tests passed: the
				// delivered revision is the tested one by construction.
				if Publishes(command) {
					continue
				}
				if g := liveGrant(grants, grantCovers[kind], d.e.Timestamp, e.Cwd); g != nil {
					*notes = append(*notes, fmt.Sprintf("%s (%s) %s at %s:%d ran under %s; delivered-untested is not read for it",
						t.Agent, t.AgentType, kind, t.File, e.Line, g.describe()))
					continue
				}
				change := or(own, named)
				if pr := MergedPR(command); own == "" && kind == "merge" && pr != "" {
					refs := seen.refs
					if refs.changeOf(pr, changes) == "" {
						refs = prRefs{worktrees: map[string][]string{}}
						for _, m := range cdInto.FindAllStringSubmatch(command, -1) {
							addTo(refs.worktrees, pr, filepath.Base(m[1]))
						}
					}
					change = or(refs.changeOf(pr, changes), change)
				}
				base := emitted{check: "delivered-untested", transcript: t.File, line: e.Line, agent: t.Agent, agentType: t.AgentType, evidence: short(command, 120), layout: byBase}
				key := change + ":" + kind
				if change == "" {
					key = "null:" + kind
				}
				if reportedKey[key] {
					continue
				}
				deliveredAt := dateParse(d.e.Timestamp)
				before := func(x string) bool { return dateParse(x) <= deliveredAt }
				// A merged PR whose CI reported every check green after the last
				// push is a tested delivery: the CI ran on what is merged, whether
				// or not a change can be tied to it.
				if pr := MergedPR(command); kind == "merge" && pr != "" && slices.ContainsFunc(seen.ci, func(g ciGreen) bool {
					return g.pr == pr && before(g.at) && !slices.ContainsFunc(seen.pushes, func(p string) bool { return dateParse(p) > dateParse(g.at) && before(p) })
				}) {
					continue
				}
				if change == "" {
					reportedKey[key] = true
					f := base
					f.severity, f.kind, f.anchor, f.layout = "suspect", "indeterminate", "delivery/same-tree", byBaseKind
					f.message = fmt.Sprintf("a %s no change can be tied to: no launch block or openspec path names one, and no earlier jhon executions run or jhon deliver check in this agent does, so no test run can be read for it", kind)
					emit(f)
					continue
				}
				runs, checks := evidenceOf(change)
				action := "merge"
				if kind == "deploy" || kind == "release" {
					action = kind
				}
				sha := ""
				if kind == "push" {
					sha = pushedCommit(d.r.Text)
				}
				// The commit this delivery delivers, as far as the session shows it:
				// a push's, or for a merge the change's last push before it.
				deliveredCommit := sha
				if kind == "merge" {
					for _, p := range seen.pushedTo {
						if p.change == change && before(p.at) {
							deliveredCommit = p.sha
						}
					}
				}
				var priorRuns []testRun
				for _, r := range runs {
					if before(r.at) {
						if r.next != "" && deliveredCommit != "" && strings.HasPrefix(r.next, deliveredCommit) {
							r = r.asNext()
						}
						priorRuns = append(priorRuns, r)
					}
				}
				slices.SortStableFunc(priorRuns, func(p, q testRun) int { return sign(dateParse(p.at) - dateParse(q.at)) })
				var priorChecks []readyCheck
				for _, c := range checks {
					if before(c.at) && c.action == action {
						priorChecks = append(priorChecks, c)
					}
				}
				tree, known := deliveredTree(sha, priorRuns, priorChecks)
				ofTree := func(trees ...string) bool {
					return !known || !slices.ContainsFunc(trees, func(x string) bool { return x != "" }) || slices.Contains(trees, tree)
				}
				// A failed run of the delivered tree voids the evidence before it.
				var lastRun *testRun
				if known {
					for k := range priorRuns {
						if r := priorRuns[k]; r.direct == "" && r.passed != nil && r.tree == tree {
							lastRun = &priorRuns[k]
						}
					}
				}
				since := math.Inf(-1)
				if lastRun != nil && !*lastRun.passed {
					since = dateParse(lastRun.at)
				}
				var ready []readyCheck
				for _, c := range priorChecks {
					if ofTree(c.tree, c.testedTree) && dateParse(c.at) > since {
						ready = append(ready, c)
					}
				}
				var passing []testRun
				for _, r := range priorRuns {
					if r.passed != nil && *r.passed && ofTree(r.tree) && dateParse(r.at) > since && !(r.direct != "" && rewritten(r, deliveredAt)) {
						passing = append(passing, r)
					}
				}
				var why *emitted
				set := func(anchor, severity, message string) {
					why = &emitted{anchor: anchor, severity: severity, message: message}
				}
				switch {
				case action != "merge" && !slices.ContainsFunc(ready, func(c readyCheck) bool {
					return c.artifact != nil && (ldd.Digest.MatchString(*c.artifact) || (action == "deploy" && ldd.IsCommitArtifact(*c.artifact) && InfraDeploy(command)))
				}):
					set("delivery/tested-revision", "violation", fmt.Sprintf("%s %s runs a %s of %s with no ready `jhon deliver check %s --action %s --artifact <digest>` (or the full commit for an infrastructure deploy) before it: the deploy names no immutable artifact",
						t.AgentType, t.Agent, kind, change, change, action))
				case len(passing) == 0 && len(ready) == 0:
					if !math.IsInf(since, -1) {
						set("delivery/tested-revision", "violation", fmt.Sprintf("%s %s delivers %s (%s) whose tree %s last had a test run that did not pass: %s (%s)",
							t.AgentType, t.Agent, change, kind, tree, or(lastRun.why, "it failed"), lastRun.source))
					} else {
						set("delivery/tested-revision", "violation", fmt.Sprintf("%s %s delivers %s (%s) with no passing test run or ready check of the delivered tree recorded before, in this session or in its execution record",
							t.AgentType, t.Agent, change, kind))
					}
				case len(ready) == 0:
					if sha == "" || !slices.ContainsFunc(passing, func(r testRun) bool { return r.commit != "" && strings.HasPrefix(r.commit, sha) }) {
						what := "passing test run"
						if !slices.ContainsFunc(passing, func(r testRun) bool { return r.direct == "" }) {
							what = "direct passing test command"
						}
						shown := "no pushed commit is shown"
						if sha != "" {
							shown = "the pushed commit " + sha + " is no tested commit"
						}
						set("delivery/same-tree", "suspect", fmt.Sprintf("%s %s delivers %s (%s) after a %s, but nothing ties the delivered revision to the tested tree: no ready jhon deliver check, and %s",
							t.AgentType, t.Agent, change, kind, what, shown))
					}
				}
				// orchestrator.md "Risk": a high tier is delivered after an independent verification.
				var risk *riskRead
				for k, x := range seen.risks {
					if x.change == change && before(x.at) {
						risk = &seen.risks[k]
					}
				}
				if why == nil && risk != nil && risk.tier == "high" && !slices.ContainsFunc(ready, func(c readyCheck) bool { return c.risked }) && !verifiedBefore(s, change, d.e.Timestamp) {
					set("delivery/high-risk-verified", "suspect", fmt.Sprintf("%s %s delivers %s (%s) after jhon risk read it as high (%s), with no independent verification before it: no verify agent ran for it, and no verify-report.md stating verdict: pass or Judgment Day record closed APPROVED was written",
						t.AgentType, t.Agent, change, kind, risk.source))
				}
				if why == nil {
					continue
				}
				reportedKey[key] = true
				f := base
				f.severity, f.anchor, f.message = why.severity, why.anchor, why.message+` (contracts/ldd.md, "Delivery")`
				emit(f)
			}
		}
	}
}
