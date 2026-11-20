package ldd

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"io/fs"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	"github.com/jachavarro2004/jhon-agent/engine/internal/isodate"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/vcs"
)

// This file is `jhon context [<change>] --phase <phase|none> [--json]`
// (docket Y).
//
// It prints a launch context an orchestrator may paste into a delegation
// (contracts/ldd.md section H): an optional, read-only helper,
// never a requirement. `jhon audit` reads a pasted `jhon-context` block's
// `change` and `project_root` to attribute the delegation.
//
// Read-only, and a view of `jhon status`, never a second derivation: `vcs`
// and every phase-specific field are copied from StatusOf as it reports
// them. Only `head`, the full sha, is read here, with the same read-only
// `git rev-parse` status uses. The phase-specific fields come from
// `phase_context_fields` in contracts/vocabulary.yaml.
//
// `project_name` is not part of it: Engram owns project identity, and the
// orchestrator adds that field from `mem_current_project` outside the block.
//
// `--handoff` (plan item 7) adds what a child session, or a session after
// /clear, needs beyond that: `risk`, the change's tier from `jhon risk` (its
// non-passive reasons), and `decisions`, the ids, types, and titles of the
// decisions and architecture Engram holds for the change and the areas its
// diff touches (`engram search`, or JHON_ENGRAM). Engram unavailable leaves
// `decisions` null with a `decisions_note` that names why (T163): not found,
// its exit code and first stderr line, a timeout, or too much output; the
// packet still prints. An Engram that finds nothing is no failure.
//
// `change` is the name given, or nil; it is never selected on the context's
// behalf, and it need not exist yet (explore runs before its directory
// does).

// NoPhase is the phase of a delegation that is not an LDD phase: a general worker, a judge, a facet.
const NoPhase = "none"

// ContextKeys are the keys every context carries, in output order.
var ContextKeys = []string{"context_id", "resolved_at", "head", "project_root", "change", "phase", "vcs"}

type contextVocabulary struct {
	phases     []string
	fields     map[string][]string
	fieldOrder []string
}

var contextVocab = sync.OnceValue(func() contextVocabulary {
	sv() // the status vocabulary loads first
	src := contract.MustContract("vocabulary.yaml")
	phases := slices.Concat(contract.List(src, "phases"), contract.List(src, "project_phases"), []string{NoPhase})
	keys, values := contract.Map(src, "phase_context_fields")
	fields := map[string][]string{}
	for _, p := range keys {
		fields[p] = contract.InlineList(values[p])
		// A declaration that names a phase no command takes would never be printed.
		if !slices.Contains(phases, p) || p == NoPhase {
			panic(fmt.Errorf("contracts/vocabulary.yaml: phase_context_fields names unknown phase '%s'", p))
		}
	}
	return contextVocabulary{phases, fields, keys}
})

// ContextPhases are every `--phase` value: the LDD phases, the project phases, and none.
func ContextPhases() []string { return contextVocab().phases }

// PhaseContextFields is `phase_context_fields`: what a phase receives beyond the base shape.
func PhaseContextFields() map[string][]string { return contextVocab().fields }

// A Field is one key a context carries beyond the base shape.
type Field struct {
	Key   string
	Value any
}

// Context is a launch context: the base shape, then Fields in order.
type Context struct {
	ContextID   string
	ResolvedAt  string
	Head        *string
	ProjectRoot string
	Change      *string
	Phase       string
	Vcs         vcs.Vcs
	Fields      []Field
}

func (c Context) object() object {
	o := object{
		{"context_id", c.ContextID}, {"resolved_at", c.ResolvedAt}, {"head", c.Head}, {"project_root", c.ProjectRoot},
		{"change", c.Change}, {"phase", c.Phase}, {"vcs", c.Vcs},
	}
	for _, f := range c.Fields {
		o = o.set(f.Key, f.Value)
	}
	return o
}

// MarshalJSON writes the context's keys in order; a field named like a base
// key replaces it in place, as assigning the property does.
func (c Context) MarshalJSON() ([]byte, error) { return c.object().MarshalJSON() }

// uuid is crypto.randomUUID(): a random version 4 UUID.
func uuid() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ResolveContext is the launch context of root for phase, resolved now. The
// error is an unknown phase, a phase field status does not report, or a
// status that cannot be read.
func ResolveContext(root string, change *string, phase string) (Context, error) {
	c, _, err := resolveContext(root, change, phase)
	return c, err
}

