package manifest

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"regexp"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// The runtime capability model (docket AG). One
// behavioural rule can be enforced in one runtime, only measured in a
// second, and neither in a third. agents.yaml declares which: the
// primitives each runtime offers (AG1), the guarantee level of every
// behavioural rule in every runtime (AG2), and how each runtime takes its
// always-on contracts (`contract_delivery`).

// MatrixKeys are the agents.yaml top-level keys ParseMatrix reads; Parse
// skips them.
var MatrixKeys = []string{"runtimes", "primitives", "platforms", "runtime_primitives", "contract_delivery", "observation_route", "guarantees"}

// Matrix is the capability model's sections of agents.yaml.
type Matrix struct {
	Runtimes, Primitives, Platforms []string
	// Primitive is runtime → primitive → {capability, verification, source, version}.
	Primitive Map[Map[Map[string]]]
	// Delivery is runtime → {max_instruction_bytes, imports, source}.
	Delivery Map[Map[string]]
	// Observation is runtime → the route that observes a behavioural rule there, or `none`.
	Observation Map[string]
	// Guarantees is anchor → {by, <runtime>, <runtime>@<platform>}.
	Guarantees Map[Map[string]]
	Issues     []Issue
}

// Field is runtime's primitive's field, and whether it is declared.
func (m Matrix) Field(runtime, primitive, field string) (string, bool) {
	row, _ := m.Primitive.Get(runtime)
	v, _ := row.Get(primitive)
	return v.Get(field)
}

// HasPrimitive is whether runtime offers primitive (capability true,
// whatever its verification).
func (m Matrix) HasPrimitive(runtime, primitive string) bool {
	c, _ := m.Field(runtime, primitive, "capability")
	return c == "true"
}

// undefined stands for a value JavaScript reads as undefined, where it is
// compared or printed.
const undefined = "\x00undefined"

// at is xs[i], or undefined.
func at(xs []string, i int) string {
	if i < len(xs) {
		return xs[i]
	}
	return undefined
}

// orUndefined is v, or undefined when it is absent.
func orUndefined(v string, ok bool) string {
	if !ok {
		return undefined
	}
	return v
}

// text is a value as a template literal prints it.
func text(v string) string {
	if v == undefined {
		return "undefined"
	}
	return v
}

var (
	matrixTop  = lazyre.New(`^([\w-]+):` + js.Space + `*(` + js.Dot + `*)$`)
	matrixList = lazyre.New(`^\[(` + js.Dot + `*)\]$`)
	matrixKV   = lazyre.New(`^(` + js.Space + `+)([\w@/.-]+):` + js.Space + `*(` + js.Dot + `*)$`)
)

// ParseMatrix reads the matrix sections of agents.yaml. Every other
// top-level key is Parse's; a line this reader cannot place inside its own
// sections is an issue, never skipped.
func ParseMatrix(src string) Matrix {
	m := Matrix{Runtimes: []string{}, Primitives: []string{}, Platforms: []string{}}
	section, runtime := "", ""
	for i, raw := range strings.Split(src, "\n") {
		line := stripComment(raw)
		if js.Trim(line) == "" || strings.HasPrefix(trimStart(line), "#") {
			continue
		}
		bad := func(msg string) { m.Issues = append(m.Issues, Issue{i + 1, msg}) }
		if top := matrixTop.FindStringSubmatch(line); top != nil {
			section, runtime = top[1], ""
			if section == "runtimes" || section == "primitives" || section == "platforms" {
				list := matrixList.FindStringSubmatch(top[2])
				if list == nil {
					bad(section + " must be a list on one line")
					continue
				}
				items := splitList(list[1])
				switch section {
				case "runtimes":
					m.Runtimes = items
				case "primitives":
					m.Primitives = items
				default:
					m.Platforms = items
				}
			}
			continue
		}
		if !slices.Contains(MatrixKeys, section) {
			continue
		}
		kv := matrixKV.FindStringSubmatch(line)
		if kv == nil {
			bad(fmt.Sprintf("cannot parse %s entry: %s", section, js.Trim(line)))
			continue
		}
		indent, key, value := js.Len(kv[1]), kv[2], kv[3]
		placed, err := m.place(section, &runtime, indent, key, value)
		if err != nil {
			bad(fmt.Sprintf("cannot parse %s entry %s: %s", section, key, err))
		} else if !placed {
			bad(fmt.Sprintf("cannot parse %s entry: %s", section, js.Trim(line)))
		}
	}
	return m
}

