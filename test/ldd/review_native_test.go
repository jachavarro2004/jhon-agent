package ldd

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// The native 4R (T118a, L726, L727): `jhon review start` freezes once and
// prints a tool-less prompt per pertinent lens; `refute` and `validate`
// print the refuter's and the validator's prompts; `record` reduces the
// three answers into review-4r.md bound to the subject, which holds only
// while a fresh freeze gives that subject.

// nativeEnv is an environment whose profiles file is profilesJSON (none
// when ""), with HOME and the XDG dirs in scratch.
func nativeEnv(t *testing.T, profilesJSON string) []string {
	t.Helper()
	home := temp(t)
	file := filepath.Join(home, "profiles.json")
	if profilesJSON != "" {
		support.Write(t, home, map[string]string{"profiles.json": profilesJSON})
	}
	return support.Env("HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "XDG_STATE_HOME="+filepath.Join(home, ".state"),
		"JHON_PROFILES="+file)
}

// nativeRepo is a feature branch whose change c1 adds a migration (risk)
// and code (reliability).
func nativeRepo(t *testing.T) string {
	t.Helper()
	return reviewRepo(t, "schema: spec-driven\n", map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [x] 1.1 a\n",
		"src/migrations/0001_add.sql": "alter table a drop column b;\n", "src/app.py": "def total(xs):\n    return sum(xs)\n"})
}

func reviewRun(t *testing.T, root string, env []string, stdin string, args ...string) support.Result {
	t.Helper()
	return support.Run(t, support.Cmd{Args: append([]string{support.Bin(t), "review"}, args...), Dir: root, Env: env, Stdin: stdin})
}

func startReview(t *testing.T, root string, env []string) map[string]any {
	t.Helper()
	r := reviewRun(t, root, env, "", "start", "--json")
	equal(t, r.Code, 0, r.Stderr)
	return object(t, r.Stdout)
}

func lensJSON(subject, lens string, findings ...map[string]any) map[string]any {
	if findings == nil {
		findings = []map[string]any{}
	}
	return map[string]any{"subject_hash": subject, "lens": lens, "inspection": map[string]any{"status": "completed", "paths": []string{"src/app.py"}},
		"findings": findings, "evidence": []string{"read the diff"}}
}

