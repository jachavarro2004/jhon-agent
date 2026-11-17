package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"github.com/jachavarro2004/jhon-agent/engine/internal/manifest"
	"github.com/jachavarro2004/jhon-agent/engine/internal/profiles"
	"github.com/jachavarro2004/jhon-agent/engine/internal/render"
	"github.com/jachavarro2004/jhon-agent/engine/internal/vcs"
)

// The native 4R (contracts/review-4r.md): one freeze, then tool-less
// prompts a runtime completes without tools or a checkout, and a record
// bound to the subject.
//
//   - `start` freezes once and prints, per pertinent lens (the route's
//     lenses), its prompt (prose/review/lens.md with the lens's scope from
//     agents.yaml's review-<lens>, the risk reasons, the diff and every
//     changed file inline), its routing key, the active profile's pi model
//     and thinking for that agent, and its size; a prompt past
//     ldd.PromptTokenCap is refused: split the slice.
//   - `refute` reads the lens results and prints the refuter's prompt;
//     `validate` reads them with the refuter's answer and prints the
//     validator's prompt for the findings that stand.
//   - `record` reads all three, reduces them and writes the change's
//     review-4r.md with its `subject:` and `base:`.
//
// Each freezes again and refuses (exit 1) a --subject that is not the
// fresh one: the bytes changed since `start`. The subject leaves out the
// change's own records (ldd.ReviewExempt), so writing review-4r.md does
// not change it. Usage errors and a state that cannot freeze (an empty
// diff among them) exit 2; a result it cannot read exits 1 and writes
// nothing.

type reviewArgs struct {
	base, change, subject string
	baseSet               bool
}

func runReviewNative(sub string, argv []string) (int, error) {
	return reviewNative(sub, argv, os.Stdin, os.Stdout, os.Stderr), nil
}