// resolveContext is ResolveContext and the status it was read from.
func resolveContext(root string, change *string, phase string) (Context, Status, error) {
	v := contextVocab()
	if !slices.Contains(v.phases, phase) {
		return Context{}, Status{}, fmt.Errorf("unknown phase '%s'; one of %s", phase, strings.Join(v.phases, ", "))
	}
	projectRoot := project.Resolve("", root)
	s, err := StatusOf(projectRoot, change)
	if err != nil {
		return Context{}, Status{}, err
	}
	c := Context{
		ContextID: uuid(), ResolvedAt: isodate.Format(time.Now()), ProjectRoot: projectRoot, Change: change, Phase: phase,
		Vcs: vcs.Vcs{State: s.Vcs.State, Reason: s.Vcs.Reason},
	}
	if head := vcs.HeadSha(projectRoot); head != "" {
		c.Head = &head
	}
	reported := s.object()
	for _, f := range v.fields[phase] {
		value, ok := reported.get(f)
		// A field status does not report would print a context that looks complete.
		if !ok {
			return Context{}, Status{}, fmt.Errorf("contracts/vocabulary.yaml: phase_context_fields.%s names '%s', which jhon status does not report", phase, f)
		}
		c.Fields = append(c.Fields, Field{f, value})
	}
	return c, s, nil
}

// LaunchHandoff is the handoff packet of a launch in cwd (T134, L1036 K1):
// `jhon context [<change>] --phase none --handoff` at cwd's project root
// (ContextRoot), read in this process, for the change status selects there
// when it selects one, else for none. Phase none adds no field, so the
// change status selected is the one `jhon context <change>` would resolve.
// Its Engram searches end with ctx.
func LaunchHandoff(ctx context.Context, cwd string) (Context, error) {
	c, s, err := resolveContext(ContextRoot(cwd), nil, "none")
	if err != nil {
		return Context{}, err
	}
	if s.Change != nil {
		c.Change = new(*s.Change)
	}
	return handoffContext(ctx, c)
}

// generic are path segments too generic to name an area of a codebase.
var generic = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`src lib app apps backend frontend core packages pkg internal cmd services service
		modules module test tests __tests__ spec docs doc scripts main java com org domain application
		infrastructure unit integration e2e components utils common shared api web`) {
		generic[w] = true
	}
}

// AreasOf is the areas a diff touches: its files' distinct, non-generic
// directory names, lowercased, at most eight.
func AreasOf(reasons []Reason) []string {
	var out []string
	for _, r := range reasons {
		segs := strings.Split(r.File, "/")
		for _, seg := range segs[:len(segs)-1] {
			w := js.Lower(seg)
			if w == "" || strings.HasPrefix(w, ".") || generic[w] || slices.Contains(out, w) {
				continue
			}
			out = append(out, w)
		}
	}
	return out[:min(len(out), 8)]
}

// Decision is one decision or architecture record Engram holds.
type Decision struct {
	ID    float64 `json:"id"`
	Type  string  `json:"type"`
	Title string  `json:"title"`
}

var (
	engramRow = lazyre.New(`^\[\d+\] #(\d+) \(([\w-]+)\) — (` + js.Dot + `+)$`)
	// lineBreak is what ends a line for JavaScript's m flag.
	lineBreak = lazyre.New("[\n\r  ]")
)

// engramTimeout is the budget of DecisionsFor's two searches together, and
// engramMaxBuffer spawnSync's default maxBuffer.
const (
	engramTimeout   = 15 * time.Second
	engramMaxBuffer = 1024 * 1024
)

// DecisionsFor is the decisions and architecture Engram holds for query;
// ok is false when Engram cannot be asked.
func DecisionsFor(root, query string) (decisions []Decision, ok bool) {
	d, why := decisionsWithin(context.Background(), root, query)
	return d, why == ""
}

// decisionsWithin is DecisionsFor, ended by parent too; why is the reason
// Engram could not be asked, or "".
func decisionsWithin(parent context.Context, root, query string) (decisions []Decision, why string) {
	bin := os.Getenv("JHON_ENGRAM")
	if bin == "" {
		bin = "engram"
	}
	ctx, cancel := context.WithTimeout(parent, engramTimeout)
	defer cancel()
	return decisionsWhy(ctx, bin, root, query)
}

// decisionsFor is DecisionsFor through bin, both searches within ctx: eight
// results of each of the two types at most.
func decisionsFor(ctx context.Context, bin, root, query string) (decisions []Decision, ok bool) {
	d, why := decisionsWhy(ctx, bin, root, query)
	return d, why == ""
}