// place stores one entry of a matrix section; false when its shape fits
// none.
func (m *Matrix) place(section string, runtime *string, indent int, key, value string) (bool, error) {
	switch {
	case section == "runtime_primitives" && indent == 2 && value == "":
		*runtime = key
		if !m.Primitive.Has(key) {
			m.Primitive.Set(key, Map[Map[string]]{})
		}
		return true, nil
	case section == "runtime_primitives" && indent == 4 && *runtime != "" && strings.HasPrefix(value, "{"):
		v, err := inlineMap(value)
		if err != nil {
			return false, err
		}
		row, _ := m.Primitive.Get(*runtime)
		row.Set(key, v)
		m.Primitive.Set(*runtime, row)
		return true, nil
	case section == "contract_delivery" && indent == 2 && strings.HasPrefix(value, "{"):
		v, err := inlineMap(value)
		if err != nil {
			return false, err
		}
		m.Delivery.Set(key, v)
		return true, nil
	case section == "observation_route" && indent == 2 && value != "":
		v, err := matrixScalar(value)
		if err != nil {
			return false, err
		}
		m.Observation.Set(key, v)
		return true, nil
	case section == "guarantees" && indent == 2 && strings.HasPrefix(value, "{"):
		v, err := inlineMap(value)
		if err != nil {
			return false, err
		}
		m.Guarantees.Set(key, v)
		return true, nil
	}
	return false, nil
}

// stripComment is a line without a trailing ` # comment`; a `#` inside
// double quotes stays.
func stripComment(line string) string {
	rs := []rune(line)
	quoted := false
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		if c == '\\' && quoted {
			i++
			continue
		}
		if c == '"' {
			quoted = !quoted
		} else if c == '#' && !quoted && (i == 0 || js.IsSpace(rs[i-1])) {
			return strings.TrimRightFunc(string(rs[:i]), js.IsSpace)
		}
	}
	return line
}

// matrixScalar is a value: JSON when double-quoted (so it may hold commas),
// else trimmed. The error is js.JSONParse's.
func matrixScalar(v string) (string, error) {
	v = js.Trim(v)
	if !strings.HasPrefix(v, `"`) {
		return v, nil
	}
	x, err := js.JSONParse(v)
	if err != nil {
		return "", err
	}
	return js.String(x), nil
}

// inlineMap is `{ k: v, k: "a, b" }` as its entries; commas inside double
// quotes do not split.
func inlineMap(s string) (Map[string], error) {
	body := strings.TrimSuffix(strings.TrimPrefix(js.Trim(s), "{"), "}")
	var parts []string
	quoted, cur := false, ""
	rs := []rune(body)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		if c == '\\' && quoted {
			i++
			next := "undefined"
			if i < len(rs) {
				next = string(rs[i])
			}
			cur += string(c) + next
			continue
		}
		if c == '"' {
			quoted = !quoted
		}
		if c == ',' && !quoted {
			parts = append(parts, cur)
			cur = ""
			continue
		}
		cur += string(c)
	}
	if js.Trim(cur) != "" {
		parts = append(parts, cur)
	}
	var out Map[string]
	for _, p := range parts {
		k, v, ok := strings.Cut(p, ":")
		if !ok {
			return out, fmt.Errorf("'%s' is not a key: value pair", js.Trim(p))
		}
		sv, err := matrixScalar(v)
		if err != nil {
			return out, err
		}
		out.Set(js.Trim(k), sv)
	}
	return out, nil
}

