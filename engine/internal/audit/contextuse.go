package audit

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// The context-use measurement (contracts/vocabulary.yaml, "Context use"):
// what each agent's transcript shows it consumed. Never a finding. A turn
// is one assistant response: the lines that share a `message.id` count
// once, and a line with no id is its own response. A turn's processed
// context is input_tokens + cache_creation_input_tokens +
// cache_read_input_tokens.

// SourceBytes is the UTF-8 bytes of tool-result text the agent was shown, by
// the source that produced it, in the order the report prints them; Total is their sum.
type SourceBytes struct {
	Total, FullRead, RangedRead, ShellRead, Codegraph, Engram, Search, Other int
}

var sourceNames = []string{"total", "full_read_bytes", "ranged_read_bytes", "shell_read_bytes", "codegraph_bytes", "engram_bytes", "search_bytes", "other_bytes"}

func (b *SourceBytes) fields() []*int {
	return []*int{&b.Total, &b.FullRead, &b.RangedRead, &b.ShellRead, &b.Codegraph, &b.Engram, &b.Search, &b.Other}
}

// AgentContextUse is what one agent's transcript shows it consumed.
type AgentContextUse struct {
	Agent, AgentType, Phase, Change, Runtime, Version, Transcript string
	Turns, TurnsWithoutUsage                                      int
	Peak, Total, Input, CacheCreation, CacheRead                  float64
	Bytes                                                         SourceBytes
	Rereads, Reloads, OverLimit                                   int
	// Start is the timestamp of the transcript's first line that has one, "" for none.
	Start string
	// End is the timestamp of its last line that has one, "" for none.
	End string
	// TurnContext is each turn's processed context, in the order the turns
	// started; 0 for a turn with no usage. Its sum is Total.
	TurnContext []float64
	// RereadFiles are the paths it read again, most often first, at most rereadFilesShown.
	RereadFiles []RereadFile
}

// A RereadFile is a path an agent read again after reading it whole, and how many times.
type RereadFile struct {
	Path  string
	Count int
}

// rereadFilesShown is how many reread paths an agent's measurement names.
const rereadFilesShown = 5

// groupKey is a grouping's key: nil, a string, or a runtime and version.
type groupKey any

type runtimeVersion struct{ runtime, version string }

// ContextUseGroup is one aggregation row: the agents whose value of the
// grouping is Key, their count, the sums, and the maximum peak.
type ContextUseGroup struct {
	Key                              groupKey
	Agents, Turns, TurnsWithoutUsage int
	Total, PeakMax                   float64
	Input, CacheCreation, CacheRead  float64
	Bytes                            SourceBytes
	Rereads, Reloads, OverLimit      int
}

// LongAgent is a sub-agent over LongAgentTurns or LongAgentPeak.
type LongAgent struct {
	Agent, AgentType, Change string
	Turns                    int
	Peak                     float64
	Why                      string
}

// ContextUse is the context-use measurement of a session.
type ContextUse struct {
	Agents                                    []*AgentContextUse
	LongAgents                                []LongAgent
	ByPhase, ByChange, ByAgentType, ByRuntime []*ContextUseGroup
}

// searchPrograms are the programs whose output is a search over files.
var searchPrograms = []string{"rg", "grep", "egrep", "fgrep", "fd", "find", "ag", "ack"}

// readerValueOptions are the options of a shell reader that take a separate value.
var readerValueOptions = map[string][]string{
	"head": {"-n", "-c", "--lines", "--bytes"}, "tail": {"-n", "-c", "--lines", "--bytes"},
	"bat": {"-r", "--line-range", "-l", "--language", "-H", "--highlight-line", "--style", "--theme", "--color", "--paging", "-m", "--map-syntax", "--tabs", "--wrap", "--terminal-width", "--file-name"},
	"sed": {"-e", "--expression", "-f", "--file", "-l", "--line-length"}, "cat": {}, "less": {},
}