// engramStderrMax is how much of a failed search's stderr is kept, and
// engramWhyMax the length of the stderr line a reason quotes.
const (
	engramStderrMax = 64 * 1024
	engramWhyMax    = 200
)

// capped keeps the first n bytes written to it and accepts the rest.
type capped struct {
	bytes.Buffer
	n int
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.n - c.Len(); room > 0 {
		c.Buffer.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// decisionsWhy is decisionsFor with the reason Engram could not be asked
// (T163): "" when it answered.
func decisionsWhy(ctx context.Context, bin, root, query string) (decisions []Decision, why string) {
	out := []Decision{}
	for _, kind := range []string{"decision", "architecture"} {
		cmd := exec.CommandContext(ctx, bin, "search", query, "--type", kind, "--limit", "8", "--match", "any")
		cmd.Dir = root
		cmd.WaitDelay = time.Second
		var stdout bytes.Buffer
		stderr := &capped{n: engramStderrMax}
		cmd.Stdout, cmd.Stderr = &stdout, stderr
		if err := cmd.Run(); err != nil {
			return nil, engramFailure(ctx, err, stderr.String())
		}
		if stdout.Len() > engramMaxBuffer {
			return nil, "engram output over 1 MiB"
		}
		for _, line := range lineBreak.Split(stdout.String(), -1) {
			m := engramRow.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			id := js.Number(m[1])
			if !slices.ContainsFunc(out, func(d Decision) bool { return d.ID == id }) {
				out = append(out, Decision{id, m[2], js.Trim(m[3])})
			}
		}
	}
	return out, ""
}

// engramFailure is why a search that failed with err, having written
// stderr, could not ask Engram.
func engramFailure(ctx context.Context, err error, stderr string) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Sprintf("engram timed out after %s", engramTimeout)
	}
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		why := fmt.Sprintf("engram exited %d", exit.ExitCode())
		for _, line := range lineBreak.Split(stderr, -1) {
			if line = strings.TrimSpace(line); line != "" {
				if r := []rune(line); len(r) > engramWhyMax {
					line = string(r[:engramWhyMax])
				}
				return why + ": " + line
			}
		}
		return why
	}
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return "engram not found"
	}
	return "engram could not run: " + err.Error()
}

// HandoffContext is the handoff packet: the context plus the change's risk
// and the decisions recorded for it. The error is a risk_rules config that
// cannot be read.
func HandoffContext(c Context) (Context, error) { return handoffContext(context.Background(), c) }

// handoffContext is HandoffContext, its Engram searches ended by ctx too.
func handoffContext(ctx context.Context, c Context) (Context, error) {
	risk, err := RiskOf(c.ProjectRoot, nil)
	if err != nil {
		return Context{}, err
	}
	// The search names the change, the areas its diff touches, and the
	// symbol and file names in it (H5), so a decision saved with its code's
	// refs is found from that code.
	var change string
	if c.Change != nil {
		change = *c.Change
	}
	var files []string
	for _, r := range risk.Reasons {
		files = append(files, r.File)
	}
	var names []string
	if base, _, ok := ChangeBase(c.ProjectRoot); ok {
		names = CodeNames(c.ProjectRoot, []string{base})
	}
	query := handoffQuery(change, files, names)
	reasons := []Reason{}
	for _, r := range risk.Reasons {
		if r.Level != Passive && len(reasons) < 20 {
			reasons = append(reasons, r)
		}
	}
	out := c.with(Field{"risk", Risk{Tier: risk.Tier, Base: risk.Base, Reasons: reasons}})
	decisions, why := []Decision{}, ""
	if query != "" {
		decisions, why = decisionsWithin(ctx, c.ProjectRoot, query)
	}
	if why != "" {
		return out.with(Field{"decisions", nil}, Field{"decisions_note", "engram unavailable (" + why + "): search Engram for the change's decisions before handing off"}), nil
	}
	return out.with(Field{"decisions", decisions}), nil
}

// with is c with fields added, sharing nothing with c.
func (c Context) with(fields ...Field) Context {
	c.Fields = append(slices.Clone(c.Fields), fields...)
	return c
}

// ContextRoot is the project root the orchestrator names: the git top level, or the working directory.
func ContextRoot(cwd string) string {
	if top := vcs.GitTopLevel(cwd); top != "" {
		return top
	}
	return project.Resolve("", cwd)
}