// reviewNative is one native 4R step over stdin, stdout and stderr, its
// exit code; `jhon review run` calls it in process.
func reviewNative(sub string, argv []string, stdin io.Reader, stdout, stderr io.Writer) int {
	var a reviewArgs
	for i := 0; i < len(argv); i++ {
		switch {
		case argv[i] == "--json":
		case argv[i] == "--base" && i+1 < len(argv):
			i++
			a.base, a.baseSet = argv[i], true
		case argv[i] == "--change" && i+1 < len(argv):
			i++
			a.change = argv[i]
		case argv[i] == "--subject" && i+1 < len(argv) && sub != "start":
			i++
			a.subject = argv[i]
		default:
			fmt.Fprintln(stderr, reviewUsage)
			return 2
		}
	}
	if sub != "start" && a.subject == "" {
		fmt.Fprintf(stderr, "jhon review %s: --subject <hash> is required (the subject `jhon review start` printed)\n", sub)
		return 2
	}
	fail := func(code int, format string, v ...any) int {
		fmt.Fprintf(stderr, "jhon review "+sub+": "+format+"\n", v...)
		return code
	}
	top := vcs.GitTopLevel(cwd())
	if top == "" {
		return fail(2, "%s is not inside a git work tree", cwd())
	}
	if a.change == "" {
		if active, _ := ldd.BranchChanges(top); len(active) == 1 {
			a.change = active[0]
		}
	}
	if sub == "record" && a.change == "" {
		return fail(2, "no single active change on this branch: name it with --change <name>")
	}
	var asked *string
	if a.baseSet {
		asked = &a.base
	}
	risk, err := ldd.RiskOf(top, asked)
	if err != nil {
		return fail(2, "%v", err)
	}
	if !a.baseSet {
		a.base = risk.Base
	}
	f, err := vcs.FreezeExempt(top, a.base, ldd.ReviewExempt(top, a.change))
	if err != nil {
		return fail(2, "%v", err)
	}
	if sub != "start" && f.Subject != a.subject {
		return fail(1, "subject mismatch: --subject %s, but the candidate freezes to %s now; the bytes changed since `jhon review start`: start the review again", a.subject, f.Subject)
	}
	root, _ := Root()
	template := func(name string) (string, error) {
		b, err := os.ReadFile(filepath.Join(root, "prose", "review", name+".md"))
		return string(b), err
	}
	files := ldd.FilesBlock(top, f)
	switch sub {
	case "start":
		return reviewStart(stdout, fail, root, template, risk, f, files, ldd.TestsToJudgeBlock(execution.TurnedGreenOf(top, a.change)))
	case "refute", "validate":
		raw, _ := io.ReadAll(stdin)
		var findings []ldd.Finding
		if sub == "refute" {
			lenses, err := ldd.ParseLensResults(raw)
			if err == nil {
				lenses, err = ldd.CheckLenses(lenses, f.Subject)
			}
			if err != nil {
				return fail(1, "%v", err)
			}
			findings = ldd.AllFindings(lenses)
		} else {
			rr, err := ldd.ParseReviewResults(raw)
			if err == nil {
				rr.Lenses, err = ldd.CheckLenses(rr.Lenses, f.Subject)
			}
			all := ldd.AllFindings(rr.Lenses)
			if err == nil {
				err = ldd.CheckRole(rr.Refuter, "refuter", f.Subject, ldd.IDs(all))
			}
			if err != nil {
				return fail(1, "%v", err)
			}
			findings = ldd.Standing(all, rr.Refuter)
		}
		t, err := template(map[string]string{"refute": "refuter", "validate": "validator"}[sub])
		if err != nil {
			return fail(2, "its prompt template cannot be read: %v", err)
		}
		list, _ := json.MarshalIndent(nonNil(findings), "", "  ")
		prompt := ldd.Fill(t, map[string]string{"subject": f.Subject, "findings": string(list), "diff": f.Diff, "files": files})
		if n := ldd.EstTokens(len(prompt)); n > ldd.PromptTokenCap {
			return fail(1, "the %s prompt is ~%d tokens, over the %d-token cap: split the slice and review each part", sub, n, ldd.PromptTokenCap)
		}
		m, err := manifest.Load(root)
		if err != nil {
			return fail(2, "agents.yaml: %v", err)
		}
		agent := map[string]string{"refute": "review-refuter", "validate": "review-validator"}[sub]
		model, effort, source := reviewModel(m, activeProfile(), agent)
		reviewJSON(stdout, map[string]any{"routing_key": agent, "model": model, "effort": effort, "model_source": source,
			"prompt": prompt, "bytes": len(prompt), "est_tokens": ldd.EstTokens(len(prompt))})
		return 0
	}
	raw, _ := io.ReadAll(stdin)
	rr, err := ldd.ParseReviewResults(raw)
	if err == nil {
		rr.Lenses, err = ldd.CheckLenses(rr.Lenses, f.Subject)
	}
	all := ldd.AllFindings(rr.Lenses)
	if err == nil {
		err = ldd.CheckRole(rr.Refuter, "refuter", f.Subject, ldd.IDs(all))
	}
	if err == nil {
		err = ldd.CheckRole(rr.Validator, "validator", f.Subject, ldd.IDs(ldd.Standing(all, rr.Refuter)))
	}
	if err == nil {
		for _, l := range risk.Lenses {
			if !slices.ContainsFunc(rr.Lenses, func(r ldd.LensResult) bool { return r.Lens == l }) {
				err = fmt.Errorf("no result for the %s lens, which this diff calls for (%s)", l, strings.Join(risk.Lenses, ", "))
				break
			}
		}
	}
	if err != nil {
		return fail(1, "%v; nothing written", err)
	}
	red := ldd.Reduce(rr)
	rel := filepath.Join(layout.For(top).ChangeDir(a.change), ldd.ReviewFile)
	if err := writeAtomic(layout.At(top, rel), ldd.RecordText(a.change, f.Subject, f.Base, red)); err != nil {
		return fail(2, "%v", err)
	}
	reviewJSON(stdout, map[string]any{"record": filepath.ToSlash(rel), "subject": f.Subject, "base": f.Base, "lenses": red.Lenses, "verdict": red.Verdict,
		"correction_set": ldd.IDs(red.CorrectionSet), "for_verify": ldd.IDs(red.ForVerify), "follow_ups": ldd.IDs(red.FollowUps),
		"information": ldd.IDs(red.Information), "refuted": ldd.IDs(red.Refuted), "unavailable": red.Unavailable})
	return 0
}