var (
	sedQuiet      = lazyre.New(`^-[a-zA-Z]*n[a-zA-Z]*$`)
	sedInPlace    = lazyre.New(`^-[a-zA-Z]*i`)
	sedScriptOpt  = lazyre.New(`^(-e|--expression|-f|--file)$`)
	sedScriptFlag = lazyre.New(`^(-[a-zA-Z]*[ef]|--expression=|--file=)`)
	batRange      = lazyre.New(`^(-r|--line-range)`)
)

// ShellRead is one file a shell read reads, and whether whole.
type ShellRead struct {
	Path  string
	Whole bool
}

// ShellReads is the files one invocation reads, when it is a shell read:
// `cat`, `bat`, or `less` of a file is a whole read, and `bat
// -r`/`--line-range`, `head`, `tail`, and `sed -n` (never `sed -i`) are
// partial. A reader with no file operand reads a pipe, and is not a shell read.
func ShellReads(program string, args []string) []ShellRead {
	takes, ok := readerValueOptions[program]
	if !ok {
		return nil
	}
	var opts []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") && a != "-" {
			opts = append(opts, a)
		}
	}
	if program == "sed" && (!slices.ContainsFunc(opts, func(o string) bool { return sedQuiet.MatchString(o) || o == "--quiet" || o == "--silent" }) ||
		slices.ContainsFunc(opts, func(o string) bool { return sedInPlace.MatchString(o) || strings.HasPrefix(o, "--in-place") })) {
		return nil
	}
	var operands []string
	script := program == "sed"
	for k := 0; k < len(args); k++ {
		a := args[k]
		if slices.Contains(takes, a) {
			if program == "sed" && sedScriptOpt.MatchString(a) {
				script = false
			}
			k++
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			if program == "sed" && sedScriptFlag.MatchString(a) {
				script = false
			}
			continue
		}
		operands = append(operands, a)
	}
	if script && len(operands) > 0 {
		operands = operands[1:]
	}
	ranged := program == "bat" && slices.ContainsFunc(opts, batRange.MatchString)
	whole := program == "cat" || program == "less" || (program == "bat" && !ranged)
	var out []ShellRead
	for _, f := range operands {
		if f != "-" {
			out = append(out, ShellRead{f, whole})
		}
	}
	return out
}

// readsOf is the shell reads of a command line, heredoc bodies aside.
func readsOf(command string) []ShellRead {
	var out []ShellRead
	for _, x := range plain(command) {
		out = append(out, ShellReads(x.Program, x.Args)...)
	}
	return out
}

// mcpServer is the MCP server a tool belongs to (`mcp__<server>__<tool>`); ok is false for a built-in tool.
func mcpServer(name string) (string, bool) {
	if !strings.HasPrefix(name, "mcp__") {
		return "", false
	}
	parts := strings.Split(name, "__")
	return parts[1], true
}

// BytesSource is the source a tool result's bytes count under (a SourceBytes JSON name).
func BytesSource(u ToolUse) string {
	if u.Name == "Read" {
		if has(u.Input, "offset") || has(u.Input, "limit") {
			return "ranged_read_bytes"
		}
		return "full_read_bytes"
	}
	if u.Name == "Grep" || u.Name == "Glob" {
		return "search_bytes"
	}
	if server, ok := mcpServer(u.Name); ok {
		switch {
		case strings.Contains(server, integrations.CodeGraph):
			return "codegraph_bytes"
		case strings.Contains(server, integrations.Engram):
			return "engram_bytes"
		}
		return "other_bytes"
	}
	if c, ok := strProp(u.Input, "command"); ok && u.Name == "Bash" {
		invs := plain(c)
		switch {
		case slices.ContainsFunc(invs, func(i Invocation) bool { return i.Program == codegraphBinary() }):
			return "codegraph_bytes"
		case slices.ContainsFunc(invs, func(i Invocation) bool { return len(ShellReads(i.Program, i.Args)) > 0 }):
			return "shell_read_bytes"
		case slices.ContainsFunc(invs, func(i Invocation) bool { return slices.Contains(searchPrograms, i.Program) }):
			return "search_bytes"
		}
	}
	return "other_bytes"
}

