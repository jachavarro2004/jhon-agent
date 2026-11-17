package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// This file is pi's shell/lib/launch-context.ts for Claude Code (T134, L1036
// K1): what a sub-agent the main session launches through the Agent tool
// gets after its brief, the same blocks pi's launch appends:
//
//   - `## Decisions on record`: the decisions of the launch's handoff packet
//     (ldd.LaunchHandoff, `jhon context [<change>] --phase none --handoff`
//     read in this process), id, type and title; at most 12 lines, 2 s.
//   - `## Code map`: what `codegraph query <name> --json` gives for up to 8
//     of the files and symbols the brief names, with the callers of the
//     first symbols; at most 4096 bytes, 3 s in all, and only when cwd's
//     root has an index, or is a linked worktree whose main checkout has
//     one, which is then asked (`-p <main checkout>`) and only results whose
//     files exist at the same path in the worktree are kept.
//
//   - `## Tests to judge` (T137, L1064 KB2): for verify and the 4R lenses
//     only, the tests the selected change's execution record marks as turned
//     green by editing only their test (ldd.LaunchTestsToJudge), for the
//     validator to judge whether each was weakened; within 2 s.
//
// All are information, never a gate: a failure, a timeout or an empty
// result leaves the heading out. The blocks follow the brief, each after a
// blank line and opening with a `## ` heading, so a writer's `## Allowed edit
// surfaces` ends where they begin (guard.SurfacesOf stops at a heading).

const (
	decisionsHeading = "## Decisions on record"
	codeMapHeading   = "## Code map"
	decisionsIntro   = "Added at launch from `jhon context --handoff`; search Engram before deciding against one."
	codeMapIntro     = "Added at launch from CodeGraph for what the brief names; lines are as of the launch."

	decisionsLines = 12
	decisionsTime  = 2 * time.Second
	codeMapBytes   = 4096
	codeMapTime    = 3 * time.Second
	codeMapNames   = 8
	// callerSymbols are the symbols whose callers are asked for too, the first the brief names.
	callerSymbols = 3
	parallel      = 4
	// outputLimit is the most a CodeGraph answer may print before it is dropped.
	outputLimit = 1 << 20
)

// launchTestsToJudge is the tests-to-judge block of a launch; a test may replace it.
var launchTestsToJudge = ldd.LaunchTestsToJudge

// testsToJudge is launchTestsToJudge's block for agent in cwd; "" on any
// failure or past within.
func testsToJudge(agent, cwd string, within time.Duration) string {
	if !slices.Contains(ldd.TestJudges, agent) {
		return ""
	}
	done := make(chan string, 1)
	go func() {
		defer func() {
			if recover() != nil {
				done <- ""
			}
		}()
		done <- launchTestsToJudge(agent, cwd)
	}()
	select {
	case block := <-done:
		return block
	case <-time.After(within):
		return ""
	}
}

// launchHandoff is the handoff packet the decisions come from; a test may replace it.
var launchHandoff = ldd.LaunchHandoff

// codegraphProgram is the CodeGraph the code map runs, found on PATH.
var codegraphProgram = "codegraph"

// oneLine is s on one line, whitespace collapsed, cut to max characters with an ellipsis.
func oneLine(s string, max int) string {
	t := strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(t) > max {
		return string([]rune(t)[:max-1]) + "…"
	}
	return t
}