// Finding is a matrix problem: rule runtime-matrix or runtime-guarantee.
type Finding struct {
	Rule, Msg string
}

// Claim is what a cited evidence path must correspond to.
type Claim struct {
	Runtime, Version, Primitive string
}

// Evidence is what a cited path proves: "spike", "real-session", "" for
// nothing, or Invalid set for an incomplete record.
type Evidence struct {
	Kind    string
	Invalid string
}

// MatrixContext is what MatrixProblems compares the matrix against.
type MatrixContext struct {
	// Levels are `guarantee_levels`, in order: enforced, behavioural, unsupported.
	Levels []string
	// Verifications are `primitive_verification`.
	Verifications []string
	// Anchors are every behavioural anchor the renderer enumerates.
	Anchors []string
	// AuditRules are contracts/vocabulary.yaml `audit_rules`.
	AuditRules []map[string]string
	// Projections are each rendered runtime → the primitives its projection emits enforcement through.
	Projections Map[[]string]
	// Exists is whether a repository-relative path exists.
	Exists func(rel string) bool
	// Evidence is what a cited path proves (docket AG addendum).
	Evidence func(rel string, claim Claim) Evidence
	// RefusalPrimitives need a refusal observed: only a spike record shows one.
	RefusalPrimitives []string
}

var primitiveFields = []string{"capability", "verification", "source", "version"}

var evidencePath = lazyre.New("(?:^|[" + js.SpaceChars + "(`,;])(test/[\\w./-]*[\\w/-])")

// evidencePaths are the repository paths under test/ that a source names.
func evidencePaths(source string) []string {
	var out []string
	for _, x := range evidencePath.FindAllStringSubmatch(source, -1) {
		out = append(out, x[1])
	}
	return out
}