// readDefaultLimit is the lines a Read with no `limit` shows at most (the runtime's default limit).
const readDefaultLimit = 2000

// WholeReadLineLimit is the line count at which a whole read is counted in `whole_reads_over_limit`.
const WholeReadLineLimit = 300

// pipes is whether a command, quotes aside, pipes one program's output into another (`|`, not `||`).
func pipes(command string) bool {
	s := unquoted(command)
	for i := 0; i < len(s); i++ {
		if s[i] == '|' && (i == 0 || s[i-1] != '|') && (i+1 == len(s) || s[i+1] != '|') {
			return true
		}
	}
	return false
}

var numberedLine = lazyre.New(`^` + space + `*(\d+)\t`)

// ShownRange is the line numbers a Read result shows: the first `<n>\t`
// line and every line after it numbered in sequence; ok is false for none.
func ShownRange(text string) (first, last float64, ok bool) {
	for _, l := range strings.Split(text, "\n") {
		m := numberedLine.FindStringSubmatch(l)
		if m == nil {
			if first != 0 {
				break
			}
			continue
		}
		n, _ := strconv.ParseFloat(m[1], 64)
		if first == 0 {
			first, last = n, n
			continue
		}
		if n != last+1 {
			break
		}
		last = n
	}
	return first, last, first != 0
}

type call struct {
	u ToolUse
	r ToolResult
}

// calls is each successful tool call of a transcript with its result, in order.
func calls(t *Transcript) []call {
	results := map[string]ToolResult{}
	for _, e := range t.Entries {
		for _, r := range e.ToolResults {
			results[r.ID] = r
		}
	}
	var out []call
	for _, e := range t.Entries {
		for _, u := range e.ToolUses {
			if r, ok := results[u.ID]; ok && !r.IsError {
				out = append(out, call{u, r})
			}
		}
	}
	return out
}

// written is the path a successful write tool call changes; ok is false for any other call.
func written(u ToolUse) (string, bool) {
	key, ok := writeToolKey[u.Name]
	if !ok {
		return "", false
	}
	return strProp(u.Input, key)
}

// wholeRead is a Read with neither offset nor limit.
func wholeRead(u ToolUse) bool { return !has(u.Input, "offset") && !has(u.Input, "limit") }

// sameFileRereads is the Reads or shell reads of a path the agent already
// read whole, with no successful write of that path since. Paths are
// compared as written, lexically normalized, so the count is a lower bound.
//
// The paths are returned too, each with its count, most often first.
func sameFileRereads(cs []call) (int, []RereadFile) {
	whole := map[string]bool{}
	rereads := 0
	byPath := map[string]int{}
	var order []string
	read := func(path string, isWhole bool) {
		p := normalize(path)
		if whole[p] {
			rereads++
			if byPath[p] == 0 {
				order = append(order, p)
			}
			byPath[p]++
		}
		if isWhole {
			whole[p] = true
		}
	}
	for _, c := range cs {
		if p, ok := written(c.u); ok {
			delete(whole, normalize(p))
		} else if p, ok := strProp(c.u.Input, "file_path"); ok && c.u.Name == "Read" {
			read(p, wholeRead(c.u))
		} else if cmd, ok := strProp(c.u.Input, "command"); ok && c.u.Name == "Bash" {
			for _, f := range readsOf(cmd) {
				read(f.Path, f.Whole)
			}
		}
	}
	files := make([]RereadFile, len(order))
	for i, p := range order {
		files[i] = RereadFile{p, byPath[p]}
	}
	slices.SortStableFunc(files, func(a, b RereadFile) int { return b.Count - a.Count })
	return rereads, files
}