// decisionsBlock is the decisions block of a handoff packet's decisions, or "" with none.
func decisionsBlock(decisions []ldd.Decision) string {
	var lines []string
	for _, d := range decisions {
		if len(lines) == decisionsLines-2 {
			break
		}
		if strings.TrimSpace(d.Title) == "" {
			continue
		}
		kind := d.Type
		if kind == "" {
			kind = "decision"
		}
		lines = append(lines, "- #"+strconv.FormatFloat(d.ID, 'f', -1, 64)+" ("+oneLine(kind, 24)+") "+oneLine(d.Title, 160))
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(append([]string{decisionsHeading, decisionsIntro}, lines...), "\n")
}

// decisionsOnRecord is the decisions block of a launch in cwd; "" on any
// failure, past the deadline, or with none. Past it, the packet's Engram
// searches are killed and its reading is left behind.
func decisionsOnRecord(cwd string, within time.Duration) string {
	ctx, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	done := make(chan string, 1)
	go func() {
		defer func() {
			if recover() != nil {
				done <- ""
			}
		}()
		c, err := launchHandoff(ctx, cwd)
		if err != nil {
			done <- ""
			return
		}
		for _, f := range c.Fields {
			if f.Key == "decisions" {
				d, _ := f.Value.([]ldd.Decision)
				done <- decisionsBlock(d)
				return
			}
		}
		done <- ""
	}()
	select {
	case block := <-done:
		if ctx.Err() != nil {
			return ""
		}
		return block
	case <-ctx.Done():
		return ""
	}
}

// A briefName is a file (path, relative to cwd) or a symbol the brief names.
type briefName struct {
	file, symbol string
}

var (
	ident     = lazyre.New(`^[A-Za-z_$][\w$]*(?:(?:\.|::|#)[A-Za-z_$][\w$]*)*(?:\(\))?$`)
	pathish   = lazyre.New(`^[\w.@-]+(?:/[\w.@-]+)*\.\w+$|^(?:[\w.@-]+/)+[\w.@-]+$`)
	lineMark  = lazyre.New(`:\d+(?:[-:]\d+)?$`)
	ticked    = lazyre.New("`([^`\n]{1,200})`")
	pathRef   = lazyre.New(`^((?:[\w.@-]+/)+[\w.@-]+\.\w+(?::\d+(?:-\d+)?)?)`)
	identPart = lazyre.New(`\.|::|#`)
)

// wordByte is whether b may not precede a path reference: [\w/`.-].
func wordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.IndexByte("_/`.-", b) >= 0
}

// pathStart is whether b may open a path reference: [\w.@-].
func pathStart(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.IndexByte("_.@-", b) >= 0
}