// MatrixProblems judges the matrix against the docket's rules.
// `runtime-matrix` holds AG1 and the delivery and observation declarations;
// `runtime-guarantee` holds AG2: every behavioural anchor has a level for
// every runtime, `enforced` rests on a verified enabling primitive (and, in
// a runtime Jhon renders, on one its projection emits), `behavioural` on an
// observation route and an audit check, and a platform exception changes
// the level it qualifies.
func MatrixProblems(m Matrix, ctx MatrixContext) []Finding {
	var out []Finding
	mx := func(msg string) { out = append(out, Finding{"runtime-matrix", msg}) }
	gu := func(msg string) { out = append(out, Finding{"runtime-guarantee", msg}) }
	enforced, behavioural := at(ctx.Levels, 0), at(ctx.Levels, 1)

	for _, issue := range m.Issues {
		mx(fmt.Sprintf("agents.yaml:%d: %s", issue.Line, issue.Msg))
	}
	for _, s := range []struct {
		key  string
		list []string
	}{{"runtimes", m.Runtimes}, {"primitives", m.Primitives}, {"platforms", m.Platforms}} {
		if len(s.list) == 0 {
			mx(s.key + " is not declared")
		}
	}

	for _, rt := range m.Runtimes {
		row, ok := m.Primitive.Get(rt)
		if !ok {
			mx(rt + ": no runtime_primitives entry")
			continue
		}
		for _, p := range m.Primitives {
			v, ok := row.Get(p)
			if !ok {
				mx(fmt.Sprintf("%s: no runtime_primitives entry for %s", rt, p))
				continue
			}
			primitiveProblems(rt, p, v, ctx, mx)
		}
		for _, p := range row.Keys() {
			if !slices.Contains(m.Primitives, p) {
				mx(fmt.Sprintf("%s: %s is not a primitive", rt, p))
			}
		}
		if d, ok := m.Delivery.Get(rt); !ok {
			mx(rt + ": no contract_delivery entry")
		} else {
			max, _ := d.Get("max_instruction_bytes")
			if !positive.MatchString(max) && max != "none" && max != "unknown" {
				mx(fmt.Sprintf("%s: max_instruction_bytes must be a byte count, none, or unknown, found '%s'", rt, max))
			}
			if imports, _ := d.Get("imports"); imports != "true" && imports != "false" {
				mx(fmt.Sprintf("%s: imports must be true or false, found '%s'", rt, imports))
			}
			if s, _ := d.Get("source"); s == "" {
				mx(rt + ": contract_delivery has no source")
			}
		}
		if o, _ := m.Observation.Get(rt); o == "" {
			mx(rt + ": no observation_route entry")
		}
	}
	for _, s := range []struct {
		section string
		keys    []string
	}{{"runtime_primitives", m.Primitive.Keys()}, {"contract_delivery", m.Delivery.Keys()}, {"observation_route", m.Observation.Keys()}} {
		for _, rt := range s.keys {
			if !slices.Contains(m.Runtimes, rt) {
				mx(fmt.Sprintf("%s: %s is not a runtime", s.section, rt))
			}
		}
	}

	// anchor → its entry's check, undefined when the entry names none.
	checkOf := map[string]string{}
	for _, r := range ctx.AuditRules {
		anchor, ok := r["anchor"]
		check, has := r["check"]
		if has {
			check = strings.Join(ruleChecks(check), ", ")
		}
		checkOf[orUndefined(anchor, ok)] = orUndefined(check, has)
	}
	unmet := func(anchor string, entry Map[string], rt, level string) string {
		switch level {
		case enforced:
			by, _ := entry.Get("by")
			if by == "" {
				return rt + " is enforced but the entry names no enabling primitive (by)"
			}
			row, _ := m.Primitive.Get(rt)
			v, ok := row.Get(by)
			if !ok {
				return "" // `by` itself is reported once, below
			}
			if c, _ := v.Get("capability"); c != "true" {
				return fmt.Sprintf("%s is enforced but %s has no %s", rt, rt, by)
			}
			if ver, ok := v.Get("verification"); ver != "verified" {
				return fmt.Sprintf("%s is enforced but %s is %s there, not verified", rt, by, text(orUndefined(ver, ok)))
			}
			emits, rendered := ctx.Projections.Get(rt)
			if !rendered {
				return fmt.Sprintf("%s is enforced, but %s is not rendered, so no Jhon mechanism runs there", rt, rt)
			}
			if !slices.Contains(emits, by) {
				return fmt.Sprintf("%s is enforced through %s, which the %s render does not project", rt, by, rt)
			}
		case behavioural:
			if o, ok := m.Observation.Get(rt); !ok || o == "none" {
				return rt + " is behavioural but " + rt + " has no observation route; without one it is unsupported"
			}
			if c, ok := checkOf[anchor]; !ok || c == undefined || c == "none" {
				return rt + " is behavioural but its audit_rules entry has check none; without a measurement it is unsupported"
			}
		}
		return ""
	}

	for _, anchor := range ctx.Anchors {
		if !m.Guarantees.Has(anchor) {
			gu(anchor + " has no guarantees entry")
		}
	}
	for _, anchor := range m.Guarantees.Keys() {
		entry, _ := m.Guarantees.Get(anchor)
		if !slices.Contains(ctx.Anchors, anchor) {
			gu(anchor + " is not a behavioural anchor the renderer enumerates")
			continue
		}
		at := func(msg string) { gu(anchor + ": " + msg) }
		by, _ := entry.Get("by")
		if by != "" && !slices.Contains(m.Primitives, by) {
			at("by: " + by + " is not a primitive")
		}
		for _, rt := range m.Runtimes {
			if !entry.Has(rt) {
				at("no level for " + rt)
			}
		}
		for _, key := range entry.Keys() {
			if key == "by" {
				continue
			}
			level, _ := entry.Get(key)
			parts := strings.Split(key, "@")
			rt := parts[0]
			if !slices.Contains(m.Runtimes, rt) {
				at(rt + " is not a runtime")
				continue
			}
			if !slices.Contains(ctx.Levels, level) {
				at(fmt.Sprintf("%s: level must be one of %s, found '%s'", key, strings.Join(ctx.Levels, "|"), level))
				continue
			}
			if len(parts) > 1 {
				platform := parts[1]
				if !slices.Contains(m.Platforms, platform) {
					at(fmt.Sprintf("%s: %s is not a platform", key, platform))
					continue
				}
				if own, ok := entry.Get(rt); ok && own == level {
					at(fmt.Sprintf("%s: the level is the same as %s's; an exception exists only where the level changes", key, rt))
					continue
				}
			}
			if why := unmet(anchor, entry, rt, level); why != "" {
				if len(parts) > 1 {
					why = key + ": " + why
				}
				at(why)
			}
		}
		if by != "" && !slices.ContainsFunc(entry.Keys(), func(k string) bool { l, _ := entry.Get(k); return k != "by" && l == enforced }) {
			at("by: " + by + " is named, but no runtime enforces the rule")
		}
	}
	return out
}