// reviewStart prints `jhon review start`'s object. judge is the change's
// tests to judge (T137), which each lens prompt ends with; "" for none.
func reviewStart(stdout io.Writer, fail func(int, string, ...any) int, root string, template func(string) (string, error), risk ldd.Risk, f vcs.Frozen, files, judge string) int {
	t, err := template("lens")
	if err != nil {
		return fail(2, "its prompt template cannot be read: %v", err)
	}
	m, err := manifest.Load(root)
	if err != nil {
		return fail(2, "agents.yaml: %v", err)
	}
	profile := activeProfile()
	reasons := ldd.ReasonsBlock(risk.Reasons)
	lenses := []any{}
	for _, lens := range risk.Lenses {
		agent := "review-" + lens
		scope := ""
		if i := slices.IndexFunc(m.Agents, func(a manifest.Agent) bool { return a.Name == agent }); i >= 0 && m.Agents[i].Vars != nil {
			scope, _ = m.Agents[i].Vars.Get("scope")
		}
		model, effort, source := reviewModel(m, profile, agent)
		prompt := ldd.Fill(t, map[string]string{"lens": lens, "scope": scope, "reasons": reasons, "subject": f.Subject, "diff": f.Diff, "files": files})
		if judge != "" {
			prompt += "\n\n" + judge
		}
		n := ldd.EstTokens(len(prompt))
		if n > ldd.PromptTokenCap {
			return fail(1, "the %s lens prompt is ~%d tokens, over the %d-token cap: split the slice and review each part", lens, n, ldd.PromptTokenCap)
		}
		lenses = append(lenses, map[string]any{"lens": lens, "routing_key": agent, "model": model, "effort": effort, "model_source": source,
			"prompt": prompt, "bytes": len(prompt), "est_tokens": n})
	}
	reviewJSON(stdout, map[string]any{"subject": f.Subject, "base": f.Base, "candidate": f.Candidate, "lenses": lenses})
	return 0
}

// activeProfile is the active profile, nil when none.
func activeProfile() any {
	pf, _ := render.ReadProfiles()
	_, profile := profiles.Active(pf)
	return profile
}

// reviewModel is agent's pi model and effort under profile, resolved as the
// rendered agents are (profiles.ModelFor: the profile's row for the agent,
// else its tier, else agents.yaml's tier), and where it came from:
// profile-agent, profile-tier or agents.yaml; all "" when unresolved.
func reviewModel(m manifest.Manifest, profile any, agent string) (model, effort, source string) {
	return reviewModelIn(m, profile, "pi", agent)
}

// reviewModelIn is reviewModel in runtime (pi or claude-code).
func reviewModelIn(m manifest.Manifest, profile any, runtime, agent string) (model, effort, source string) {
	i := slices.IndexFunc(m.Agents, func(a manifest.Agent) bool { return a.Name == agent })
	if i < 0 {
		return "", "", ""
	}
	v, ok := profiles.ModelFor(m.TierTable(), profile, runtime, profiles.Agent{Name: agent, Tier: m.Agents[i].Tier})
	if !ok {
		return "", "", ""
	}
	tier, ok := profiles.TierValue(js.String(v.Value))
	if !ok {
		return "", "", ""
	}
	switch {
	case strings.HasPrefix(v.Source, "profile agent "):
		source = "profile-agent"
	case strings.HasPrefix(v.Source, "profile tier "):
		source = "profile-tier"
	default:
		source = "agents.yaml"
	}
	return tier.Model, tier.Effort, source
}

// nonNil is findings, [] when none.
func nonNil(findings []ldd.Finding) []ldd.Finding {
	if findings == nil {
		return []ldd.Finding{}
	}
	return findings
}

// reviewJSON prints v to w as one line of JSON, HTML characters unescaped.
func reviewJSON(w io.Writer, v any) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	w.Write(b.Bytes())
}

// writeAtomic writes text to file through a temporary file in its directory.
func writeAtomic(file, text string) error {
	dir := filepath.Dir(file)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return errors.New("the change's directory " + dir + " does not exist")
	}
	tmp, err := os.CreateTemp(dir, ".review-4r-*")
	if err != nil {
		return err
	}
	if _, err := tmp.WriteString(text); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	tmp.Close()
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), file); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}