// redundantReloads is the whole reads of a path the agent already read
// whole and complete, whose result is byte-identical, with no successful
// write of it in between. Complete is a Read with neither offset nor limit
// that showed fewer lines than the default limit, or an unpiped whole shell read.
func redundantReloads(cs []call) int {
	held := map[string]string{}
	reloads := 0
	load := func(path, text string, complete bool) {
		p := normalize(path)
		if prev, ok := held[p]; complete && ok && prev == text {
			reloads++
		}
		if complete {
			held[p] = text
		} else {
			delete(held, p)
		}
	}
	for _, c := range cs {
		if p, ok := written(c.u); ok {
			delete(held, normalize(p))
		} else if p, ok := strProp(c.u.Input, "file_path"); ok && c.u.Name == "Read" {
			first, last, shown := ShownRange(c.r.Shown)
			load(p, c.r.Shown, wholeRead(c.u) && shown && last-first+1 < readDefaultLimit)
		} else if cmd, ok := strProp(c.u.Input, "command"); ok && c.u.Name == "Bash" {
			command := WithoutHeredocs(cmd)
			reads := readsOf(cmd)
			if len(reads) == 1 {
				load(reads[0].Path, c.r.Shown, reads[0].Whole && !pipes(command))
			} else {
				for _, f := range reads {
					delete(held, normalize(f.Path))
				}
			}
		}
	}
	return reloads
}

var trailingNewlines = lazyre.New(`\n+$`)

// wholeReadsOverLimit is the whole reads of a file the transcript shows to
// have WholeReadLineLimit lines or more: a Read with neither offset nor
// limit numbered from 1 to that line or beyond, or an unpiped whole shell
// read of one file whose output has that many lines.
func wholeReadsOverLimit(cs []call) int {
	n := 0
	for _, c := range cs {
		if c.u.Name == "Read" && wholeRead(c.u) {
			if first, last, ok := ShownRange(c.r.Shown); ok && first == 1 && last >= WholeReadLineLimit {
				n++
			}
		} else if cmd, ok := strProp(c.u.Input, "command"); ok && c.u.Name == "Bash" {
			command := WithoutHeredocs(cmd)
			reads := readsOf(cmd)
			if len(reads) == 1 && reads[0].Whole && !pipes(command) && strings.Count(trailingNewlines.ReplaceAllString(c.r.Shown, ""), "\n")+1 >= WholeReadLineLimit {
				n++
			}
		}
	}
	return n
}

func agentContextUse(t *Transcript, sub *Subagent, s *Session) *AgentContextUse {
	type turn struct{ usage *Usage }
	turns := map[string]*turn{}
	var order []*turn
	for _, e := range t.Entries {
		if e.Role != "assistant" {
			continue
		}
		key := fmt.Sprintf("line:%d", e.Line)
		if e.MessageID != nil {
			key = *e.MessageID
		}
		if x, ok := turns[key]; !ok {
			turns[key] = &turn{e.Usage}
			order = append(order, turns[key])
		} else if e.Usage != nil && x.usage == nil {
			x.usage = e.Usage
		}
	}
	cs := calls(t)
	rereads, files := sameFileRereads(cs)
	a := &AgentContextUse{Agent: t.Agent, AgentType: t.AgentType, Runtime: s.Runtime, Version: s.Version, Transcript: t.File, Turns: len(turns),
		Rereads: rereads, Reloads: redundantReloads(cs), OverLimit: wholeReadsOverLimit(cs), RereadFiles: files[:min(len(files), rereadFilesShown)]}
	if sub != nil {
		a.Phase, a.Change = sub.phase, sub.change
	}
	if len(t.Entries) > 0 {
		a.Version = t.Entries[0].Version
	}
	for _, e := range t.Entries {
		if e.Timestamp != "" {
			a.Start = e.Timestamp
			break
		}
	}
	for i := len(t.Entries) - 1; i >= 0; i-- {
		if t.Entries[i].Timestamp != "" {
			a.End = t.Entries[i].Timestamp
			break
		}
	}
	uses := map[string]ToolUse{}
	for _, e := range t.Entries {
		for _, u := range e.ToolUses {
			uses[u.ID] = u
		}
	}
	for _, e := range t.Entries {
		for _, r := range e.ToolResults {
			n := len(r.Shown)
			source := "other_bytes"
			if u, ok := uses[r.ID]; ok {
				source = BytesSource(u)
			}
			*a.Bytes.fields()[slices.Index(sourceNames, source)] += n
			a.Bytes.Total += n
		}
	}
	for _, x := range order {
		if x.usage == nil {
			a.TurnsWithoutUsage++
			a.TurnContext = append(a.TurnContext, 0)
			continue
		}
		u := x.usage
		context := u.Input + u.CacheCreation + u.CacheRead
		a.Input += u.Input
		a.CacheCreation += u.CacheCreation
		a.CacheRead += u.CacheRead
		a.Total += context
		a.TurnContext = append(a.TurnContext, context)
		a.Peak = math.Max(a.Peak, context)
	}
	return a
}