var positive = lazyre.New(`^[1-9]\d*$`)

// primitiveProblems are the AG1 problems of runtime rt's primitive p.
func primitiveProblems(rt, p string, v Map[string], ctx MatrixContext, mx func(string)) {
	at := rt + "." + p
	for _, k := range v.Keys() {
		if !slices.Contains(primitiveFields, k) {
			mx(fmt.Sprintf("%s: unknown field '%s'", at, k))
		}
	}
	capability, _ := v.Get("capability")
	if capability != "true" && capability != "false" {
		mx(fmt.Sprintf("%s: capability must be true or false, found '%s'", at, capability))
	}
	verification, _ := v.Get("verification")
	if !slices.Contains(ctx.Verifications, verification) {
		mx(fmt.Sprintf("%s: verification must be one of %s, found '%s'", at, strings.Join(ctx.Verifications, "|"), verification))
	}
	source, _ := v.Get("source")
	version, _ := v.Get("version")
	if source == "" {
		mx(at + ": has no source")
	}
	if version == "" {
		mx(at + ": has no version")
	}
	paths := evidencePaths(source)
	for _, path := range paths {
		if !ctx.Exists(path) {
			mx(fmt.Sprintf("%s: %s does not exist", at, path))
		}
	}
	if verification != "verified" {
		return
	}
	// Judged by what was executed and observed, not by file kind: a
	// complete spike record, or a real-session fixture for a primitive
	// whose verification needs no refusal. A unit test shows Jhon's side.
	refusal := slices.Contains(ctx.RefusalPrimitives, p)
	var kinds []string
	for _, path := range paths {
		if !ctx.Exists(path) {
			continue
		}
		k := ctx.Evidence(path, Claim{rt, version, p})
		if k.Invalid != "" {
			mx(fmt.Sprintf("%s: %s: %s", at, path, k.Invalid))
		}
		kinds = append(kinds, k.Kind)
	}
	if slices.Contains(kinds, "spike") {
		return
	}
	if refusal && slices.Contains(kinds, "real-session") {
		mx(fmt.Sprintf("%s: %s needs a spike record: a real-session fixture does not show a refusal honoured", at, p))
	} else if refusal || !slices.Contains(kinds, "real-session") {
		alt := ""
		if !refusal {
			alt = " or a real-session fixture (test/testdata/audit/<name>, source.json real)"
		}
		mx(fmt.Sprintf("%s: verified needs a spike record (test/testdata/spikes/<name>/record.json)%s; its source cites none", at, alt))
	}
}

var byteCount = lazyre.New(`^\d+$`)

// BudgetProblem is why the always-on bundle of bytes cannot be delivered to
// runtime; "" when it can. Only a runtime without imports receives the
// bundle inline; `unknown` cannot be checked and is the caller's to report.
func BudgetProblem(m Matrix, runtime string, bytes int) string {
	d, ok := m.Delivery.Get(runtime)
	imports, _ := d.Get("imports")
	maxText, _ := d.Get("max_instruction_bytes")
	if !ok || imports != "false" || !byteCount.MatchString(maxText) {
		return ""
	}
	max := js.Number(maxText)
	if float64(bytes) > max {
		return fmt.Sprintf("the always-on bundle is %d bytes and %s takes at most %s bytes, inline (no imports)", bytes, runtime, js.NumberString(max))
	}
	return ""
}