func role(subject, name string, results ...map[string]any) map[string]any {
	if results == nil {
		results = []map[string]any{}
	}
	return map[string]any{"subject_hash": subject, "role": name, "results": results}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestReviewStart(t *testing.T) {
	t.Parallel()
	root := nativeRepo(t)
	env := nativeEnv(t, `{"active":"p","profiles":{"p":{"tiers":{"pi":{"strong":"prov/t@low"}},"agents":{"review-risk":{"pi":"prov/m1@high"}}}}}`)
	s := startReview(t, root, env)
	equal(t, keys(t, []byte(mustJSON(t, s))), []string{"base", "candidate", "lenses", "subject"}, "start's keys")
	match(t, s["subject"].(string), `^[0-9a-f]{64}$`)
	match(t, s["base"].(string), `^[0-9a-f]{40}$`)
	match(t, s["candidate"].(string), `^[0-9a-f]{40}$`)
	route, _ := statusJSON(t, root)["route"].(map[string]any)
	lenses := s["lenses"].([]any)
	var names []any
	for _, l := range lenses {
		m := l.(map[string]any)
		names = append(names, m["lens"])
		equal(t, keys(t, []byte(mustJSON(t, m))), []string{"bytes", "effort", "est_tokens", "lens", "model", "model_source", "prompt", "routing_key"}, "a lens's keys")
		equal(t, m["routing_key"], "review-"+m["lens"].(string))
		p := m["prompt"].(string)
		equal(t, m["bytes"], float64(len(p)), "bytes")
		equal(t, m["est_tokens"], float64(int(float64(len(p))/3.5)), "est_tokens")
		match(t, p, `(?m)^\+alter table a drop column b;$`, "the diff inline")
		match(t, p, `(?m)^----- FILE src/app\.py -----\ndef total\(xs\):\n    return sum\(xs\)\n----- END FILE src/app\.py -----$`, "the file inline")
		match(t, p, `"subject_hash": "`+s["subject"].(string)+`"`, "the answer's shape names the subject")
		noMatch(t, p, `\{\{\w+\}\}`, "no placeholder left")
	}
	equal(t, names, route["lenses"], "the route's lenses")
	equal(t, names, []any{"risk", "reliability"})
	risk := lenses[0].(map[string]any)
	equal(t, risk["model"], "prov/m1", "the profile's model for review-risk")
	equal(t, risk["effort"], "high", "the profile's thinking")
	equal(t, risk["model_source"], "profile-agent", "review-risk's own profile row")
	rel := lenses[1].(map[string]any)
	equal(t, []any{rel["model"], rel["effort"], rel["model_source"]}, []any{"prov/t", "low", "profile-tier"}, "review-reliability from the profile's strong tier")
	match(t, risk["prompt"].(string), `Inspect security, authorization, data exposure`, "the lens's scope from agents.yaml")
	match(t, risk["prompt"].(string), `(?m)^- high src/migrations/0001_add\.sql: migration or schema$`, "the risk reasons")
	match(t, lenses[1].(map[string]any)["prompt"].(string), `Inspect behavior, tests, boundaries`)
	again := startReview(t, root, env)
	equal(t, again["subject"], s["subject"], "the same bytes freeze the same")
}

func TestReviewStartRefusals(t *testing.T) {
	t.Parallel()
	env := nativeEnv(t, "")
	big := nativeRepo(t)
	support.Write(t, big, map[string]string{"src/big.py": strings.Repeat("x = 'a long enough line of code to fill the prompt'\n", 9000)})
	r := reviewRun(t, big, env, "", "start", "--json")
	equal(t, r.Code, 1, "a prompt over ~100K tokens is refused")
	match(t, r.Stderr, `^jhon review start: the risk lens prompt is ~\d+ tokens, over the 100000-token cap: split the slice and review each part\n$`)
	equal(t, r.Stdout, "", "no object")

	empty := temp(t)
	support.Write(t, empty, map[string]string{"a.txt": "a\n"})
	support.GitRepo(t, empty)
	support.Git(t, empty, "add", "-A")
	support.Git(t, empty, "commit", "-q", "-m", "base")
	r = reviewRun(t, empty, env, "", "start", "--base", "HEAD", "--json")
	equal(t, r.Code, 2, "an empty diff")
	match(t, r.Stderr, `^jhon review start: nothing to freeze in .*: the diff against HEAD is empty`)
	equal(t, r.Stdout, "")
}

func TestReviewRefuteValidate(t *testing.T) {
	t.Parallel()
	root := nativeRepo(t)
	env := nativeEnv(t, `{"active":"p","profiles":{"p":{"tiers":{"pi":{"strong":"prov/t@low"}},"agents":{"review-refuter":{"pi":"prov/ref@medium"},"review-validator":{"pi":"prov/val@xhigh"}}}}}`)
	subject := startReview(t, root, env)["subject"].(string)
	finding := map[string]any{"location": "src/app.py:2", "severity": "CRITICAL", "claim": "sum of nothing", "evidence_class": "inferential",
		"causal_disposition": "introduced", "proof_refs": []string{"src/app.py:2"}}
	second := map[string]any{"location": "src/app.py:1", "severity": "WARNING", "claim": "the name says little", "proof_refs": []string{"src/app.py:1"}}
	lenses := []any{lensJSON(subject, "risk"), lensJSON(subject, "reliability", finding, second)}
	stale := strings.Repeat("0", 64)
	for _, sub := range []string{"refute", "validate"} {
		r := reviewRun(t, root, env, "[]", sub, "--subject", stale)
		equal(t, r.Code, 1, sub+" refuses a stale subject")
		match(t, r.Stderr, `^jhon review `+sub+`: subject mismatch: --subject 0{64}, but the candidate freezes to `+subject)
		equal(t, r.Stdout, "")
	}
	r := reviewRun(t, root, env, mustJSON(t, lenses), "refute", "--subject", subject)
	equal(t, r.Code, 0, r.Stderr)
	o := object(t, r.Stdout)
	equal(t, keys(t, []byte(r.Stdout)), []string{"bytes", "effort", "est_tokens", "model", "model_source", "prompt", "routing_key"})
	equal(t, []any{o["routing_key"], o["model"], o["effort"], o["model_source"]}, []any{"review-refuter", "prov/ref", "medium", "profile-agent"}, "the refuter's own profile row")
	p := o["prompt"].(string)
	equal(t, o["bytes"], float64(len(p)))
	match(t, p, `"id": "reliability-1"`, "findings get ids")
	match(t, p, `sum of nothing`)
	match(t, p, `(?m)^\+def total\(xs\):$`, "the diff")
	match(t, p, `"role": "refuter"`)

	refuter := role(subject, "refuter", map[string]any{"finding_id": "reliability-1", "outcome": "stands", "reason": "src/app.py:2 returns 0"},
		map[string]any{"finding_id": "reliability-2", "outcome": "refuted", "reason": "a style point"})
	r = reviewRun(t, root, env, mustJSON(t, map[string]any{"lenses": lenses, "refuter": refuter}), "validate", "--subject", subject)
	equal(t, r.Code, 0, r.Stderr)
	o = object(t, r.Stdout)
	equal(t, []any{o["routing_key"], o["model"], o["effort"], o["model_source"]}, []any{"review-validator", "prov/val", "xhigh", "profile-agent"}, "the validator's own profile row")
	p = o["prompt"].(string)
	match(t, p, `"id": "reliability-1"`, "the finding that stands")
	noMatch(t, p, `reliability-2`, "the refuted one is not validated")
	match(t, p, `"role": "validator"`)

	bad := role(subject, "refuter", map[string]any{"finding_id": "reliability-1", "outcome": "stands", "reason": "x"})
	r = reviewRun(t, root, env, mustJSON(t, map[string]any{"lenses": lenses, "refuter": bad}), "validate", "--subject", subject)
	equal(t, r.Code, 1, "a refuter that skips a finding")
	match(t, r.Stderr, `the refuter gives no verdict on reliability-2`)

	// Without a profile, the refuter's model comes from agents.yaml's tier.
	r = reviewRun(t, root, nativeEnv(t, ""), mustJSON(t, lenses), "refute", "--subject", subject)
	equal(t, r.Code, 0, r.Stderr)
	o = object(t, r.Stdout)
	equal(t, o["model_source"], "agents.yaml", "no profile")
	match(t, o["model"].(string), `.`, "agents.yaml's strong tier names a model")
}

func TestReviewRecord(t *testing.T) {
	t.Parallel()
	root := nativeRepo(t)
	env := nativeEnv(t, "")
	file := filepath.Join(root, "openspec/changes/c1/review-4r.md")
	subject := startReview(t, root, env)["subject"].(string)
	clean := map[string]any{"lenses": []any{lensJSON(subject, "risk"), lensJSON(subject, "reliability")},
		"refuter": role(subject, "refuter"), "validator": role(subject, "validator")}

	r := reviewRun(t, root, env, mustJSON(t, clean), "record", "--subject", strings.Repeat("0", 64))
	equal(t, r.Code, 1, "a stale subject")
	match(t, r.Stderr, `subject mismatch`)
	if exists(file) {
		t.Fatal("a refused record writes nothing")
	}

	r = reviewRun(t, root, env, mustJSON(t, clean), "record", "--subject", subject)
	equal(t, r.Code, 0, r.Stderr)
	o := object(t, r.Stdout)
	equal(t, o["verdict"], "pass")
	equal(t, o["record"], "openspec/changes/c1/review-4r.md")
	equal(t, o["lenses"], []any{"risk", "reliability"})
	base := o["base"].(string)
	src := support.Read(t, file)
	equal(t, src, "# 4R: c1\n\nlenses: risk, reliability\nverdict: pass\nsubject: "+subject+"\nbase: "+base+"\n", "the record")
	equal(t, reviews(t, root)["4r"]["done"], true, "a record whose subject is the fresh one holds")
	equal(t, startReview(t, root, env)["subject"], subject, "writing the record leaves the subject as it was")

	for name, in := range map[string]string{
		"not JSON":             "{",
		"a lens outside four":  mustJSON(t, map[string]any{"lenses": []any{lensJSON(subject, "speed")}, "refuter": role(subject, "refuter"), "validator": role(subject, "validator")}),
		"a missing lens":       mustJSON(t, map[string]any{"lenses": []any{lensJSON(subject, "risk")}, "refuter": role(subject, "refuter"), "validator": role(subject, "validator")}),
		"no validator":         mustJSON(t, map[string]any{"lenses": clean["lenses"], "refuter": role(subject, "refuter")}),
		"another lens subject": mustJSON(t, map[string]any{"lenses": []any{lensJSON(subject, "risk"), lensJSON(strings.Repeat("1", 64), "reliability")}, "refuter": role(subject, "refuter"), "validator": role(subject, "validator")}),
		"a severe finding without proof": mustJSON(t, map[string]any{"lenses": []any{lensJSON(subject, "risk",
			map[string]any{"location": "a:1", "severity": "BLOCKER", "claim": "x", "proof_refs": []string{}}), lensJSON(subject, "reliability")},
			"refuter": role(subject, "refuter", map[string]any{"finding_id": "risk-1", "outcome": "stands", "reason": "x"}), "validator": role(subject, "validator")}),
	} {
		r := reviewRun(t, root, env, in, "record", "--subject", subject)
		equal(t, r.Code, 1, name)
		match(t, r.Stderr, `^jhon review record: .*; nothing written\n$`, name)
		equal(t, support.Read(t, file), src, name+": the record is unchanged")
	}

	// A confirmed severe finding is the correction set: the verdict fails.
	severe := map[string]any{"location": "src/app.py:2", "severity": "CRITICAL", "claim": "wrong total", "evidence_class": "deterministic",
		"causal_disposition": "introduced", "proof_refs": []string{"src/app.py:2"}}
	maybe := map[string]any{"location": "src/app.py:1", "severity": "BLOCKER", "claim": "maybe", "evidence_class": "inferential",
		"causal_disposition": "unknown", "proof_refs": []string{"src/app.py:1"}}
	old := map[string]any{"location": "src/base.py:1", "severity": "CRITICAL", "claim": "old", "evidence_class": "deterministic",
		"causal_disposition": "pre_existing", "proof_refs": []string{"src/base.py:1"}}
	found := map[string]any{"lenses": []any{lensJSON(subject, "risk", old), lensJSON(subject, "reliability", severe, maybe)},
		"refuter": role(subject, "refuter", map[string]any{"finding_id": "risk-1", "outcome": "stands", "reason": "r"},
			map[string]any{"finding_id": "reliability-1", "outcome": "stands", "reason": "r"}, map[string]any{"finding_id": "reliability-2", "outcome": "stands", "reason": "r"}),
		"validator": role(subject, "validator", map[string]any{"finding_id": "risk-1", "outcome": "confirmed", "reason": "v", "proof_refs": []string{"src/base.py:1"}},
			map[string]any{"finding_id": "reliability-1", "outcome": "confirmed", "reason": "v", "proof_refs": []string{"src/app.py:2"}},
			map[string]any{"finding_id": "reliability-2", "outcome": "unconfirmed", "reason": "v"})}
	r = reviewRun(t, root, env, mustJSON(t, found), "record", "--subject", subject)
	equal(t, r.Code, 0, r.Stderr)
	o = object(t, r.Stdout)
	equal(t, o["verdict"], "fail")
	equal(t, o["correction_set"], []any{"reliability-1"})
	equal(t, o["for_verify"], []any{"reliability-2"})
	equal(t, o["follow_ups"], []any{"risk-1"})
	src = support.Read(t, file)
	match(t, src, "(?m)^verdict: fail$")
	match(t, src, "(?m)^## Correction set\n\n- \\[reliability-1\\] CRITICAL src/app.py:2: wrong total \\(proof: src/app.py:2\\)$")
	equal(t, reviews(t, root)["4r"]["done"], false, "a failing record holds no 4R")

	r = reviewRun(t, root, env, mustJSON(t, clean), "record", "--subject", subject)
	equal(t, r.Code, 0, r.Stderr)
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "c1")
	equal(t, reviews(t, root)["4r"]["done"], true, "committing the same bytes keeps the subject")
	support.Write(t, root, map[string]string{"src/app.py": "def total(xs):\n    return 0\n"})
	equal(t, reviews(t, root)["4r"]["done"], false, "a record whose candidate changed since is stale")
	support.Write(t, root, map[string]string{"openspec/changes/c1/review-4r.md": "lenses: risk, reliability\nverdict: pass\n"})
	equal(t, reviews(t, root)["4r"]["done"], true, "a record without subject: is read as before")
	if err := os.WriteFile(file, []byte("lenses: risk\nverdict: pass\nsubject: "+subject+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	equal(t, reviews(t, root)["4r"]["done"], false, "a subject without its base holds nothing")
}

// fileHash is the sha256 of the file at p, or "" when it is absent.
func fileHash(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

// `record` writes nothing on a malformed answer (T118a verify, L732): each
// refused input exits 1 and leaves review-4r.md as it was, absent or not.
func TestReviewRecordNothingWritten(t *testing.T) {
	t.Parallel()
	root := nativeRepo(t)
	env := nativeEnv(t, "")
	file := filepath.Join(root, "openspec/changes/c1/review-4r.md")
	subject := startReview(t, root, env)["subject"].(string)
	other := strings.Repeat("1", 64)
	both := []any{lensJSON(subject, "risk"), lensJSON(subject, "reliability")}
	in := func(lenses []any, refuter, validator map[string]any) string {
		m := map[string]any{"lenses": lenses}
		if refuter != nil {
			m["refuter"] = refuter
		}
		if validator != nil {
			m["validator"] = validator
		}
		return mustJSON(t, m)
	}
	noFindings := lensJSON(subject, "reliability")
	delete(noFindings, "findings")
	finding := map[string]any{"location": "src/app.py:2", "severity": "WARNING", "claim": "c", "proof_refs": []string{"src/app.py:2"}}
	severe := map[string]any{"location": "src/app.py:2", "severity": "CRITICAL", "claim": "c", "evidence_class": "deterministic",
		"causal_disposition": "introduced", "proof_refs": []string{"src/app.py:2"}}
	withFinding := []any{lensJSON(subject, "risk"), lensJSON(subject, "reliability", finding)}
	withSevere := []any{lensJSON(subject, "risk"), lensJSON(subject, "reliability", severe)}
	stands := role(subject, "refuter", map[string]any{"finding_id": "reliability-1", "outcome": "stands", "reason": "r"})
	type tc struct{ name, stdin, subject string }
	cases := []tc{
		{"a missing route lens", in([]any{lensJSON(subject, "risk")}, role(subject, "refuter"), role(subject, "validator")), subject},
		{"a duplicate lens", in([]any{lensJSON(subject, "risk"), lensJSON(subject, "risk"), lensJSON(subject, "reliability")}, role(subject, "refuter"), role(subject, "validator")), subject},
		{"an unknown lens", in(append([]any{lensJSON(subject, "speed")}, both...), role(subject, "refuter"), role(subject, "validator")), subject},
		{"a wrong subject_hash", in([]any{lensJSON(subject, "risk"), lensJSON(other, "reliability")}, role(subject, "refuter"), role(subject, "validator")), subject},
		{"a --subject mismatch", in(both, role(subject, "refuter"), role(subject, "validator")), other},
		{"no refuter", in(both, nil, role(subject, "validator")), subject},
		{"no validator", in(both, role(subject, "refuter"), nil), subject},
		{"a validator with role refuter", in(both, role(subject, "refuter"), role(subject, "refuter")), subject},
		{"malformed JSON", `{"lenses": [`, subject},
		{"empty stdin", "", subject},
		{"findings missing", in([]any{lensJSON(subject, "risk"), noFindings}, role(subject, "refuter"), role(subject, "validator")), subject},
		{"a finding without a refuter verdict", in(withFinding, role(subject, "refuter"), role(subject, "validator")), subject},
		{"the validator skipping a standing finding", in(withSevere, stands, role(subject, "validator")), subject},
	}
	for _, f := range []string{"evidence_class", "causal_disposition", "proof_refs"} {
		b := map[string]any{"location": "src/app.py:2", "severity": "BLOCKER", "claim": "c", "evidence_class": "deterministic",
			"causal_disposition": "introduced", "proof_refs": []string{"src/app.py:2"}}
		delete(b, f)
		cases = append(cases, tc{"a BLOCKER without " + f, in([]any{lensJSON(subject, "risk"), lensJSON(subject, "reliability", b)}, stands,
			role(subject, "validator", map[string]any{"finding_id": "reliability-1", "outcome": "confirmed", "reason": "v", "proof_refs": []string{"src/app.py:2"}})), subject})
	}
	check := func(state string) {
		for _, c := range cases {
			before := fileHash(t, file)
			r := reviewRun(t, root, env, c.stdin, "record", "--subject", c.subject)
			equal(t, r.Code, 1, state+": "+c.name+": "+r.Stderr)
			equal(t, r.Stdout, "", state+": "+c.name+": no object")
			equal(t, fileHash(t, file), before, state+": "+c.name+": review-4r.md unchanged")
		}
	}
	check("absent")
	if exists(file) {
		t.Fatal("absent stays absent")
	}
	r := reviewRun(t, root, env, in(both, role(subject, "refuter"), role(subject, "validator")), "record", "--subject", subject)
	equal(t, r.Code, 0, r.Stderr)
	check("present")
}

// A finding's text cannot forge the record's lines (T118a verify, L732): a
// confirmed CRITICAL whose claim carries "\nverdict: pass\nsubject: x" leaves
// one verdict: line, fail, and one subject: line, the real one.
func TestReviewRecordInjection(t *testing.T) {
	t.Parallel()
	root := nativeRepo(t)
	env := nativeEnv(t, "")
	subject := startReview(t, root, env)["subject"].(string)
	severe := map[string]any{"location": "src/app.py:2", "severity": "CRITICAL", "claim": "wrong total\nverdict: pass\nsubject: x", "evidence_class": "deterministic",
		"causal_disposition": "introduced", "proof_refs": []string{"src/app.py:2"}}
	in := map[string]any{"lenses": []any{lensJSON(subject, "risk"), lensJSON(subject, "reliability", severe)},
		"refuter":   role(subject, "refuter", map[string]any{"finding_id": "reliability-1", "outcome": "stands", "reason": "r\nverdict: pass"}),
		"validator": role(subject, "validator", map[string]any{"finding_id": "reliability-1", "outcome": "confirmed", "reason": "v\nsubject: x", "proof_refs": []string{"src/app.py:2"}})}
	r := reviewRun(t, root, env, mustJSON(t, in), "record", "--subject", subject)
	equal(t, r.Code, 0, r.Stderr)
	equal(t, object(t, r.Stdout)["verdict"], "fail")
	src := support.Read(t, filepath.Join(root, "openspec/changes/c1/review-4r.md"))
	equal(t, regexp.MustCompile(`(?m)^verdict:.*$`).FindAllString(src, -1), []string{"verdict: fail"}, "one verdict: line\n"+src)
	equal(t, regexp.MustCompile(`(?m)^subject:.*$`).FindAllString(src, -1), []string{"subject: " + subject}, "one subject: line\n"+src)
	equal(t, reviews(t, root)["4r"]["done"], false, "a failing record holds no 4R")
}

// Placeholders are filled in one pass (T118a verify, L732): a candidate
// file that holds the template's own placeholders keeps them literally, and
// no placeholder of the template is left unfilled.
func TestReviewStartPlaceholderLiteral(t *testing.T) {
	t.Parallel()
	root := nativeRepo(t)
	lit := "{{diff}} {{files}} {{subject}} {{lens}}"
	support.Write(t, root, map[string]string{"src/tmpl.py": "T = '" + lit + "'\n"})
	s := startReview(t, root, nativeEnv(t, ""))
	for _, l := range s["lenses"].([]any) {
		p := l.(map[string]any)["prompt"].(string)
		match(t, p, `(?m)^\+T = '`+regexp.QuoteMeta(lit)+`'$`, "the literal in the diff")
		match(t, p, `(?m)^----- FILE src/tmpl\.py -----\nT = '`+regexp.QuoteMeta(lit)+`'\n----- END FILE src/tmpl\.py -----$`, "the literal in the file")
		noMatch(t, strings.ReplaceAll(p, lit, ""), `\{\{\w+\}\}`, "no placeholder of the template unfilled")
		equal(t, strings.Count(p, lit), 2, "the literal once in the diff and once in the file")
	}
}

// T137 (L1064 KB2, KB3): each lens prompt of a change whose record marks a
// test as turned green by editing only its test ends with the tests to
// judge; with none marked, no lens prompt names them.
func TestReviewStartCarriesTheTestsToJudge(t *testing.T) {
	t.Parallel()
	root := nativeRepo(t)
	env := nativeEnv(t, "")
	for _, l := range startReview(t, root, env)["lenses"].([]any) {
		noMatch(t, l.(map[string]any)["prompt"].(string), `## Tests to judge`, "nothing to judge")
	}
	support.Write(t, root, map[string]string{"openspec/changes/c1/execution-record.json": `{"timestamp":"2026-01-01T00:02:00.000Z","complete":true,"scope":"full","executions":[],` +
		`"turned_green":[{"project":"src","test":"tests/test_ids.py::test_ids","file":"src/tests/test_ids.py","red_run":"2026-01-01T00:01:00.000Z","green_run":"2026-01-01T00:02:00.000Z","red_tree":"t1","green_tree":"t2"}]}` + "\n"})
	for _, l := range startReview(t, root, env)["lenses"].([]any) {
		p := l.(map[string]any)["prompt"].(string)
		match(t, p, "\n\n## Tests to judge\nAdded at launch from the change's execution record: .*report it as a finding\\.\n"+
			"- `tests/test_ids\\.py::test_ids` \\(src, src/tests/test_ids\\.py\\): failed in run 2026-01-01T00:01:00\\.000Z, passed in run 2026-01-01T00:02:00\\.000Z$", l.(map[string]any)["lens"])
	}
}
