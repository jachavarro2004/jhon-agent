// T177 (L1453, L1454): the agents explore, worker and verify are jhon-scout,
// jhon-builder and jhon-checker; the 4R review is the Prism review, its
// lenses facets, its agents prism-*; review_required takes prism and
// checker. For one version an old name is read with a one-line warning,
// `jhon settings migrate` and `jhon profile` rewrite it, and a record
// written under one is shown under the new name. Every check runs the
// binary through its public interface.
package rename

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// scratch is a HOME, its settings and profiles files (profiles holding
// body, when not ""), and the environment that points the binary at them.
type scratch struct {
	home, settings, profiles string
	env                      []string
}

func newScratch(t *testing.T, profiles string) scratch {
	t.Helper()
	h := t.TempDir()
	s := scratch{home: h, settings: filepath.Join(h, ".config", "jhon", "settings.toml"), profiles: filepath.Join(h, ".config", "jhon", "profiles.json")}
	if err := os.MkdirAll(filepath.Dir(s.profiles), 0o777); err != nil {
		t.Fatal(err)
	}
	if profiles != "" {
		if err := os.WriteFile(s.profiles, []byte(profiles), 0o666); err != nil {
			t.Fatal(err)
		}
	}
	root := support.Root()
	s.env = support.Env("HOME="+h, "XDG_CONFIG_HOME="+filepath.Join(h, ".config"), "XDG_STATE_HOME="+filepath.Join(h, ".state"),
		"JHON_SETTINGS="+s.settings, "JHON_PROFILES="+s.profiles, "JHON_SOURCE="+root, "JHON_ROOT="+root)
	return s
}

func (s scratch) jhon(t *testing.T, dir string, args ...string) support.Result {
	t.Helper()
	return support.Jhon(t, dir, s.env, args...)
}

func wantOut(t *testing.T, r support.Result, code int, patterns ...string) {
	t.Helper()
	if r.Code != code {
		t.Fatalf("exit %d, want %d\n%s", r.Code, code, r.Out())
	}
	for _, p := range patterns {
		if !regexp.MustCompile(p).MatchString(r.Out()) {
			t.Fatalf("output does not match %q:\n%s", p, r.Out())
		}
	}
}

// warning is the one-line alias warning for old read as now.
func warning(what, old, now string) string {
	return regexp.QuoteMeta(what + ` "` + old + `" is now "` + now + `"; the old name is read until v0.4 (jhon settings migrate rewrites it)`)
}

var newAgents = []string{"jhon-scout", "jhon-builder", "jhon-checker", "prism-safety", "prism-failure", "prism-clarity", "prism-behavior", "prism-challenger", "prism-arbiter", "jhon-orchestrator"}

var oldAgents = map[string]string{
	"explore": "jhon-scout", "worker": "jhon-builder", "verify": "jhon-checker",
	"review-risk": "prism-safety", "review-resilience": "prism-failure", "review-readability": "prism-clarity",
	"review-reliability": "prism-behavior", "review-refuter": "prism-challenger", "review-validator": "prism-arbiter",
}

// TestNewNamesRender: the manifest's agents are the new names, and the
// active profile's rows name only them.
func TestNewNamesRender(t *testing.T) {
	s := newScratch(t, "")
	r := s.jhon(t, "", "profile", "show")
	wantOut(t, r, 0)
	for _, a := range newAgents {
		if !regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(a) + ` `).MatchString(r.Stdout) {
			t.Errorf("profile show has no row for %s:\n%s", a, r.Stdout)
		}
	}
	for old := range oldAgents {
		if regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(old) + ` `).MatchString(r.Stdout) {
			t.Errorf("profile show still names %s:\n%s", old, r.Stdout)
		}
	}
	for _, f := range []string{"jhon-scout.md", "jhon-builder.md", "jhon-checker.md"} {
		if _, err := os.Stat(support.Path("prose", "agents", f)); err != nil {
			t.Errorf("prose/agents/%s: %v", f, err)
		}
	}
}

