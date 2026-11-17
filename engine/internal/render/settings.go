package render

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"os"
	"path/filepath"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// ~/.claude/settings.json carries no Jhon guard: the safety layer rides
// only in each Jhon agent's frontmatter (hooksBlock), and a session that
// runs no Jhon agent is the user's. An earlier release added the guards to
// `hooks.PreToolUse`; the install removes them, keeping the user's keys and
// hooks as they are. A hook is Jhon's when its command runs a script under
// `current/hooks/`. A matcher group, `PreToolUse`, or `hooks` left empty is
// dropped, an absent file is never created, and a file that is not valid
// JSON is never rewritten.

// settingsProblem is why settings.json blocks an install, or "". A file
// that is not a JSON object is never rewritten, and `disableAllHooks: true`
// turns every hook off, the agents' frontmatter guards included.
func (e *engine) settingsProblem(prev string) string {
	if js.Trim(prev) == "" {
		return ""
	}
	doc, err := js.JSONParse(prev)
	if err != nil {
		return fmt.Sprintf("is not valid JSON (%s); nothing was written", err)
	}
	o, ok := doc.(*js.Object)
	if !ok {
		return "is not a JSON object; nothing was written"
	}
	if v, _ := js.Prop(o, "disableAllHooks").(bool); v {
		return "sets `disableAllHooks: true`, which turns every Jhon guard off; nothing was written. Remove it (or set it to false), then sync again"
	}
	return ""
}

// mergeSettings is settings.json's text without the Jhon guards, or an
// error; prev is its text ("" when absent).
func (e *engine) mergeSettings(prev string) (string, string) {
	if problem := e.settingsProblem(prev); problem != "" {
		return "", problem
	}
	if js.Trim(prev) == "" {
		return prev, ""
	}
	owned := "'" + filepath.Join(e.current(), "hooks") + "/"
	parsed, _ := js.ParseJSON(prev)
	doc := parsed.(*js.Object)
	if !doc.Has("hooks") {
		return prev, ""
	}
	hooks, ok := js.Prop(doc, "hooks").(*js.Object)
	if !ok {
		return "", "has a `hooks` that is not an object; nothing was written"
	}
	if !hooks.Has("PreToolUse") {
		return prev, ""
	}
	pre, ok := js.Prop(hooks, "PreToolUse").([]any)
	if !ok {
		return "", "has a `hooks.PreToolUse` that is not a list; nothing was written"
	}
	mine := func(h any) bool {
		c, ok := js.Prop(h, "command").(string)
		return ok && strings.HasPrefix(c, owned)
	}
	kept := []any{}
	changed := false
	for _, group := range pre {
		inner, ok := js.Prop(group, "hooks").([]any)
		if !ok {
			kept = append(kept, group)
			continue
		}
		var rest []any
		for _, h := range inner {
			if !mine(h) {
				rest = append(rest, h)
			}
		}
		if len(rest) == len(inner) {
			kept = append(kept, group)
			continue
		}
		changed = true
		if len(rest) > 0 {
			g := group.(*js.Object).Clone()
			g.Set("hooks", rest)
			kept = append(kept, g)
		}
	}
	if !changed {
		return prev, ""
	}
	nextHooks := without(hooks, "PreToolUse")
	if len(kept) > 0 {
		nextHooks.Set("PreToolUse", kept)
	}
	next := without(doc, "hooks")
	if len(nextHooks.Keys()) > 0 {
		next.Set("hooks", nextHooks)
	}
	return js.StringifyValue(next) + "\n", ""
}

// without is {...o} less key k: the rest in their order.
func without(o *js.Object, k string) *js.Object {
	out := js.NewObject()
	for _, key := range o.Keys() {
		if key != k {
			out.Set(key, js.Prop(o, key))
		}
	}
	return out
}

// installSettings removes the Jhon guards from settings.json, atomically;
// whether it changed, or the error.
func (e *engine) installSettings(check bool) (bool, string) {
	prev := readText(e.settings())
	text, err := e.mergeSettings(prev)
	if err != "" {
		return false, err
	}
	changed := text != prev
	if changed && !check {
		tmp := fmt.Sprintf("%s.jhon-%d.tmp", e.settings(), os.Getpid())
		writeText(tmp, text)
		if err := os.Rename(tmp, e.settings()); err != nil {
			panic(js.FSError(err).Error())
		}
	}
	return changed, ""
}

// ~/.claude/CLAUDE.md is the user's: an earlier release rendered a block
// between these markers, which the install removes, keeping every other
// byte. A file whose markers are unpaired, duplicated, or out of order is
// refused whole rather than repaired; one without markers is left alone,
// and an absent file is never created.
const (
	claudeMdStart = "<!-- jhon:start -->"
	claudeMdEnd   = "<!-- jhon:end -->"
)

// claudeMdBlock locates the old block in src (markers included): found
// false when it has none, or why it cannot be located.
func claudeMdBlock(src string) (from, to int, found bool, problem string) {
	starts, ends := strings.Count(src, claudeMdStart), strings.Count(src, claudeMdEnd)
	if starts == 0 && ends == 0 {
		return 0, 0, false, ""
	}
	if starts != 1 || ends != 1 {
		return 0, 0, false, fmt.Sprintf("needs exactly one %s and one %s, or neither; found %d and %d. Nothing was written: add or remove markers by hand", claudeMdStart, claudeMdEnd, starts, ends)
	}
	from = strings.Index(src, claudeMdStart)
	to = strings.Index(src, claudeMdEnd) + len(claudeMdEnd)
	if to < from+len(claudeMdStart) {
		return 0, 0, false, fmt.Sprintf("%s comes before %s. Nothing was written: reorder the markers by hand", claudeMdEnd, claudeMdStart)
	}
	return from, to, true, ""
}

var (
	trailingNewlines = lazyre.New(`\n+$`)
	leadingNewlines  = lazyre.New(`^\n+`)
)

// withoutClaudeMdBlock is CLAUDE.md without the old block; false when its
// markers do not locate it.
func withoutClaudeMdBlock(src string) (string, bool) {
	from, to, found, problem := claudeMdBlock(src)
	if problem != "" {
		return "", false
	}
	if !found {
		return src, true
	}
	before := trailingNewlines.ReplaceAllString(src[:from], "")
	after := leadingNewlines.ReplaceAllString(src[to:], "")
	switch {
	case before == "":
		return after, true
	case after != "":
		return before + "\n\n" + after, true
	}
	return before + "\n", true
}