// briefNames is the files (existing under cwd) and symbols (backticked
// identifiers) the brief names, in order, at most codeMapNames.
func briefNames(brief, cwd string) []briefName {
	var out []briefName
	add := func(n briefName) {
		if len(out) < codeMapNames && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	file := func(raw string) bool {
		path := strings.TrimPrefix(lineMark.ReplaceAllString(raw, ""), "./")
		if !pathish.MatchString(path) || filepath.IsAbs(path) || slices.Contains(strings.Split(path, "/"), "..") {
			return false
		}
		if fi, err := os.Stat(filepath.Join(cwd, path)); err != nil || !fi.Mode().IsRegular() {
			return false
		}
		add(briefName{file: path})
		return true
	}
	// Backticked spans in order, then the path:line references and paths outside them.
	type token struct {
		at     int
		text   string
		ticked bool
	}
	var tokens []token
	for _, m := range ticked.FindAllStringSubmatchIndex(brief, -1) {
		tokens = append(tokens, token{m[0], strings.TrimSpace(brief[m[2]:m[3]]), true})
	}
	for i := 0; i < len(brief); i++ {
		if !pathStart(brief[i]) || i > 0 && wordByte(brief[i-1]) {
			continue
		}
		if m := pathRef.FindStringSubmatchIndex(brief[i:]); m != nil {
			tokens = append(tokens, token{i, brief[i+m[2] : i+m[3]], false})
			i += m[1] - 1
		}
	}
	sort.SliceStable(tokens, func(a, b int) bool { return tokens[a].at < tokens[b].at })
	for _, t := range tokens {
		if file(t.text) || !t.ticked || !ident.MatchString(t.text) {
			continue
		}
		parts := identPart.Split(strings.TrimSuffix(t.text, "()"), -1)
		if name := parts[len(parts)-1]; len(name) >= 3 {
			add(briefName{symbol: name})
		}
	}
	return out
}

// codegraphRoot is the root CodeGraph indexes for cwd: the git top level, else cwd.
func codegraphRoot(cwd string) string {
	if out, err := project.ReadGitCmd(cwd, "rev-parse", "--show-toplevel").Output(); err == nil {
		if top := strings.TrimSpace(string(out)); top != "" {
			return top
		}
	}
	abs, _ := filepath.Abs(cwd)
	return abs
}

// hasIndex is whether root holds a CodeGraph database: CODEGRAPH_DIR's, .codegraph's or .codegraph-wsl's.
func hasIndex(root string) bool {
	dirs := []string{".codegraph", ".codegraph-wsl"}
	if named := os.Getenv("CODEGRAPH_DIR"); named != "" && !strings.ContainsAny(named, `/\`) && named != "." && named != ".." {
		dirs = append([]string{named}, dirs...)
	}
	return slices.ContainsFunc(dirs, func(d string) bool {
		_, err := os.Stat(filepath.Join(root, d, "codegraph.db"))
		return err == nil
	})
}

// codegraphIndexRoot is the checkout whose index answers for cwd: cwd's root
// when indexed, else the main checkout of the linked worktree cwd is in when
// that one is; "" for none. It only reads.
func codegraphIndexRoot(cwd string) string {
	root := codegraphRoot(cwd)
	if hasIndex(root) {
		return root
	}
	out, err := project.ReadGitCmd(cwd, "rev-parse", "--path-format=absolute", "--git-common-dir", "--git-dir").Output()
	if err != nil {
		return ""
	}
	dirs := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(dirs) != 2 || dirs[0] == "" || dirs[1] == "" || dirs[0] == dirs[1] || filepath.Base(dirs[0]) != ".git" {
		return ""
	}
	if main := filepath.Dir(dirs[0]); main != root && hasIndex(main) {
		return main
	}
	return ""
}

// capped is a writer that keeps up to outputLimit bytes and says when more came.
type capped struct {
	bytes.Buffer
	over bool
}

func (c *capped) Write(p []byte) (int, error) {
	if c.Len()+len(p) > outputLimit {
		c.over = true
		return 0, os.ErrInvalid
	}
	return c.Buffer.Write(p)
}

// jsonOf is the stdout of program args in cwd parsed as JSON, nil on any
// failure or past ctx's deadline (the process is killed).
func jsonOf(ctx context.Context, program string, args []string, cwd string) any {
	if ctx.Err() != nil {
		return nil
	}
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = cwd
	cmd.WaitDelay = 100 * time.Millisecond
	var out capped
	cmd.Stdout = &out
	if cmd.Run() != nil || out.over {
		return nil
	}
	var v any
	if json.Unmarshal(out.Bytes(), &v) != nil {
		return nil
	}
	return v
}

// jsText is v as JavaScript's String prints it, undefined for none.
func jsText(v any) string {
	if v == nil {
		return "undefined"
	}
	return js.String(v)
}

// node is one result of `codegraph query --json`.
type node map[string]any

// matchingNodes is the nodes of a query's result that are the named symbol, or live in the named file.
func matchingNodes(result any, want briefName, root, cwd string) []node {
	var nodes []node
	list, _ := result.([]any)
	for _, r := range list {
		m, _ := r.(map[string]any)
		n, _ := m["node"].(map[string]any)
		_, named := n["name"].(string)
		_, filed := n["filePath"].(string)
		if named && filed {
			nodes = append(nodes, n)
		}
	}
	var out []node
	if want.symbol != "" {
		for _, n := range nodes {
			if n["name"] == want.symbol && len(out) < 2 {
				out = append(out, n)
			}
		}
		return out
	}
	rel, err := filepath.Rel(root, filepath.Join(cwd, want.file))
	if err != nil {
		return nil
	}
	for _, n := range nodes {
		if n["filePath"] == filepath.ToSlash(rel) && n["kind"] != "file" && len(out) < 3 {
			out = append(out, n)
		}
	}
	return out
}

// callersOf is the callers of `codegraph callers <name> --json` whose files are kept, as `name (path:line)`.
func callersOf(result any, kept func(string) bool) []string {
	var out []string
	m, _ := result.(map[string]any)
	defs, _ := m["definitions"].([]any)
	for _, d := range defs {
		dm, _ := d.(map[string]any)
		callers, _ := dm["callers"].([]any)
		for _, c := range callers {
			cm, _ := c.(map[string]any)
			name, ok1 := cm["name"].(string)
			path, ok2 := cm["filePath"].(string)
			if ok1 && ok2 && kept(path) {
				out = append(out, name+" ("+path+":"+jsText(cm["startLine"])+")")
			}
		}
	}
	return out
}

// codeLine is one code map line: kind name path:line signature; callers.
func codeLine(n node, callers []string) string {
	sig := ""
	if s, ok := n["signature"].(string); ok && strings.TrimSpace(s) != "" {
		sig = " " + oneLine(s, 200)
	}
	who := ""
	if len(callers) > 0 {
		who = "; callers: " + strings.Join(callers[:min(5, len(callers))], ", ")
		if len(callers) > 5 {
			who += ", … (" + strconv.Itoa(len(callers)) + ")"
		}
	}
	return "- " + jsText(n["kind"]) + " " + jsText(n["name"]) + " " + jsText(n["filePath"]) + ":" + jsText(n["startLine"]) + sig + who
}

// codeMap is the code map block for the brief launched in cwd, or "" with no
// index, no names, no result, or on failure.
func codeMap(brief, cwd string, within time.Duration) string {
	indexRoot := codegraphIndexRoot(cwd)
	if indexRoot == "" {
		return ""
	}
	names := briefNames(brief, cwd)
	if len(names) == 0 {
		return ""
	}
	root := codegraphRoot(cwd)
	// Another checkout's index (this worktree's main one) is asked by path,
	// and its results count only where the same relative file is here.
	other := indexRoot != root
	var at []string
	if other {
		at = []string{"-p", indexRoot}
	}
	kept := func(path string) bool {
		if !other {
			return true
		}
		if filepath.IsAbs(path) || slices.Contains(strings.Split(path, "/"), "..") {
			return false
		}
		_, err := os.Stat(filepath.Join(root, path))
		return err == nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	var jobs [][]string
	var symbols []string
	for _, n := range names {
		word := n.symbol
		if n.file != "" {
			base := filepath.Base(n.file)
			word = strings.TrimSuffix(base, filepath.Ext(base))
		} else if len(symbols) < callerSymbols {
			symbols = append(symbols, n.symbol)
		}
		jobs = append(jobs, append([]string{"query", word, "--json"}, at...))
	}
	for _, s := range symbols {
		jobs = append(jobs, append([]string{"callers", s, "--json", "--limit", "5"}, at...))
	}
	results := make([]any, len(jobs))
	var next int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range min(parallel, len(jobs)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				i := next
				next++
				mu.Unlock()
				if i >= len(jobs) {
					return
				}
				results[i] = jsonOf(ctx, codegraphProgram, jobs[i], cwd)
			}
		}()
	}
	wg.Wait()
	var lines []string
	seen := map[string]bool{}
	for i, n := range names {
		var callers []string
		if s := slices.Index(symbols, n.symbol); n.symbol != "" && s >= 0 {
			callers = callersOf(results[len(names)+s], kept)
		}
		for _, m := range matchingNodes(results[i], n, root, cwd) {
			path, _ := m["filePath"].(string)
			if !kept(path) {
				continue
			}
			key := path + ":" + jsText(m["startLine"]) + ":" + jsText(m["name"])
			if seen[key] {
				continue
			}
			seen[key] = true
			// Callers go with the symbol's first definition only.
			lines = append(lines, codeLine(m, callers))
			callers = nil
		}
	}
	block := []string{codeMapHeading, codeMapIntro}
	size := len(codeMapHeading) + 1 + len(codeMapIntro)
	for _, line := range lines {
		if size+len(line)+1 > codeMapBytes {
			break
		}
		block = append(block, line)
		size += len(line) + 1
	}
	if len(block) == 2 {
		return ""
	}
	return strings.Join(block, "\n")
}

// launchContext is what a launch of agent appends to its brief: the
// blocks, each when it has something, gathered at once, each after a blank
// line; "" for none.
func launchContext(brief, cwd, agent string) string {
	var decisions, code, judge string
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); decisions = decisionsOnRecord(cwd, decisionsTime) }()
	go func() { defer wg.Done(); judge = testsToJudge(agent, cwd, decisionsTime) }()
	go func() {
		defer wg.Done()
		defer func() { _ = recover() }()
		code = codeMap(brief, cwd, codeMapTime)
	}()
	wg.Wait()
	var out string
	for _, b := range []string{decisions, code, judge} {
		if b != "" {
			out += "\n\n" + b
		}
	}
	return out
}

// launchInput is the tool input Claude Code runs in place of the payload's
// for an Agent call of the main session the guards allowed: the same input
// with only its prompt followed by launchContext, every other field kept
// byte for byte. It is nil for every other call (pi's, scoped to `*`, whose
// shell appends the blocks itself; a sub-agent's own launch, which carries
// an agent_id, L1036 K4; any tool but Agent), for a prompt that is not
// text, and when the blocks come out empty.
func launchInput(agent string, payload []byte) map[string]json.RawMessage {
	p, ok := read(bytes.NewReader(payload))
	if agent == "*" || !ok || p.Str("tool_name") != "Agent" || strings.TrimSpace(p.Str("agent_id")) != "" {
		return nil
	}
	var top, input map[string]json.RawMessage
	if json.Unmarshal(payload, &top) != nil || json.Unmarshal(top["tool_input"], &input) != nil {
		return nil
	}
	var prompt string
	if json.Unmarshal(input["prompt"], &prompt) != nil || strings.TrimSpace(prompt) == "" {
		return nil
	}
	added := launchContext(prompt, p.Cwd(), strings.TrimSpace(p.Obj("tool_input").Str("subagent_type")))
	if added == "" {
		return nil
	}
	updated := map[string]json.RawMessage{}
	for k, v := range input {
		updated[k] = v
	}
	updated["prompt"] = json.RawMessage(marshal(prompt + added))
	return updated
}

// launchAllowModes are the permission modes in which the launch's answer
// says allow: those in which Claude Code launches an Agent call the hooks
// allow without asking anyone. In plan mode (as sandboxedJSON), auto mode,
// dontAsk, and any other the answer carries no decision, so the mode's own
// rules decide about the launch as they do today (L1036 K2).
var launchAllowModes = []string{"default", "acceptEdits", "bypassPermissions"}

// launchAllowReason goes with the allow of a launch; Claude Code writes it to its debug log only.
const launchAllowReason = "Jhon's guards allow the launch; its brief is followed by the decisions on record and the code map"

// launchJSON is what pre-tool prints for an Agent call the guards allowed
// whose brief launchInput extended: updatedInput, the allow of the modes
// that take it, and the guards' notes for the agent.
func launchJSON(notes []string, updated map[string]json.RawMessage, mode string) string {
	out := map[string]any{"hookEventName": "PreToolUse", "updatedInput": updated}
	if slices.Contains(launchAllowModes, mode) {
		out["permissionDecision"], out["permissionDecisionReason"] = "allow", launchAllowReason
	}
	if len(notes) > 0 {
		out["additionalContext"] = strings.Join(notes, "; ")
	}
	return marshal(map[string]any{"hookSpecificOutput": out})
}