// TestOldProfileAgentIsAliased: a profile naming an old agent is read under
// the new name, with the warning, and its model reaches the new agent.
func TestOldProfileAgentIsAliased(t *testing.T) {
	s := newScratch(t, `{"active":"default","profiles":{"p":{"agents":{"worker":{"pi":"anthropic/claude-x@high"},"review-risk":{"pi":"anthropic/claude-y"}}}}}`)
	r := s.jhon(t, "", "profile", "show", "p")
	wantOut(t, r, 0, warning("p.agents: agent", "worker", "jhon-builder"), warning("p.agents: agent", "review-risk", "prism-safety"),
		`(?m)^  jhon-builder\s+pi\s+anthropic/claude-x@high\s+profile agent jhon-builder\.pi$`,
		`(?m)^  prism-safety\s+pi\s+anthropic/claude-y\s+profile agent prism-safety\.pi$`)
	if n := strings.Count(r.Stderr, "\n"); n != 2 {
		t.Errorf("want one warning line per old name, got %d lines:\n%s", n, r.Stderr)
	}
	// `jhon profile set` takes an old agent name too, with the warning.
	r = s.jhon(t, "", "profile", "set", "p", "explore", "pi", "anthropic/claude-z")
	wantOut(t, r, 0, warning("set: agent", "explore", "jhon-scout"), `profile p: jhon-scout in pi is anthropic/claude-z`)
	if got := support.Read(t, s.profiles); strings.Contains(got, `"worker"`) || strings.Contains(got, `"explore"`) || !strings.Contains(got, `"jhon-scout": {`) {
		t.Fatalf("profiles.json after set:\n%s", got)
	}
}

// TestProfileEditRewritesOldNames: any `jhon profile` edit writes the old
// names under their new ones.
func TestProfileEditRewritesOldNames(t *testing.T) {
	s := newScratch(t, `{"active":"default","profiles":{"p":{"agents":{"verify":{"pi":"anthropic/claude-x"}}}}}`)
	wantOut(t, s.jhon(t, "", "profile", "mode", "p", "writer"), 0, `rewritten: .*`+warning("p.agents: agent", "verify", "jhon-checker"))
	got := support.Read(t, s.profiles)
	if strings.Contains(got, `"verify"`) || !strings.Contains(got, `"jhon-checker": {`) {
		t.Fatalf("profiles.json after the edit:\n%s", got)
	}
}