func groupBy(agents []*AgentContextUse, keyOf func(*AgentContextUse) groupKey) []*ContextUseGroup {
	var rows []*ContextUseGroup
	for _, a := range agents {
		key := keyOf(a)
		i := slices.IndexFunc(rows, func(r *ContextUseGroup) bool { return r.Key == key })
		if i < 0 {
			rows = append(rows, &ContextUseGroup{Key: key})
			i = len(rows) - 1
		}
		g := rows[i]
		g.Agents++
		g.Turns += a.Turns
		g.TurnsWithoutUsage += a.TurnsWithoutUsage
		g.Total += a.Total
		g.Input += a.Input
		g.CacheCreation += a.CacheCreation
		g.CacheRead += a.CacheRead
		g.Rereads += a.Rereads
		g.Reloads += a.Reloads
		g.OverLimit += a.OverLimit
		for k, f := range g.Bytes.fields() {
			*f += *a.Bytes.fields()[k]
		}
		g.PeakMax = math.Max(g.PeakMax, a.Peak)
	}
	return rows
}

// Agents that ran long: more turns, or a larger context in one turn, than these.
const (
	LongAgentTurns = 150
	LongAgentPeak  = 200_000
)

// nullable is a string as a grouping key: nil for "".
func nullable(s string) groupKey {
	if s == "" {
		return nil
	}
	return s
}

// computeContextUse is the context-use measurement of a loaded session: the
// main thread first, then each sub-agent, and their groupings. Each agent
// is measured concurrently.
func computeContextUse(s *Session) *ContextUse {
	ts := s.transcripts()
	agents := make([]*AgentContextUse, len(ts))
	parallel(len(ts), func(i int) {
		var sub *Subagent
		if i > 0 {
			sub = s.Subagents[i-1]
		}
		agents[i] = agentContextUse(ts[i], sub, s)
	})
	u := &ContextUse{Agents: agents}
	for _, a := range agents {
		if a.AgentType == main || (a.Turns <= LongAgentTurns && a.Peak <= LongAgentPeak) {
			continue
		}
		var why []string
		if a.Turns > LongAgentTurns {
			why = append(why, fmt.Sprintf("%d turns", a.Turns))
		}
		if a.Peak > LongAgentPeak {
			why = append(why, js.NumberString(math.Floor(a.Peak/1000+0.5))+"K context in one turn")
		}
		u.LongAgents = append(u.LongAgents, LongAgent{a.Agent, a.AgentType, a.Change, a.Turns, a.Peak, strings.Join(why, ", ")})
	}
	u.ByPhase = groupBy(agents, func(a *AgentContextUse) groupKey { return nullable(a.Phase) })
	u.ByChange = groupBy(agents, func(a *AgentContextUse) groupKey { return nullable(a.Change) })
	u.ByAgentType = groupBy(agents, func(a *AgentContextUse) groupKey { return a.AgentType })
	u.ByRuntime = groupBy(agents, func(a *AgentContextUse) groupKey { return runtimeVersion{a.Runtime, a.Version} })
	return u
}

// parallel runs f(0) … f(n-1) concurrently and waits; a panic in any is re-raised here.
func parallel(n int, f func(i int)) {
	var wg sync.WaitGroup
	panics := make([]any, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { panics[i] = recover() }()
			f(i)
		}()
	}
	wg.Wait()
	for _, p := range panics {
		if p != nil {
			panic(p)
		}
	}
}