// ReportOptions are what Report measures a runtime with.
type ReportOptions struct {
	AuditRules []map[string]string
	Levels     []string
	// BundleBytes is the size of the always-on bundle as this runtime would receive it inline.
	BundleBytes int
	// Problems are MatrixProblems of the whole matrix; the report shows those naming this runtime.
	Problems []Finding
}

// ruleChecks are the checks an audit_rules entry's `check` names: one, or
// an inline list of the forms that read one rule (render's validate).
func ruleChecks(check string) []string {
	if l := contract.InlineList(check); l != nil {
		return l
	}
	return []string{check}
}

// ruleStatus is the validation status jhon audit gives a finding of rule on
// runtime (internal/audit's emit): pending-real-fixture where the entry's
// `pending_on` names the runtime, else its validation_status.
func ruleStatus(rule map[string]string, runtime string) string {
	if slices.Contains(contract.InlineList(rule["pending_on"]), runtime) {
		return "pending-real-fixture"
	}
	return rule["validation_status"]
}

// Report is `jhon doctor --runtime <name>` (AG3): guarantee coverage and
// primitive verification, never a score; failed when a check fails.
func Report(m Matrix, runtime string, o ReportOptions) (string, bool) {
	enforced, behavioural := at(o.Levels, 0), at(o.Levels, 1)
	lines := []string{"runtime " + runtime, ""}
	failed := false
	fail := func(msg string) { failed = true; lines = append(lines, "  FAIL  "+msg) }
	names := regexp.MustCompile(`(^|[` + js.SpaceChars + `:(])` + regexp.QuoteMeta(runtime) + `([` + js.SpaceChars + `.:@)]|$)`)
	var mine []Finding
	for _, p := range o.Problems {
		if names.MatchString(p.Msg) {
			mine = append(mine, p)
		}
	}
	ruleOf := map[string]map[string]string{}
	for _, r := range o.AuditRules {
		anchor, ok := r["anchor"]
		ruleOf[orUndefined(anchor, ok)] = r
	}

	type row struct{ anchor, level, detail string }
	var rows []row
	width := 0
	for _, anchor := range m.Guarantees.Keys() {
		entry, _ := m.Guarantees.Get(anchor)
		level, ok := entry.Get(runtime)
		if !ok {
			level = "(none)"
		}
		var detail []string
		switch level {
		case enforced:
			by, ok := entry.Get("by")
			if !ok {
				by = "(no primitive)"
			}
			detail = append(detail, "by "+by)
		case behavioural:
			r, has := ruleOf[anchor]
			check, ok := r["check"]
			if !has || !ok {
				check = "none"
			}
			check = strings.Join(ruleChecks(check), ", ")
			if vs := ruleStatus(r, runtime); vs != "" {
				check += ", " + vs
			}
			obs, ok := m.Observation.Get(runtime)
			detail = append(detail, fmt.Sprintf("%s (%s)", text(orUndefined(obs, ok)), check))
		}
		for _, k := range entry.Keys() {
			if strings.HasPrefix(k, runtime+"@") {
				l, _ := entry.Get(k)
				detail = append(detail, k[len(runtime)+1:]+": "+l)
			}
		}
		rows = append(rows, row{anchor, level, strings.Join(detail, "; ")})
		width = max(width, js.Len(anchor))
	}
	lines = append(lines, "guarantees (one level per rule)")
	for _, level := range o.Levels {
		n := 0
		for _, r := range rows {
			if r.level == level {
				n++
			}
		}
		lines = append(lines, fmt.Sprintf("  %s %d rule(s)", js.PadEnd(level, 12), n))
	}
	for _, level := range o.Levels {
		for _, r := range rows {
			if r.level == level {
				lines = append(lines, fmt.Sprintf("  %s %s", js.PadEnd(level, 12), strings.TrimRightFunc(js.PadEnd(r.anchor, width)+"  "+r.detail, js.IsSpace)))
			}
		}
	}

	lines = append(lines, "", "primitives (verification, capability, version, source)")
	pwidth := 0
	for _, p := range m.Primitives {
		pwidth = max(pwidth, js.Len(p))
	}
	for _, p := range m.Primitives {
		row, _ := m.Primitive.Get(runtime)
		v, ok := row.Get(p)
		if !ok {
			lines = append(lines, fmt.Sprintf("  %s %s", js.PadEnd("missing", 11), p))
			continue
		}
		get := func(k string) string { s, ok := v.Get(k); return text(orUndefined(s, ok)) }
		yes := "no "
		if c, _ := v.Get("capability"); c == "true" {
			yes = "yes"
		}
		lines = append(lines, fmt.Sprintf("  %s %s  %s  %s  %s", js.PadEnd(get("verification"), 11), js.PadEnd(p, pwidth), yes, get("version"), get("source")))
	}

	lines = append(lines, "", "contract delivery")
	d, ok := m.Delivery.Get(runtime)
	if imports, _ := d.Get("imports"); ok && imports == "true" {
		lines = append(lines, "  ok    imports: the runtime takes imports, so no instruction budget binds the always-on bundle")
	} else if ok {
		maxText, _ := d.Get("max_instruction_bytes")
		if why := BudgetProblem(m, runtime, o.BundleBytes); why != "" {
			fail(why)
		} else if maxText == "unknown" {
			lines = append(lines, fmt.Sprintf("  warn  the always-on bundle is %d bytes; %s's instruction budget is unknown, so whether it fits is unchecked", o.BundleBytes, runtime))
		} else if maxText == "none" {
			lines = append(lines, fmt.Sprintf("  ok    the always-on bundle is %d bytes, inline; %s documents no cap", o.BundleBytes, runtime))
		} else {
			mt, has := d.Get("max_instruction_bytes")
			lines = append(lines, fmt.Sprintf("  ok    the always-on bundle is %d bytes, within %s", o.BundleBytes, text(orUndefined(mt, has))))
		}
	}

	if len(mine) > 0 {
		lines = append(lines, "", "matrix problems")
		for _, p := range mine {
			fail(p.Rule + ": " + p.Msg)
		}
	}
	return strings.Join(lines, "\n") + "\n", failed
}

