package ldd

import (
	"errors"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	"github.com/jachavarro2004/jhon-agent/engine/internal/discover"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// This file is the one resolution of Strict TDD.
//
// contracts/tdd.md, "Precedence": without openspec/config.yaml the
// contract's own mode governs. With it, the human's `strict_tdd_policy`
// governs (require or forbid); without a policy, a legacy `strict_tdd` key
// keeps its old meaning, and without either the value is derived from the
// project's live capability on every call and never written anywhere
// (docket J). Policy is intent and capability is fact; neither stands in
// for the other.
//
// apply and verify read this resolution from `jhon status <change>
// --json`, so both phases see exactly the same answer.
//
// Capability is what backs a `true` (docket M4). It is read from the
// resolved projects (discover.ResolveProjects: live discovery with the
// config's `test_command_override` applied): true iff at least one project
// is `testable` and none is `unknown`. `structurally_exempt` projects need
// nothing. A legacy `strict_tdd_evidence` block is never read for
// capability: when present it is only noted, so a human reconciles it into
// overrides. A required true without capability is `invalid`, and status
// notes it (it never blocks); a derived value without it is false.
//
// Every value (sources, policy values, evidence states, config keys, note
// templates) comes from contracts/vocabulary.yaml, "Strict TDD resolution",
// by position or by name, read on first use.

type tddVocabulary struct {
	fromPolicy, derived, fromLegacy, fromContract string
	ok, invalid, notApplicable                    string
	require, forbid                               string
	keys, notes                                   map[string]string
}

var tddVocab = sync.OnceValue(func() tddVocabulary {
	source := contract.MustVocabulary("strict_tdd_source", 4)
	evidence := contract.MustVocabulary("strict_tdd_evidence_state", 3)
	policy := contract.MustVocabulary("strict_tdd_policy", 2)
	src := contract.MustContract("vocabulary.yaml")
	_, keys := contract.Map(src, "strict_tdd_config")
	for _, k := range []string{"file", "policy", "mode", "mode_parent", "evidence", "override"} {
		if keys[k] == "" {
			panic(errors.New("contracts/vocabulary.yaml: strict_tdd_config must name file, policy, mode, mode_parent, evidence, and override"))
		}
	}
	_, notes := contract.Map(src, "strict_tdd_notes")
	for _, k := range []string{"legacy", "legacy_ignored", "legacy_evidence"} {
		if notes[k] == "" {
			panic(errors.New("contracts/vocabulary.yaml: strict_tdd_notes must declare legacy, legacy_ignored, and legacy_evidence"))
		}
	}
	return tddVocabulary{
		source[0], source[1], source[2], source[3],
		evidence[0], evidence[1], evidence[2],
		policy[0], policy[1],
		keys, notes,
	}
})

func tddNote(template, keys string) string {
	k := tddVocab().keys
	s := strings.ReplaceAll(template, "{file}", k["file"])
	s = strings.ReplaceAll(s, "{policy}", k["policy"])
	s = strings.ReplaceAll(s, "{keys}", keys)
	s = strings.ReplaceAll(s, "{evidence}", k["evidence"])
	return strings.ReplaceAll(s, "{override}", k["override"])
}

// StrictTdd is the resolution. Notes are non-blocking: status appends them
// to its own notes. They say when a legacy key is in play, so a human
// reconciles it with /jhon-ldd init; the resolution never rewrites it.
type StrictTdd struct {
	Value    bool     `json:"value"`
	Source   string   `json:"source"`
	Evidence string   `json:"evidence"`
	Reason   string   `json:"reason"`
	Notes    []string `json:"notes"`
}

var modeLine = lazyre.New(`^\*\*Mode: (enabled|disabled)\.\*\*` + js.Space + `*$`)

// TddContractMode is the mode contracts/tdd.md (its text, src) states on its
// `**Mode: enabled.**` line.
func TddContractMode(src string) (bool, error) {
	// A line ends at any line terminator: \n, \r, U+2028, or U+2029.
	for _, line := range strings.FieldsFunc(src, func(r rune) bool { return r == '\n' || r == '\r' || r == 0x2028 || r == 0x2029 }) {
		if m := modeLine.FindStringSubmatch(line); m != nil {
			return m[1] == "enabled", nil
		}
	}
	return false, errors.New("contracts/tdd.md: no '**Mode: enabled.**' or '**Mode: disabled.**' line")
}

// unquoteScalar is a trimmed value without one pair of surrounding quotes.
func unquoteScalar(s string) string {
	s = js.Trim(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] && !strings.ContainsAny(s[1:len(s)-1], "\r  ") {
		return s[1 : len(s)-1]
	}
	return s
}

var (
	trailingComment = lazyre.New(`(?:^|[ \t])#` + js.Dot + `*$`)
	commentOnly     = lazyre.New(`^` + js.Space + `*#`)
	crlf            = lazyre.New(`\r?\n`)
)

func stripComment(raw string) string { return js.Trim(trailingComment.ReplaceAllString(raw, "")) }

// scalarBool is a scalar's literal true or false; nil for anything else.
func scalarBool(raw string) *bool {
	switch stripComment(raw) {
	case "true":
		return new(true)
	case "false":
		return new(false)
	}
	return nil
}

func keyLine(k string) *regexp.Regexp {
	return regexp.MustCompile(`^` + regexp.QuoteMeta(k) + `:[ \t]*(` + js.Dot + `*)$`)
}

// configModes is the mode as the config states it, in its two accepted
// places: the top-level key, and a direct child of the top-level parent
// mapping (`testing:` → `strict_tdd:`, the form earlier `jhon init` releases
// wrote). Each is its literal true or false, or nil when absent or anything
// else. A line scan, not a YAML parser, so it only trusts structure it can
// see: comment lines, the content of block scalars (`key: |`, `key: >-`),
// and every other mapping are skipped, and a nested key counts only at the
// parent's first child indentation.
func configModes(config string) (top, nested *bool) {
	k := tddVocab().keys
	mode, parent := keyLine(k["mode"]), keyLine(k["mode_parent"])
	inParent := false
	childIndent := -1
	for _, line := range crlf.Split(config, -1) {
		if js.Trim(line) == "" || commentOnly.MatchString(line) {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if indent == 0 {
			// A new top-level entry closes whatever mapping or block scalar came before.
			inParent = false
			if t := mode.FindStringSubmatch(line); t != nil && top == nil {
				top = scalarBool(t[1])
			}
			if p := parent.FindStringSubmatch(line); p != nil && stripComment(p[1]) == "" {
				inParent, childIndent = true, -1
			}
			continue
		}
		if !inParent {
			continue
		}
		if childIndent < 0 {
			childIndent = indent
		}
		if indent != childIndent {
			continue
		}
		if n := mode.FindStringSubmatch(line[indent:]); n != nil && nested == nil {
			nested = scalarBool(n[1])
		}
	}
	return top, nested
}

// configPolicy is the policy as the config states it: the top-level key's
// raw scalar, comment stripped and unquoted, or nil when the key is absent.
// Only the top level counts; the policy has no legacy nested form.
func configPolicy(config string) *string {
	re := keyLine(tddVocab().keys["policy"])
	for _, line := range crlf.Split(config, -1) {
		if m := re.FindStringSubmatch(line); m != nil {
			return new(unquoteScalar(stripComment(m[1])))
		}
	}
	return nil
}

// capability is whether the project can run Strict TDD right now, read live
// from the resolved projects and never stored: at least one `testable`
// project and no `unknown` one. The reason says why either way, naming the
// projects.
func capability(config string, d *discover.Discovery) (able bool, reason string) {
	projects := discover.ResolveProjects(d, config).Projects
	named := func(state string) []string {
		var out []string
		for _, p := range projects {
			if p.Testability == state {
				out = append(out, p.Path)
			}
		}
		return out
	}
	testable, exempt, unknown := named(discover.Testable()), named(discover.Exempt()), named(discover.Unknown())
	exemption := ""
	if len(exempt) > 0 {
		exemption = fmt.Sprintf("; %s: %s", discover.Exempt(), strings.Join(exempt, ", "))
	}
	if len(unknown) > 0 {
		return false, fmt.Sprintf("%s: %s; each needs a %s stated through /jhon-ldd init%s", discover.Unknown(), strings.Join(unknown, ", "), tddVocab().keys["override"], exemption)
	}
	if len(testable) == 0 {
		return false, fmt.Sprintf("no %s project among %d discovered%s", discover.Testable(), len(projects), exemption)
	}
	return true, fmt.Sprintf("every project resolves: %s: %s%s", discover.Testable(), strings.Join(testable, ", "), exemption)
}

// required is a required true: ok when capability backs it, otherwise
// still true with invalid evidence. A human decision is never silently
// relaxed; status notes it until capability catches up.
func required(source, config string, d *discover.Discovery, why string, notes []string) StrictTdd {
	v := tddVocab()
	able, reason := capability(config, d)
	evidence := v.invalid
	if able {
		evidence = v.ok
	}
	return StrictTdd{true, source, evidence, why + "; " + reason, notes}
}

// ResolveStrictTdd is the resolution. config is the text of
// openspec/config.yaml, nil when the project has none; tddMode is the
// contract's mode; d is `jhon init`'s reading of the same root, to which the
// config's overrides are applied (discover.ResolveProjects). Order: no
// config, then policy, then legacy key, then derived.
func ResolveStrictTdd(config *string, tddMode bool, d *discover.Discovery) StrictTdd {
	v := tddVocab()
	k := v.keys
	if config == nil {
		state := "disabled"
		if tddMode {
			state = "enabled"
		}
		return StrictTdd{tddMode, v.fromContract, v.notApplicable, fmt.Sprintf("no %s; contracts/tdd.md states the mode %s", k["file"], state), []string{}}
	}
	cfg := *config
	// A legacy evidence block no longer means anything; it is reported so a
	// human reconciles it into overrides, whatever the rest resolves to.
	evidenceNote := []string{}
	if discover.ConfigMap(cfg, k["evidence"]) != nil {
		evidenceNote = []string{tddNote(v.notes["legacy_evidence"], "")}
	}
	top, nested := configModes(cfg)
	at := k["mode_parent"] + "." + k["mode"]
	var present []string
	if top != nil {
		present = append(present, k["mode"])
	}
	if nested != nil {
		present = append(present, at)
	}
	legacyKeys := strings.Join(present, " and ")

	if policy := configPolicy(cfg); policy != nil {
		// The policy governs; a legacy key beside it is dead and is reported
		// so a human removes it with /jhon-ldd init.
		notes := []string{}
		if legacyKeys != "" {
			notes = append(notes, tddNote(v.notes["legacy_ignored"], legacyKeys))
		}
		notes = append(notes, evidenceNote...)
		switch *policy {
		case v.forbid:
			return StrictTdd{false, v.fromPolicy, v.notApplicable, fmt.Sprintf("%s sets %s: %s", k["file"], k["policy"], v.forbid), notes}
		case v.require:
			return required(v.fromPolicy, cfg, d, fmt.Sprintf("%s sets %s: %s", k["file"], k["policy"], v.require), notes)
		}
		// A policy that is neither value is a human decision this code
		// cannot read. Deriving instead could relax it, so fail closed, as a
		// legacy contradiction does: true with invalid evidence, blocking
		// until fixed.
		return StrictTdd{true, v.fromPolicy, v.invalid,
			fmt.Sprintf("%s sets %s: '%s', which is neither %s nor %s", k["file"], k["policy"], *policy, v.require, v.forbid), notes}
	}

	if legacyKeys != "" {
		// A legacy key is honoured with its old meaning and never
		// reinterpreted: true behaves as require, false as false. Reconciling
		// it into a policy is a human decision taken through /jhon-ldd init.
		notes := append([]string{tddNote(v.notes["legacy"], legacyKeys)}, evidenceNote...)
		if top != nil && nested != nil && *top != *nested {
			// Fail closed on a contradiction. Picking either value would
			// silently choose for the human, and picking false would disable
			// Strict TDD. So the resolution is true with invalid evidence,
			// which status notes until someone makes the two locations agree.
			return StrictTdd{true, v.fromLegacy, v.invalid,
				fmt.Sprintf("%s sets top-level %s: %t but %s: %t; make them agree or remove one", k["file"], k["mode"], *top, at, *nested), notes}
		}
		where, value := k["mode"], top
		if top == nil {
			where, value = at, nested
		}
		if *value {
			return required(v.fromLegacy, cfg, d, fmt.Sprintf("%s sets the legacy %s: true", k["file"], where), notes)
		}
		return StrictTdd{false, v.fromLegacy, v.notApplicable, fmt.Sprintf("%s sets the legacy %s: false", k["file"], where), notes}
	}

	// No stated intent: the value is whatever the project can do right now,
	// recomputed on every call and never persisted.
	able, reason := capability(cfg, d)
	if able {
		return StrictTdd{true, v.derived, v.ok, fmt.Sprintf("no %s; derived from capability: %s", k["policy"], reason), evidenceNote}
	}
	return StrictTdd{false, v.derived, v.notApplicable, fmt.Sprintf("no %s; derived false: %s", k["policy"], reason), evidenceNote}
}

// StrictTddFor is the resolution for a project root: its config.yaml, the
// contract's mode, and its discovery. The error is a contract or a config
// that cannot be read.
func StrictTddFor(root string) (StrictTdd, error) {
	var config *string
	if file := project.LddConfigFile(root); project.Exists(file) {
		b, err := os.ReadFile(file)
		if err != nil {
			return StrictTdd{}, err
		}
		config = new(string(b))
	}
	src, err := contract.Contract("tdd.md")
	if err != nil {
		return StrictTdd{}, err
	}
	mode, err := TddContractMode(src)
	if err != nil {
		return StrictTdd{}, err
	}
	t := ResolveStrictTdd(config, mode, discover.Discover(root))
	// The contract names the OpenSpec config; a project of another layout reads its own.
	if named, own := tddVocab().keys["file"], layout.For(root).Config(); named != own {
		t.Reason = strings.ReplaceAll(t.Reason, named, own)
		for i := range t.Notes {
			t.Notes[i] = strings.ReplaceAll(t.Notes[i], named, own)
		}
	}
	return t, nil
}