// TestMigrateRewritesProfilesAndProject: `jhon settings migrate` rewrites
// the profiles' old agent names and the project's review_required values,
// keeping every other byte, and is idempotent.
func TestMigrateRewritesProfilesAndProject(t *testing.T) {
	s := newScratch(t, "{\n  \"active\": \"p\",\n  \"profiles\": {\n    \"p\": {\n      \"agents\": {\n        \"explore\": {\"pi\": \"a/m\"},\n        \"jhon-orchestrator\": {\"pi\": \"a/o\"},\n        \"review-validator\": {\"claude-code\": \"claude-x@high\"}\n      }\n    }\n  }\n}\n")
	proj := filepath.Join(s.home, "proj")
	config := "schema: spec-driven # keep\nreview_required:\n  - change: verify-login\n    review: verify # owner\n  - area: \"src/**\"\n    review: [Verify, \"4R\"]\n  - {change: c3, review: 4r}\n  - area: docs\n    review:\n      - 4r\n      - checker\ncontext: verify stays here\n"
	support.Write(t, proj, map[string]string{"openspec/config.yaml": config})
	support.GitRepo(t, proj)

	r := s.jhon(t, proj, "settings", "migrate")
	wantOut(t, r, 0,
		`rewritten: .*`+warning("p.agents: agent", "explore", "jhon-scout"),
		`rewritten: .*`+warning("p.agents: agent", "review-validator", "prism-arbiter"),
		`rewritten: `+warning("review_required line 4: review", "verify", "checker"),
		`rewritten: `+warning("review_required line 6: review", "4R", "prism"))
	var doc struct {
		Profiles map[string]struct {
			Agents map[string]map[string]string `json:"agents"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(support.Read(t, s.profiles)), &doc); err != nil {
		t.Fatal(err)
	}
	agents := doc.Profiles["p"].Agents
	if len(agents) != 3 || agents["jhon-scout"]["pi"] != "a/m" || agents["jhon-orchestrator"]["pi"] != "a/o" || agents["prism-arbiter"]["claude-code"] != "claude-x@high" {
		t.Fatalf("profiles after migrate: %#v", agents)
	}
	wantConfig := "schema: spec-driven # keep\nreview_required:\n  - change: verify-login\n    review: checker # owner\n  - area: \"src/**\"\n    review: [checker, \"prism\"]\n  - {change: c3, review: prism}\n  - area: docs\n    review:\n      - prism\n      - checker\ncontext: verify stays here\n"
	if got := support.Read(t, filepath.Join(proj, "openspec", "config.yaml")); got != wantConfig {
		t.Fatalf("config after migrate:\n%s\nwant:\n%s", got, wantConfig)
	}
	profilesBefore := support.Read(t, s.profiles)
	r = s.jhon(t, proj, "settings", "migrate")
	wantOut(t, r, 0, `nothing to migrate`)
	if strings.Contains(r.Out(), "rewritten") {
		t.Fatalf("a second migrate rewrote again:\n%s", r.Out())
	}
	if support.Read(t, s.profiles) != profilesBefore || support.Read(t, filepath.Join(proj, "openspec", "config.yaml")) != wantConfig {
		t.Fatal("a second migrate changed a file")
	}
}

// statusOf is `jhon status c1 --json` in proj.
func statusOf(t *testing.T, s scratch, proj string) (map[string]any, support.Result) {
	t.Helper()
	r := s.jhon(t, proj, "status", "c1", "--json")
	var out map[string]any
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		t.Fatalf("status --json: %v\n%s", err, r.Out())
	}
	return out, r
}

// ldd is a project under LDD with change c1, its config holding config.
func lddProject(t *testing.T, s scratch, config string, files map[string]string) string {
	t.Helper()
	proj := filepath.Join(s.home, "proj")
	all := map[string]string{"openspec/config.yaml": config, "openspec/changes/c1/tasks.md": "## Intent\n\nx\n\n- [ ] 1.1 x\n", "src/app.py": "X = 1\n"}
	for k, v := range files {
		all[k] = v
	}
	support.Write(t, proj, all)
	support.GitRepo(t, proj)
	support.Git(t, proj, "add", "-A")
	support.Git(t, proj, "commit", "-q", "-m", "base")
	return proj
}

// TestReviewRequiredNewAndOldValues: prism and checker are the review
// values; 4r and verify are read as them, with the warning in status's notes.
func TestReviewRequiredNewAndOldValues(t *testing.T) {
	s := newScratch(t, "")
	proj := lddProject(t, s, "schema: spec-driven\nreview_required:\n  - change: c1\n    review: [prism, checker]\n", nil)
	st, r := statusOf(t, s, proj)
	if !strings.Contains(r.Stdout, `"review":"prism"`) && !strings.Contains(r.Stdout, `"review": "prism"`) {
		t.Fatalf("status names no prism review:\n%s", r.Stdout)
	}
	if strings.Contains(r.Out(), "is now") {
		t.Fatalf("new values warned:\n%s", r.Out())
	}
	s2 := newScratch(t, "")
	proj2 := lddProject(t, s2, "schema: spec-driven\nreview_required:\n  - change: c1\n    review: [verify, 4r]\n", nil)
	st, r = statusOf(t, s2, proj2)
	notes, _ := json.Marshal(st["notes"])
	var list []string
	_ = json.Unmarshal(notes, &list)
	text := r.Stdout + "\n" + strings.Join(list, "\n")
	for _, p := range []string{`"review":\s*"prism"`, `"review":\s*"checker"`, warning("review_required line 4: review", "4r", "prism"), warning("review_required line 4: review", "verify", "checker")} {
		if !regexp.MustCompile(p).MatchString(text) {
			t.Fatalf("status --json does not match %q:\n%s", p, text)
		}
	}
	h := s2.jhon(t, proj2, "status", "c1")
	wantOut(t, h, 0, `(?m)^review\s+Prism review due \(review_required: change c1\)`, `(?m)^review\s+checker due`, warning("review_required line 4: review", "4r", "prism"))
}

// TestOldReviewRecordShownUnderNewNames: a review-4r.md written before the
// rename holds the Prism review, its lenses shown as facets.
func TestOldReviewRecordShownUnderNewNames(t *testing.T) {
	s := newScratch(t, "")
	proj := lddProject(t, s, "schema: spec-driven\nreview_required:\n  - change: c1\n    review: prism\n",
		map[string]string{"openspec/changes/c1/review-4r.md": "# 4R: c1\n\nlenses: risk, reliability\nverdict: pass\n"})
	_, r := statusOf(t, s, proj)
	if !strings.Contains(r.Stdout, `review-4r.md (facets: safety, behavior; verdict: pass)`) || !regexp.MustCompile(`"review":\s*"prism"[^}]*"done":\s*true`).MatchString(r.Stdout) {
		t.Fatalf("status does not hold the old record under the new names:\n%s", r.Stdout)
	}
}

// guard is a route-guard call in proj, by the owner's session, launching
// agent with prompt.
func guard(t *testing.T, s scratch, proj, agent, prompt string, args ...string) support.Result {
	t.Helper()
	payload := map[string]any{"tool_name": "Agent", "tool_input": map[string]any{"subagent_type": agent, "prompt": prompt}, "cwd": proj, "permission_mode": "default"}
	return support.Hook(t, s.env, "route-guard", payload, args...)
}

// TestRouteGuardNewAndOldNames: the bounded writer is jhon-builder under its
// new name and its old one, also as rendered before the rename (`worker`
// in the guard's arguments); a Prism facet nobody asked for is asked about
// under either name.
func TestRouteGuardNewAndOldNames(t *testing.T) {
	s := newScratch(t, "")
	proj := lddProject(t, s, "schema: spec-driven\n", nil)
	for _, c := range []struct{ rendered, launched string }{{"jhon-builder", "jhon-builder"}, {"jhon-builder", "worker"}, {"worker", "jhon-builder"}} {
		r := guard(t, s, proj, c.launched, "Fix it.", "--agent", "*", c.rendered)
		wantOut(t, r, 2, `route-guard: a jhon-builder is launched with its scope`)
		r = guard(t, s, proj, c.launched, "Fix it.\n\n## Allowed edit surfaces\n- src/app.py\n", "--agent", "*", c.rendered)
		wantOut(t, r, 0)
	}
	for _, agent := range []string{"prism-safety", "review-risk"} {
		r := guard(t, s, proj, agent, "x")
		wantOut(t, r, 0, `a prism-safety reviewer runs only when the owner asks for a Prism review`)
	}
}

// TestOldAuditRecordShownUnderNewName: a recorded session whose sub-agent
// ran as `worker` is reported under jhon-builder.
func TestOldAuditRecordShownUnderNewName(t *testing.T) {
	s := newScratch(t, "")
	dir := support.Fixture("audit", "pi-worker-refusal")
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	main := ""
	for _, e := range es {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			main = filepath.Join(dir, e.Name())
		}
	}
	r := s.jhon(t, "", "audit", main, "--json")
	if r.Code > 1 {
		t.Fatalf("audit exit %d:\n%s", r.Code, r.Out())
	}
	if !strings.Contains(r.Stdout, `"jhon-builder"`) || regexp.MustCompile(`"agent_type":\s*"worker"`).MatchString(r.Stdout) {
		t.Fatalf("the audit does not show the worker under jhon-builder:\n%s", r.Stdout)
	}
}