// Declaration is the runtime guarantees the render delivers with the
// always-on contracts of runtime (AG4, amended by docket AR): the count of
// rules at each level, unsupported ones included, the primitives the
// runtime lacks, and a pointer to `jhon doctor --runtime`.
func Declaration(m Matrix, runtime string, levels []string) string {
	enforced, behavioural, unsupported := at(levels, 0), at(levels, 1), at(levels, 2)
	count := func(level string) int {
		n := 0
		for _, anchor := range m.Guarantees.Keys() {
			e, _ := m.Guarantees.Get(anchor)
			if l, ok := e.Get(runtime); orUndefined(l, ok) == level {
				n++
			}
		}
		return n
	}
	obs, ok := m.Observation.Get(runtime)
	lines := []string{
		fmt.Sprintf("Runtime guarantees in %s (agents.yaml `guarantees`): %d %s, ", runtime, count(enforced), text(enforced)) +
			fmt.Sprintf("%d %s (measured by %s), %d %s.", count(behavioural), text(behavioural), text(orUndefined(obs, ok)), count(unsupported), text(unsupported)),
		"An unsupported rule is neither enforced nor measured here; it binds by instruction alone. Every rule binds",
		fmt.Sprintf("whatever its level. `jhon doctor --runtime %s` lists every rule with its level and what holds or measures it.", runtime),
	}
	var absent []string
	for _, p := range m.Primitives {
		if c, _ := m.Field(runtime, p, "capability"); c == "false" {
			absent = append(absent, p)
		}
	}
	if len(absent) > 0 {
		lines = append(lines, "Absent here: "+strings.Join(absent, ", ")+".")
	}
	return strings.Join(lines, "\n")
}
