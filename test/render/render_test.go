// Tests of the renderer, `jhon render-engine`.
//
// The renderer writes into $HOME. Every case therefore runs it in a child
// process whose HOME is a fresh temporary directory.
package render

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// renderIn runs the renderer on the tree at root, under a substituted HOME.
func renderIn(t testing.TB, home, root string, args ...string) support.Result {
	t.Helper()
	return support.Run(t, support.Cmd{
		Args: append([]string{support.Bin(t), "render-engine"}, args...),
		Env:  support.Env("HOME="+home, "JHON_ROOT="+root, support.RuntimesPath(t)),
	})
}

func render(t testing.TB, home string, args ...string) support.Result {
	t.Helper()
	return renderIn(t, home, repoRoot, args...)
}

// releaseName is the release name a test install of repo uses: a 40-hex id
// derived from its path.
func releaseName(repo string) string { return sha1Hex(repo) }

// install installs repo into home as `jhon sync` does (docket AV1): a copy as
// the release ~/.local/share/jhon/releases/<id>, `current` pointing at it, and
// the renderer run on that release with --install --source <repo>. Only a
// release installs; the renderer refuses to install from anything else.
func install(t testing.TB, home, repo string, args ...string) support.Result {
	t.Helper()
	share := filepath.Join(home, ".local", "share", "jhon")
	release := filepath.Join(share, "releases", releaseName(repo))
	os.RemoveAll(release)
	copyTree(t, repo, release)
	os.Remove(filepath.Join(share, "current"))
	if err := os.Symlink(release, filepath.Join(share, "current")); err != nil {
		t.Fatal(err)
	}
	return renderIn(t, home, release, append([]string{"--install", "--source", repo}, args...)...)
}

// hookPath is where an earlier release's hook commands ran a guard from.
func hookPath(home, script string) string {
	return filepath.Join(home, ".local", "share", "jhon", "current", "hooks", script)
}

// guardWords are the words of a rendered command that runs hook name: the
// active release's binary, `hook`, the name.
func guardWords(home, name string) []string {
	return []string{filepath.Join(home, ".local", "share", "jhon", "current", "build", "jhon"), "hook", name}
}

func quoted(words []string) string {
	q := make([]string, len(words))
	for i, w := range words {
		q[i] = "'" + w + "'"
	}
	return strings.Join(q, " ")
}

var guardNameRe = regexp.MustCompile(`^'[^']+' 'hook' '([\w-]+)'`)

// guardName is the hook a rendered command runs.
func guardName(command string) string {
	if m := guardNameRe.FindStringSubmatch(command); m != nil {
		return m[1]
	}
	return ""
}

// treePath is a path as the validator names it: relative to prose/, unless
// it names one of the tree's other top directories.
func treePath(root, rel string) string {
	first, _, _ := strings.Cut(rel, "/")
	if contains([]string{"engine", "prose", "runtimes", "test", "tools", "docs"}, first) {
		return filepath.Join(root, rel)
	}
	return filepath.Join(root, "prose", rel)
}

// editFile applies one literal edit (first occurrence) to path.
func editFile(t testing.TB, path, find, repl string) {
	t.Helper()
	src := support.Read(t, path)
	if !strings.Contains(src, find) {
		t.Fatalf("anchor not found in %s: %s", path, find)
	}
	write(t, path, strings.Replace(src, find, repl, 1))
}

// repoWithEdit is a repository copy with one literal edit applied to file.
func repoWithEdit(t testing.TB, file, find, repl string) string {
	t.Helper()
	repo := support.CopyRepo(t)
	editFile(t, treePath(repo, file), find, repl)
	return repo
}

// repoWithManifest is a repository copy whose agents.yaml has find replaced by repl.
func repoWithManifest(t testing.TB, find, repl string) string {
	t.Helper()
	repo := support.CopyRepo(t)
	editFile(t, filepath.Join(repo, "prose", "agents.yaml"), find, repl)
	return repo
}

func validate(t testing.TB, repo string) support.Result {
	t.Helper()
	return renderIn(t, t.TempDir(), repo, "--validate")
}

var rendered lazyHome

// renderedHome is a HOME with a full render in it, shared read-only.
func renderedHome(t *testing.T) string {
	t.Helper()
	return rendered.get(t, func(home string) string {
		if r := install(t, home, repoRoot); r.Code != 0 {
			return "the shared render failed:\n" + r.Out()
		}
		return ""
	})
}

func claudeAgent(t testing.TB, home, name string) string {
	t.Helper()
	return support.Read(t, filepath.Join(home, ".claude", "agents", name+".md"))
}

// frontmatter is the frontmatter of a rendered agent file, without its delimiters.
func frontmatter(md string) string {
	return strings.TrimPrefix(strings.Split(md, "\n---\n")[0], "---\n")
}

var ws = regexp.MustCompile(`\s+`)

func flat(s string) string { return ws.ReplaceAllString(s, " ") }

var nameSplit = regexp.MustCompile(`\n\s+-\s+name:\s*`)

func manifestSrc(t testing.TB) string {
	return support.Read(t, filepath.Join(repoRoot, "prose", "agents.yaml"))
}

// manifestAgents are the names declared in agents.yaml, split by whether the
// agent is primary.
func manifestAgents(t testing.TB) (primary, delegable []string) {
	t.Helper()
	isPrimary := regexp.MustCompile(`\n\s+mode:\s*primary\b`)
	for _, b := range nameSplit.Split(manifestSrc(t), -1)[1:] {
		name := strings.TrimSpace(strings.Split(b, "\n")[0])
		if isPrimary.MatchString(b) {
			primary = append(primary, name)
		} else {
			delegable = append(delegable, name)
		}
	}
	return
}

func allManifestAgents(t testing.TB) []string {
	t.Helper()
	var out []string
	for _, b := range nameSplit.Split(manifestSrc(t), -1)[1:] {
		out = append(out, strings.TrimSpace(strings.Split(b, "\n")[0]))
	}
	return out
}

// manifestCaps are the caps an agent declares in agents.yaml.
func manifestCaps(t testing.TB, name string) []string {
	t.Helper()
	for _, b := range nameSplit.Split(manifestSrc(t), -1) {
		if strings.TrimSpace(strings.Split(b, "\n")[0]) != name {
			continue
		}
		m := regexp.MustCompile(`\n\s+caps:\s*\[(.*)\]`).FindStringSubmatch(b)
		if m == nil {
			t.Fatalf("%s declares no caps", name)
		}
		return splitList(m[1])
	}
	t.Fatalf("no agent %s in agents.yaml", name)
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		out = append(out, strings.TrimSpace(x))
	}
	return out
}

// vcsDirs are the VCS directories no agent may write (contracts/vocabulary.yaml, `vcs_dirs`).
func vcsDirs(t testing.TB) []string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^vcs_dirs:\s*\[(.*)\]`).FindStringSubmatch(support.Read(t, filepath.Join(repoRoot, "prose", "contracts", "vocabulary.yaml")))
	if m == nil {
		return []string{}
	}
	out := []string{}
	for _, s := range splitList(m[1]) {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

var (
	toolsLine = regexp.MustCompile(`(?m)^tools: (.+)$`)
	listItem  = regexp.MustCompile(`(?m)^  - (.+)$`)
)

// claudeTools are an agent's Claude Code tools, from either tools field shape.
func claudeTools(fm string) []string {
	if m := toolsLine.FindStringSubmatch(fm); m != nil {
		return splitList(m[1])
	}
	return listItems(fm)
}

func listItems(fm string) []string {
	var out []string
	for _, m := range listItem.FindAllStringSubmatch(fm, -1) {
		out = append(out, m[1])
	}
	return out
}

// lineTools is the one-line tools field.
func lineTools(t testing.TB, fm string) []string {
	t.Helper()
	m := toolsLine.FindStringSubmatch(fm)
	if m == nil {
		t.Fatalf("no one-line tools field:\n%s", fm)
	}
	return splitList(m[1])
}

type hookEntry struct{ matcher, command string }

var preToolUseRe = regexp.MustCompile(`(?m)^    - matcher: "(.+)"\n      hooks:\n        - type: command\n          command: (".+")$`)

// preToolUse are the PreToolUse entries of a rendered frontmatter.
// preToolUse is the frontmatter's PreToolUse guards, one entry per guard as
// the runtime applies them: the one `jhon hook pre-tool` entry (docket AY
// B4) expanded into each guard it runs, with the matcher pre-tool gives that
// guard and the command that runs it alone.
func preToolUse(t testing.TB, fm string) []hookEntry {
	t.Helper()
	var out []hookEntry
	for _, h := range hookSection(t, fm, "PreToolUse") {
		if guardName(h.command) != "pre-tool" {
			out = append(out, h)
			continue
		}
		args := strings.Split(h.command[1:len(h.command)-1], "' '")
		head := []string{args[0], "hook"}
		owner := args[3:5]
		var cur []string
		flush := func() {
			if cur != nil {
				out = append(out, hookEntry{preToolMatchers(t)[cur[0]], quoted(append(append(append([]string{}, head...), cur[0]), append(append([]string{}, owner...), cur[1:]...)...))})
			}
		}
		for _, a := range args[5:] {
			if _, guard := preToolMatchers(t)[a]; guard {
				flush()
				cur = []string{a}
			} else {
				cur = append(cur, a)
			}
		}
		flush()
	}
	return out
}

// preToolMatchers are the tools pre-tool applies each guard to, as the
// binary states them (`jhon hook pre-tool --matchers`): the suite holds the
// render to the engine's one table rather than to a copy of it.
func preToolMatchers(t testing.TB) map[string]string {
	t.Helper()
	matchersOnce.Do(func() {
		out, err := exec.Command(support.Bin(t), "hook", "pre-tool", "--matchers").Output()
		if err != nil {
			t.Fatalf("jhon hook pre-tool --matchers: %v", err)
		}
		matchers = map[string]string{}
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if name, tools, ok := strings.Cut(l, "\t"); ok {
				matchers[name] = tools
			}
		}
	})
	return matchers
}

var (
	matchersOnce sync.Once
	matchers     map[string]string
)

// hookSection is the entries of one hook event in the frontmatter's
// `hooks:`, up to the next event.
func hookSection(t testing.TB, fm, event string) []hookEntry {
	t.Helper()
	at := strings.Index(fm, "\n  "+event+":\n")
	if at < 0 {
		return nil
	}
	fm = fm[at+len(event)+4:]
	if end := regexp.MustCompile(`\n  [A-Za-z]+:\n`).FindStringIndex(fm); end != nil {
		fm = fm[:end[0]]
	}
	var out []hookEntry
	for _, m := range preToolUseRe.FindAllStringSubmatch(fm, -1) {
		var c string
		if err := json.Unmarshal([]byte(m[2]), &c); err != nil {
			t.Fatalf("command %s: %v", m[2], err)
		}
		out = append(out, hookEntry{m[1], c})
	}
	return out
}

func hookPairs(hs []hookEntry) [][2]string {
	out := [][2]string{}
	for _, h := range hs {
		out = append(out, [2]string{h.matcher, guardName(h.command)})
	}
	return out
}

// guardsOf are the entries of an agent that run the guard words(home).
func guardsOf(t testing.TB, home, name string, words []string) []hookEntry {
	t.Helper()
	var out []hookEntry
	for _, h := range preToolUse(t, frontmatter(claudeAgent(t, home, name))) {
		if strings.HasPrefix(h.command, quoted(words)+" ") {
			out = append(out, h)
		}
	}
	return out
}

func fmField(md, key string) (string, bool) {
	m := regexp.MustCompile(`(?m)^` + key + `: (.+)$`).FindStringSubmatch(frontmatter(md))
	if m == nil {
		return "", false
	}
	return m[1], true
}

// renderedAgents are the names of the agent files in home's ~/.claude/agents.
func renderedAgents(t testing.TB, home string) []string {
	t.Helper()
	des, err := os.ReadDir(filepath.Join(home, ".claude", "agents"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, d := range des {
		if strings.HasSuffix(d.Name(), ".md") {
			out = append(out, strings.TrimSuffix(d.Name(), ".md"))
		}
	}
	return out
}

func readDirNames(t testing.TB, dir string) []string {
	t.Helper()
	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, d := range des {
		out = append(out, d.Name())
	}
	return out
}

// walkFiles is every regular file under dir.
func walkFiles(t testing.TB, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRenderArguments(t *testing.T) {
	t.Parallel()

	t.Run("a full render writes Claude agents and nothing for OpenCode", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		r := install(t, home, repoRoot)
		wantCode(t, r, 0)
		es := entries(t, home)
		if !contains(es, filepath.Join(".claude", "agents", "jhon-orchestrator.md")) {
			t.Error(r.Out())
		}
		noMatch(t, r.Out(), `(?i)opencode`)
		for _, e := range es {
			if strings.HasPrefix(e, filepath.Join(".config", "opencode")) {
				t.Errorf("an OpenCode entry: %s", e)
			}
		}
	})

	// The OpenCode runtime was dropped on 2026-10-03 (docket AG): an installed
	// OpenCode configuration is neither written nor pruned, and its leftovers
	// are the user's to remove.
	t.Run("a render and a check leave an installed OpenCode configuration untouched", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		dir := filepath.Join(home, ".config", "opencode")
		cfg := `{"agent":{"general":{"__managed_by":"jhon","permission":{"task":"allow"}}}}` + "\n"
		write(t, filepath.Join(dir, "opencode.json"), cfg)
		for _, args := range [][]string{{}, {"--check"}, {"--validate"}} {
			var r support.Result
			if len(args) > 0 {
				r = render(t, home, args...)
			} else {
				r = install(t, home, repoRoot)
			}
			if !(r.Code == 0 || (len(args) > 0 && args[0] == "--check" && r.Code == 1)) {
				t.Fatalf("%v: exit %d\n%s", args, r.Code, r.Out())
			}
			noMatch(t, r.Out(), `(?i)opencode`)
		}
		equal(t, readDirNames(t, dir), []string{"opencode.json"})
		equal(t, support.Read(t, filepath.Join(dir, "opencode.json")), cfg)
	})

	// An argument the renderer does not recognise used to be ignored, so
	// `--help` or a typo ran a full write render into $HOME. Every refusal below
	// must happen before validation and before any write.
	for _, flag := range []string{"--help", "-h"} {
		t.Run(flag+" prints usage on stdout, exits 0, and writes nothing", func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			r := render(t, home, flag)
			wantCode(t, r, 0)
			match(t, r.Stdout, `usage:`)
			noMatch(t, r.Stdout, `validation:|claude-code:`)
			equal(t, len(entries(t, home)), 0)
		})
	}

	for _, argv := range [][]string{
		{"--bogus"},
		{"--only"},
		{"--only", "nope"},
		{"--print"},
		{"--print", "--check"},
		{"stray"},
		{"--check", "stray"},
		{"--only", "claude-code", "--only", "claude-code"},
		{"--only", "opencode"},
	} {
		t.Run(fmt.Sprintf("refuses %s with usage on stderr and writes nothing", mustJSON(argv)), func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			r := render(t, home, argv...)
			wantCode(t, r, 2)
			match(t, r.Stderr, `usage:`)
			equal(t, r.Stdout, "")
			equal(t, len(entries(t, home)), 0)
		})
	}

	t.Run("--check writes nothing", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		r := render(t, home, "--check")
		wantCode(t, r, 1) // nothing rendered yet, so everything drifts
		equal(t, len(entries(t, home)), 0)
	})

	t.Run("--print shows the whole frontmatter and writes nothing", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		r := render(t, home, "--print", "review-risk")
		wantCode(t, r, 0)
		match(t, r.Out(), `'hook' 'pre-tool' '--agent' 'review-risk' 'inspect-guard'`)
		equal(t, len(entries(t, home)), 0)
	})
}

// Jhon's skills are read from the active release, never from a global skills
// directory: every agent and command names its skill by its path under
// ~/.local/share/jhon/current/skills. ~/.agents/skills (which ~/.claude/skills
// may link to) is the user's, and a session that runs no Jhon agent sees no
// Jhon skill. The install removes the skills an earlier release projected
// there, as its .jhon-owned record names them, and nothing else.

// homeWithProjectedSkills is a HOME whose ~/.agents/skills holds what an
// earlier release projected, and a foreign skill.
func homeWithProjectedSkills(t testing.TB) (home, skills string) {
	t.Helper()
	home = t.TempDir()
	skills = filepath.Join(home, ".agents", "skills")
	for _, s := range []string{"old-init", "judgment-day", "foreign"} {
		write(t, filepath.Join(skills, s, "SKILL.md"), s+"\n")
	}
	write(t, filepath.Join(skills, ".jhon-owned"), "judgment-day\nold-init\n")
	return
}

func TestRenderSkills(t *testing.T) {
	t.Parallel()

	t.Run("the install projects no skill, removes the ones an earlier release owned, and keeps foreign skills", func(t *testing.T) {
		t.Parallel()
		home, skills := homeWithProjectedSkills(t)
		wantCode(t, install(t, home, repoRoot), 0)
		for _, s := range []string{"old-init", "judgment-day"} {
			equal(t, exists(filepath.Join(skills, s)), false, s)
		}
		equal(t, exists(filepath.Join(skills, ".jhon-owned")), false, "the record outlived its skills")
		equal(t, support.Read(t, filepath.Join(skills, "foreign", "SKILL.md")), "foreign\n")
		equal(t, exists(filepath.Join(skills, "ldd")), false, "a skill was projected")
	})

	t.Run("--check reports an owned skill still installed as drift and removes nothing", func(t *testing.T) {
		t.Parallel()
		home, skills := homeWithProjectedSkills(t)
		r := render(t, home, "--check")
		wantCode(t, r, 1)
		match(t, r.Out(), `DRIFT.*\.agents/skills/judgment-day`)
		if !exists(filepath.Join(skills, "judgment-day", "SKILL.md")) {
			t.Error("the owned skill was removed")
		}
	})

	t.Run("every agent and command names its skill by its path in the active release", func(t *testing.T) {
		t.Parallel()
		home := renderedHome(t)
		match(t, flat(claudeAgent(t, home, "worker")), `~/\.local/share/jhon/current/prose/skills/ldd/references/apply\.md`)
		match(t, flat(support.Read(t, filepath.Join(repoRoot, "runtimes", "claude-code", "plugin", "skills", "jhon-ldd", "SKILL.md"))), `~/\.local/share/jhon/current/prose/skills/ldd/references/verify\.md`)
		for _, dir := range []string{filepath.Join(home, ".claude", "agents"), filepath.Join(repoRoot, "runtimes", "claude-code", "plugin"), filepath.Join(repoRoot, "prose", "skills")} {
			for _, f := range walkFiles(t, dir) {
				noMatch(t, support.Read(t, f), `~/\.(claude|agents)/skills/`, f)
			}
		}
	})

}

func TestRenderValidateRules(t *testing.T) {
	t.Parallel()

	// Runtime boundaries. A sub-agent refused ten times by the worktree
	// isolation guard ran `/usr/bin/git -C <worktree>` to get past it, and
	// saved that as a technique in Engram. Each edit removes one core sentence
	// of the rule that forbids this, or the contract from the always-on layer,
	// and must fail the runtime-boundaries rule on the edited file. The
	// always-on layer is compiled into the renderer; internal/render's own test
	// holds the rule there.
	for _, c := range []struct{ name, file, find, repl string }{
		{"the contract without the stop rule", "contracts/runtime-boundaries.md",
			"**A safety refusal is a stop.**", "**A safety refusal is a hint.**"},
		{"the contract without the verbatim report", "contracts/runtime-boundaries.md",
			"Report it verbatim;", "Summarise it;"},
		{"the contract without the same-effect clause", "contracts/runtime-boundaries.md",
			"**Never reach the same effect another way.** The criterion is the effect,\n  not the command", "**Try another command.**"},
		{"the contract without the delegation clause", "contracts/runtime-boundaries.md",
			"a delegate asked to do what\n  the refusal prevented", "a delegate"},
		{"the contract without the refusal-versus-failure clause", "contracts/runtime-boundaries.md",
			"**A refusal is not a failure.**", "**A refusal is a failure.**"},
		{"the contract without the no-technique clause", "contracts/runtime-boundaries.md",
			"**Record the refusal, never the way around it**", "**Record the refusal and the way around it**"},
		// Docket AA5: a form the refusing authority itself allows is compliance.
		{"the contract without the allowed-form clause", "contracts/runtime-boundaries.md",
			"using the allowed form is compliance", "using the allowed form is optional"},
		{"the contract without the owner's resolution", "contracts/runtime-boundaries.md",
			"**The owner resolves it**", "**The agent resolves it**"},
		{"the orchestrator without the no-bypass rule for executor refusals", "orchestrator.md",
			"Never perform,\n  propose, or instruct a bypass, including by re-delegating the same effect\n  to another agent.", ""},
	} {
		t.Run(c.name+" fails the runtime-boundaries rule", func(t *testing.T) {
			t.Parallel()
			r := validate(t, repoWithEdit(t, c.file, c.find, c.repl))
			wantCode(t, r, 1)
			match(t, r.Out(), `runtime-boundaries\s+`+regexp.QuoteMeta(c.file))
		})
	}

	// Audit registry. contracts/vocabulary.yaml `audit_rules` maps every
	// behavioural anchor to the transcript check that looks for a breach of
	// it, or to `none` with a reason; the checks it names must be exactly the
	// ones internal/audit exports. Each edit breaks one side of that and must
	// fail the audit-registry rule on the vocabulary.
	const iso = "    check: isolation-bypass\n    evidence_class: deterministic\n    validation_status: verified\n"
	for _, c := range []struct{ name, find, repl, why string }{
		{"an anchor with no entry",
			"  - anchor: runtime-boundaries/no-technique\n    check: none\n", "  - anchor: runtime-boundaries/removed\n    check: none\n",
			`runtime-boundaries/no-technique has no audit_rules entry`},
		{"an entry naming a check internal/audit does not export",
			"    check: isolation-bypass\n", "    check: isolation-quoted\n", `isolation-quoted is not a check internal/audit exports`},
		{"an exported check no entry names",
			iso + "    real_clean: [general-isolation-stop]\n    real_violation: [apply-worktree-bypass]\n", "    check: none\n    reason: moved elsewhere\n",
			`isolation-bypass is exported by internal/audit and named by no entry`},
		{"a none entry without a reason",
			"    reason: it defines which errors are refusals; refusal-bypass applies the definition and does not test it\n", "",
			`runtime-boundaries/refusal-not-failure: check none needs a reason`},
		{"an entry with an evidence class outside deterministic and heuristic",
			iso, strings.Replace(iso, "deterministic", "probable", 1), `isolation/no-other-tree: evidence_class must be deterministic or heuristic`},
		{"the delivery anchor with no entry",
			"  - anchor: delivery/tested-revision\n", "  - anchor: delivery/removed\n", `delivery/tested-revision has no audit_rules entry`},
		{"the delivered-untested check named by no entry",
			"    check: delivered-untested\n    evidence_class: deterministic\n    validation_status: verified\n    real_clean: [background-run-delivered]\n    real_violation: [background-run-failed-delivered]\n  - anchor: delivery/same-tree\n    check: delivered-untested\n    evidence_class: heuristic\n  - anchor: delivery/high-risk-verified\n    check: delivered-untested\n    evidence_class: heuristic\n",
			"    check: none\n    reason: moved elsewhere\n  - anchor: delivery/same-tree\n    check: none\n    reason: moved elsewhere\n  - anchor: delivery/high-risk-verified\n    check: none\n    reason: moved elsewhere\n",
			`delivered-untested is exported by internal/audit and named by no entry`},
		// A deterministic entry declares validation_status, and `verified`
		// names real fixture directories both ways, each existing under
		// test/testdata/audit/ and marked real.
		{"a deterministic entry without validation_status",
			iso, strings.Replace(iso, "    validation_status: verified\n", "", 1), `isolation/no-other-tree: a deterministic check declares validation_status`},
		{"a validation_status outside audit_validation_status",
			iso, strings.Replace(iso, "verified", "proven", 1), `isolation/no-other-tree: validation_status must be one of verified\|pending-real-fixture, found 'proven'`},
		{"a verified entry without a real violation fixture",
			"    real_violation: [apply-worktree-bypass]\n", "", `isolation/no-other-tree: verified needs real_clean and real_violation`},
		{"a real fixture that is not a directory under test/testdata/audit",
			"    real_violation: [apply-worktree-bypass]\n", "    real_violation: [apply-nowhere]\n",
			`isolation/no-other-tree: apply-nowhere is not a fixture directory under test/testdata/audit`},
		{"an entry naming a synthetic directory as a real fixture",
			"    real_clean: [general-isolation-stop]\n", "    real_clean: [clean]\n", `isolation/no-other-tree: clean is not marked real \(source\.json\)`},
		{"an entry still using real_positive",
			"    real_violation: [apply-worktree-bypass]\n", "    real_violation: [apply-worktree-bypass]\n    real_positive: [apply-bypass-archive]\n",
			`isolation/no-other-tree: real_positive and real_negative are renamed real_clean and real_violation`},
		{"the allowed-form anchor with no entry",
			"  - anchor: runtime-boundaries/allowed-form\n", "  - anchor: runtime-boundaries/removed-form\n",
			`runtime-boundaries/allowed-form has no audit_rules entry`},
		{"an entry for an anchor the validator does not enumerate",
			"  - anchor: ldd/slices\n", "  - anchor: ldd/bogus\n    check: none\n    reason: x\n  - anchor: ldd/slices\n",
			`ldd/bogus is not a behavioural anchor`},
	} {
		t.Run(c.name+" fails the audit-registry rule", func(t *testing.T) {
			t.Parallel()
			r := validate(t, repoWithEdit(t, "contracts/vocabulary.yaml", c.find, c.repl))
			wantCode(t, r, 1)
			match(t, r.Out(), `audit-registry\s+contracts/vocabulary\.yaml`)
			match(t, r.Out(), c.why)
		})
	}

	t.Run("a real fixture whose source.json does not say real fails the audit-registry rule", func(t *testing.T) {
		t.Parallel()
		r := validate(t, repoWithEdit(t, "test/testdata/audit/apply-worktree-bypass/source.json", `"real": true`, `"real": false`))
		wantCode(t, r, 1)
		match(t, r.Out(), `audit-registry\s+contracts/vocabulary\.yaml\s+.*apply-worktree-bypass is not marked real`)
	})

	t.Run("a pre-commit gate that is not executable fails the executable rule", func(t *testing.T) {
		t.Parallel()
		repo := support.CopyRepo(t)
		if err := os.Chmod(filepath.Join(repo, "tools", "git-hooks", "pre-commit"), 0o644); err != nil {
			t.Fatal(err)
		}
		r := validate(t, repo)
		wantCode(t, r, 1)
		match(t, r.Out(), `executable\s+.*hooks/pre-commit`)
	})

	// Safety (docket AH1, re-pointed). Repository-state operations are any
	// writer's; the safety guard refuses destructive ones and asks for
	// outward-facing ones; a runtime isolation refusal is a stop, not a reason
	// to make another tree. Each edit removes one clause and must fail the
	// safety rule on the edited file.
	for _, c := range []struct{ name, file, find, repl string }{
		{"the orchestrator without the safety guard's role", "orchestrator.md",
			"`jhon hook safety-guard` refuses irreversible\n  operations to every agent and to this thread", "`jhon hook safety-guard` advises"},
		{"the orchestrator without the owner's approval of a deletion", "orchestrator.md",
			"runs once the\n  owner approves it (class `destroy`)", "runs on its own"},
		{"the orchestrator without the credentials rule", "orchestrator.md",
			"No agent reads, edits, or deletes a credentials file", "An agent may read a credentials file"},
		{"the shared steps restricting repository state to one agent", "contracts/ldd.md",
			"Any agent that holds Bash may perform repository-state operations locally", "Only one agent performs repository-state operations"},
		{"the shared steps without the safety guard's refusals", "contracts/ldd.md",
			"`jhon hook safety-guard`\nrefuses irreversible operations to every agent and the top-level session", "`jhon hook safety-guard` reviews operations"},
		{"the shared steps without the outward-facing ask", "contracts/ldd.md", "It asks for the outward-facing ones", "It allows the outward-facing ones"},
		{"the shared steps allowing the refused write in another tree", "contracts/ldd.md", "making the refused write there is a\nbypass", "making the refused write there is allowed"},
		{"the apply reference making another worktree after an isolation refusal", "skills/ldd/references/apply.md", "never create or select another worktree", "create another worktree if needed"},
	} {
		t.Run(c.name+" fails the safety rule", func(t *testing.T) {
			t.Parallel()
			r := validate(t, repoWithEdit(t, c.file, c.find, c.repl))
			wantCode(t, r, 1)
			match(t, r.Out(), `safety\s+`+regexp.QuoteMeta(c.file))
		})
	}

	for _, anchor := range []string{"isolation/no-other-tree", "delivery/same-tree", "safety/destructive-deny", "safety/outward-ask"} {
		t.Run("the "+anchor+" anchor with no audit_rules entry fails the audit-registry rule", func(t *testing.T) {
			t.Parallel()
			r := validate(t, repoWithEdit(t, "contracts/vocabulary.yaml", "  - anchor: "+anchor+"\n", "  - anchor: vcs/removed\n"))
			wantCode(t, r, 1)
			match(t, r.Out(), `audit-registry\s+contracts/vocabulary\.yaml\s+`+anchor+` has no audit_rules entry`)
		})
	}
}

var inspectPrefixes = []string{"git diff", "git show", "git log", "git status", "codegraph callers", "codegraph impact", "codegraph affected", "jhon review freeze"}

func TestRenderAgentTools(t *testing.T) {
	t.Parallel()

	// Docket AY B4: an agent's guards are one process per call. Its
	// frontmatter holds exactly one PreToolUse entry, `jhon hook pre-tool`,
	// whose matcher is the union of the tools its guards take, or every tool
	// (`*`) when one of them, a capped agent's turn-guard, takes every tool.
	t.Run("every agent's guards run as one pre-tool entry", func(t *testing.T) {
		t.Parallel()
		home := renderedHome(t)
		for _, name := range renderedAgents(t, home) {
			raw := hookSection(t, frontmatter(claudeAgent(t, home, name)), "PreToolUse")
			if len(raw) != 1 || guardName(raw[0].command) != "pre-tool" {
				t.Errorf("%s: %+v", name, raw)
				continue
			}
			tools := map[string]bool{}
			for _, g := range preToolUse(t, frontmatter(claudeAgent(t, home, name))) {
				for _, tool := range strings.Split(g.matcher, "|") {
					tools[tool] = true
				}
			}
			if tools["*"] {
				equal(t, raw[0].matcher, "*", name+": a guard takes every tool")
				continue
			}
			for _, tool := range strings.Split(raw[0].matcher, "|") {
				if !tools[tool] {
					t.Errorf("%s: the matcher takes %s, which none of its guards judges", name, tool)
				}
				delete(tools, tool)
			}
			if len(tools) > 0 {
				t.Errorf("%s: the matcher misses %v", name, tools)
			}
		}
	})

	// S35 (L138): workers read 14.9 MB of shell output and 0 bytes of
	// codegraph. In both runtimes the worker holds codegraph and is told to
	// use it first; the worker and verify bound their command output.
	// T131 (L905, L908): no turn cap; a slice fits the context budget, and
	// the worker and explore return at their context notice.
	t.Run("the worker locates code with codegraph, bounds its command output, and checkpoints at its context notice", func(t *testing.T) {
		t.Parallel()
		home := renderedHome(t)
		_, pi := sharedPiHome(t)
		if !contains(claudeTools(frontmatter(claudeAgent(t, home, "worker"))), "mcp__codegraph__codegraph_explore") {
			t.Error("the Claude Code worker lacks codegraph_explore")
		}
		if !contains(listItems(frontmatter(support.Read(t, filepath.Join(pi, "agents", "worker.md")))), "mcp__codegraph__codegraph_explore") {
			t.Error("the pi worker lacks codegraph_explore")
		}
		for _, rt := range []struct{ label, dir string }{{"claude-code", filepath.Join(home, ".claude", "agents")}, {"pi", filepath.Join(pi, "agents")}} {
			worker := flat(support.Read(t, filepath.Join(rt.dir, "worker.md")))
			match(t, worker, `Use codegraph_explore before grep/find to locate code\.`, rt.label)
			match(t, worker, `Your slice is sized to fit your context budget; at the context notice, checkpoint: stop and return what is done, the partial state, and what is left, so the orchestrator can hand the rest to a fresh worker\.`, rt.label)
			explore := flat(support.Read(t, filepath.Join(rt.dir, "explore.md")))
			match(t, explore, "Cite `file:line` for every claim\\. At the context notice, return what you found\\.", rt.label)
			for name, body := range map[string]string{"worker": worker, "explore": explore} {
				noMatch(t, body, `(?i)turn cap|before the cap|\d+ turns`, rt.label+" "+name)
			}
			// The worker's tests are a unit's run, never by hand (T133); the
			// verifier checks the slice or the change, so its run names a base.
			for name, run := range map[string]string{
				"worker": "Run tests through `jhon executions run <change> --affected` \\(the unit's run\\), never by hand; pipe",
				"verify": "Run tests through `jhon executions run <change> --affected <base>` or with quiet flags; pipe",
			} {
				body := flat(support.Read(t, filepath.Join(rt.dir, name+".md")))
				match(t, body, run, rt.label+" "+name)
				if name == "worker" {
					noMatch(t, body, `or with quiet flags`, rt.label+" "+name)
				}
				match(t, body, "pipe long output through `\\| tail -n 40` or a filter, and read only the failures", rt.label+" "+name)
			}
		}
	})

	t.Run("an inspect agent's Claude Code frontmatter runs the inspect guard with the allowlist", func(t *testing.T) {
		t.Parallel()
		home := renderedHome(t)
		fm := frontmatter(claudeAgent(t, home, "review-risk"))
		inspect := guardsOf(t, home, "review-risk", guardWords(home, "inspect-guard"))
		if len(inspect) != 1 || inspect[0].matcher != "Bash" {
			t.Fatal(fm)
		}
		command := inspect[0].command
		if !strings.HasPrefix(command, quoted(guardWords(home, "inspect-guard"))+" '--agent' 'review-risk' ") {
			t.Error(command)
		}
		for _, p := range inspectPrefixes {
			if !strings.Contains(command, " '"+p+"'") {
				t.Errorf("%s missing from %s", p, command)
			}
		}
	})

	// Item 5: sessions run as jhon-orchestrator never resumed an executor (0
	// SendMessage, 23 Agent launches in abc-002-1, the apply relaunched 8
	// times) because the tool was not in their allowlist; plain sessions resumed.
	t.Run("the orchestrator may resume an executor with SendMessage; no other agent holds it", func(t *testing.T) {
		t.Parallel()
		home := renderedHome(t)
		if !contains(claudeTools(frontmatter(claudeAgent(t, home, "jhon-orchestrator"))), "SendMessage") {
			t.Error("the orchestrator lacks SendMessage")
		}
		for _, name := range renderedAgents(t, home) {
			if name == "jhon-orchestrator" {
				continue
			}
			if contains(claudeTools(frontmatter(claudeAgent(t, home, name))), "SendMessage") {
				t.Error(name)
			}
		}
		match(t, flat(claudeAgent(t, home, "jhon-orchestrator")), "continue it with `SendMessage`", "the orchestrator's body states the resume rule")
	})

	t.Run("the orchestrator may ask, write, and invoke every delegable agent but itself", func(t *testing.T) {
		t.Parallel()
		fm := frontmatter(claudeAgent(t, renderedHome(t), "jhon-orchestrator"))
		tools := listItems(fm)
		match(t, fm, `(?m)^tools:\n  - `, "a tools list containing commas must render as a YAML block list")
		for _, x := range []string{"AskUserQuestion", "ExitPlanMode", "Edit", "Write"} {
			if !contains(tools, x) {
				t.Errorf("%s missing: %v", x, tools)
			}
		}
		i := slices.IndexFunc(tools, func(s string) bool { return strings.HasPrefix(s, "Agent(") })
		if i < 0 {
			t.Fatalf("no Agent(...) entry: %v", tools)
		}
		entry := tools[i]
		invocable := splitList(entry[len("Agent(") : len(entry)-1])
		primary, delegable := manifestAgents(t)
		equal(t, sorted(invocable), sorted(delegable))
		for _, p := range primary {
			if contains(invocable, p) {
				t.Errorf("%s is primary and must not be invocable", p)
			}
		}
	})

	// Docket AE1: in Claude Code's plan mode the orchestrator must write its
	// plan file and leave the mode with ExitPlanMode.
	t.Run("ExitPlanMode is a Claude Code tool of the orchestrator only", func(t *testing.T) {
		t.Parallel()
		home := renderedHome(t)
		for _, name := range renderedAgents(t, home) {
			fm := frontmatter(claudeAgent(t, home, name))
			equal(t, regexp.MustCompile(`\bExitPlanMode\b`).MatchString(fm), name == "jhon-orchestrator", name)
		}
	})

	// The orchestrator reads, writes, and runs Bash itself; only the safety
	// layer guards it: the safety guard on Bash, the write guard on
	// Edit/Write/NotebookEdit, the framework guard, the secrets guard on
	// Read/Grep and writes, and the route guard on writes, Bash and Agent, the
	// only calls it acts on (S28; writes since T89, L646, for the first code
	// edit in a project without an LDD layout). No allowlist, no write list.
	t.Run("the orchestrator holds Read, Edit, Write, and Bash, guarded only by the safety layer", func(t *testing.T) {
		t.Parallel()
		fm := frontmatter(claudeAgent(t, renderedHome(t), "jhon-orchestrator"))
		tools := claudeTools(fm)
		for _, x := range []string{"Read", "Edit", "Write", "Bash", "AskUserQuestion"} {
			if !contains(tools, x) {
				t.Errorf("%s missing: %v", x, tools)
			}
		}
		equal(t, hookPairs(preToolUse(t, fm)), [][2]string{
			{"Bash", "safety-guard"}, {"Edit|MultiEdit|Write|NotebookEdit", "vcs-guard"}, {"Edit|MultiEdit|Write|Bash", "framework-guard"},
			{"Read|Grep|Glob|Edit|MultiEdit|Write|NotebookEdit", "secrets-guard"},
			{"Edit|MultiEdit|Write|NotebookEdit|Bash|Agent", "route-guard"},
		})
	})

	// A main session's frontmatter hooks also fire for the sub-agents it
	// launches, so a guard rendered without its owner would judge every
	// sub-agent's call by the main agent's lists. Every guard therefore names
	// the agent it belongs to.
	// Docket AY B2: style is never the agent's call. Every agent that writes
	// has its edits rewritten by the project's formatter; one that cannot
	// write has no such hook.
	t.Run("a writer's edits pass through format-edit, named for the writer", func(t *testing.T) {
		t.Parallel()
		home := renderedHome(t)
		for _, name := range renderedAgents(t, home) {
			fm := frontmatter(claudeAgent(t, home, name))
			post := hookSection(t, fm, "PostToolUse")
			writes := regexp.MustCompile(`(?m)^tools:.*\b(Edit|Write)\b|^  - (Edit|Write)$`).MatchString(fm)
			if !writes {
				equal(t, len(post), 0, name+" cannot write")
				continue
			}
			equal(t, hookPairs(post), [][2]string{{"Edit|Write", "format-edit"}}, name)
			match(t, post[0].command, `^'[^']+/build/jhon' 'hook' 'format-edit' '--agent' '`+regexp.QuoteMeta(name)+`'$`, name)
		}
	})

	t.Run("every rendered guard names its own agent right after the script", func(t *testing.T) {
		t.Parallel()
		home := renderedHome(t)
		guarded := 0
		for _, name := range renderedAgents(t, home) {
			for _, h := range preToolUse(t, frontmatter(claudeAgent(t, home, name))) {
				guarded++
				match(t, h.command, `^'[^']+/build/jhon' 'hook' '[a-z]+-guard' `, name)
				args := strings.Split(h.command[1:len(h.command)-1], "' '")
				equal(t, args[3:5], []string{"--agent", name}, name+": "+h.command)
			}
		}
		if guarded < 10 {
			t.Errorf("only %d guarded hooks rendered", guarded)
		}
	})

	t.Run("a bounded writer carries scope-guard on its writes and Bash; no other agent does", func(t *testing.T) {
		t.Parallel()
		home := renderedHome(t)
		for _, name := range []string{"worker", "jd-fix-agent"} {
			var h []hookEntry
			for _, x := range preToolUse(t, frontmatter(claudeAgent(t, home, name))) {
				if guardName(x.command) == "scope-guard" {
					h = append(h, x)
				}
			}
			if len(h) != 1 {
				t.Fatalf("%s: %d scope guards", name, len(h))
			}
			equal(t, h[0].matcher, "Edit|MultiEdit|Write|NotebookEdit|Bash", name)
		}
		for _, name := range []string{"jhon-orchestrator", "verify", "explore", "review-risk"} {
			noMatch(t, frontmatter(claudeAgent(t, home, name)), `scope-guard`, name)
		}
	})

	t.Run("an agent whose tools contain no comma keeps the one-line tools field", func(t *testing.T) {
		t.Parallel()
		match(t, frontmatter(claudeAgent(t, renderedHome(t), "explore")), `(?m)^tools: Read, Grep, Glob, `)
	})

	// verify must write verify-report.md into the change directory, and the
	// archive gate reads nothing else. Without the write capability the phase
	// can only return the report as text, so no change could ever reach archive.
	t.Run("verify may write its report", func(t *testing.T) {
		t.Parallel()
		tools := lineTools(t, frontmatter(claudeAgent(t, renderedHome(t), "verify")))
		for _, x := range []string{"Write", "Edit"} {
			if !contains(tools, x) {
				t.Errorf("%s missing: %v", x, tools)
			}
		}
	})

	// A mem_save can return judgment_required; the protocol then requires
	// mem_judge. An agent granted memory without it can save a conflict it
	// can never resolve, which is what a live orchestrator session hit.
	t.Run("an agent with the memory capability can resolve an Engram conflict", func(t *testing.T) {
		t.Parallel()
		tools := lineTools(t, frontmatter(claudeAgent(t, renderedHome(t), "worker")))
		for _, x := range []string{"mcp__engram__mem_judge", "mcp__plugin_engram_engram__mem_judge"} {
			if !contains(tools, x) {
				t.Errorf("%s missing: %v", x, tools)
			}
		}
	})

	// Docket AS1. A live orchestrator delegated a pending-items inventory to
	// explore and told it to search Engram; explore had no memory tool, said
	// so, and the plan reached the user built from stale files alone. Since
	// the lean roster, explore also runs the LDD explore phase, which saves its
	// rationale as ldd/<change>/explore, so it holds the whole memory capability.
	t.Run("explore can search, read, and save Engram", func(t *testing.T) {
		t.Parallel()
		tools := lineTools(t, frontmatter(claudeAgent(t, renderedHome(t), "explore")))
		for _, p := range []string{"mcp__engram__", "mcp__plugin_engram_engram__"} {
			for _, x := range []string{"mem_search", "mem_get_observation", "mem_save", "mem_judge"} {
				if !contains(tools, p+x) {
					t.Errorf("%s missing: %v", p+x, tools)
				}
			}
		}
	})

	// Session and lifecycle memory belong to the top-level agent
	// (contracts/vocabulary.yaml, "Top-level tools"). A live run found the
	// orchestrator unable to call mem_session_summary, which ldd.md
	// requires of it after archive, while no executor may call it at all.
	topLevel := []string{"mem_session_summary", "mem_context", "mem_current_project", "mem_review"}
	engramPrefixes := []string{"mcp__engram__", "mcp__plugin_engram_engram__"}

	t.Run("the orchestrator holds every top-level tool", func(t *testing.T) {
		t.Parallel()
		tools := claudeTools(frontmatter(claudeAgent(t, renderedHome(t), "jhon-orchestrator")))
		for _, x := range topLevel {
			for _, p := range engramPrefixes {
				if !contains(tools, p+x) {
					t.Errorf("%s missing: %v", p+x, tools)
				}
			}
		}
	})

	t.Run("no executor holds a top-level tool", func(t *testing.T) {
		t.Parallel()
		home := renderedHome(t)
		_, delegable := manifestAgents(t)
		if !contains(delegable, "verify") {
			t.Fatal(delegable)
		}
		for _, name := range delegable {
			leaked := []string{}
			for _, x := range claudeTools(frontmatter(claudeAgent(t, home, name))) {
				for _, tl := range topLevel {
					if strings.HasSuffix(x, "__"+tl) {
						leaked = append(leaked, x)
						break
					}
				}
			}
			equal(t, leaked, []string{}, name)
		}
	})

	t.Run("an executor granted memory-session fails the top-level-tools rule", func(t *testing.T) {
		t.Parallel()
		r := validate(t, repoWithManifest(t,
			"caps: [read, write, bash, memory, codegraph, todo, context7]\n    description: \"Implement a scoped task",
			"caps: [read, write, bash, memory, memory-session, codegraph, todo, context7]\n    description: \"Implement a scoped task"))
		wantCode(t, r, 1)
		match(t, r.Out(), `top-level-tools\s+agents\.yaml\s+worker: .*mcp__engram__mem_session_summary`)
	})

	t.Run("a top-level tool smuggled into a shared capability fails the top-level-tools rule", func(t *testing.T) {
		t.Parallel()
		r := validate(t, repoWithManifest(t, "mcp__plugin_engram_engram__mem_judge]",
			"mcp__plugin_engram_engram__mem_judge, mcp__plugin_engram_engram__mem_context]"))
		wantCode(t, r, 1)
		match(t, r.Out(), `top-level-tools\s+agents\.yaml\s+worker: .*mcp__plugin_engram_engram__mem_context`)
	})

	t.Run("the orchestrator missing a top-level tool fails the top-level-tools rule", func(t *testing.T) {
		t.Parallel()
		r := validate(t, repoWithManifest(t, "mcp__engram__mem_session_summary, mcp__plugin_engram_engram__mem_session_summary, ", ""))
		wantCode(t, r, 1)
		match(t, r.Out(), `top-level-tools\s+agents\.yaml\s+jhon-orchestrator: .*mcp__engram__mem_session_summary`)
	})
}

// hookLine is the live claude-code blocking-hook-agent line of agents.yaml,
// the anchor every edit below replaces.
func hookLine(t testing.TB) string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^    blocking-hook-agent: \{ capability: true, verification: verified, .*$`).FindString(manifestSrc(t))
	if m == "" {
		t.Fatal("no verified blocking-hook-agent line in agents.yaml")
	}
	return m
}

const hookDocumented = `    blocking-hook-agent: { capability: true, verification: documented, version: "2.1.280", source: "Claude Code hooks reference" }`

func spike() obj {
	return obj{
		{"runtime", "claude-code"}, {"runtime_version", "2.1.280"}, {"guard", "hooks/git-state-guard.mjs"}, {"guard_version", "0203cdf"},
		{"call", obj{{"tool", "Bash"}, {"command", "git commit -m x"}, {"agent", "general"}}}, {"payload", obj{{"tool_name", "Bash"}, {"agent_type", "general"}}},
		{"decision", "refuse (exit 2)"}, {"runtime_result", "the call was not run; the agent received the refusal"}, {"no_effect_check", "git log shows no new commit"},
		{"control_case", obj{{"call", "git status"}, {"result", "ran"}}},
	}
}

// patched is o with each patch key's value replaced in place, or appended.
func patched(o obj, patch obj) obj {
	out := append(obj(nil), o...)
	for _, p := range patch {
		i := slices.IndexFunc(out, func(e kv) bool { return e.k == p.k })
		if i >= 0 {
			out[i].v = p.v
		} else {
			out = append(out, p)
		}
	}
	return out
}

func hookVerified(t testing.TB, source string) [2]string {
	return [2]string{hookLine(t), `    blocking-hook-agent: { capability: true, verification: verified, version: "2.1.280", source: "` + source + `" }`}
}

var supersededSource = regexp.MustCompile(`(blocking-hook-agent: \{[^}]*?source: "[^"]*",[^}]*?), source: "Claude Code hooks reference[^"]*"`)

// repoWithSpike is a repository copy with a spike record at
// test/testdata/spikes/<name>/record.json and agents.yaml edited.
func repoWithSpike(t testing.TB, name string, record obj, edits ...[2]string) string {
	t.Helper()
	repo := support.CopyRepo(t)
	b, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(repo, "test", "testdata", "spikes", name, "record.json"), string(b)+"\n")
	file := filepath.Join(repo, "prose", "agents.yaml")
	src := support.Read(t, file)
	for _, e := range edits {
		if !strings.Contains(src, e[0]) {
			t.Fatalf("anchor not found: %s", e[0])
		}
		src = strings.Replace(src, e[0], e[1], 1)
	}
	// A value keeps its last `source`: drop the documented one an edit superseded.
	if m := supersededSource.FindStringSubmatchIndex(src); m != nil {
		src = src[:m[0]] + src[m[2]:m[3]] + src[m[1]:]
	}
	write(t, file, src)
	return repo
}

// renderManifestEdits renders a copy of the repository whose agents.yaml has
// each edit applied, and returns the HOME.
func renderManifestEdits(t testing.TB, edits ...[2]string) string {
	t.Helper()
	repo := support.CopyRepo(t)
	file := filepath.Join(repo, "prose", "agents.yaml")
	src := support.Read(t, file)
	for _, e := range edits {
		if !strings.Contains(src, e[0]) {
			t.Fatalf("anchor not found: %s", e[0])
		}
		src = strings.Replace(src, e[0], e[1], 1)
	}
	write(t, file, src)
	home := t.TempDir()
	wantCode(t, install(t, home, repo, "--only", "claude-code"), 0)
	return home
}

func TestRenderRuntimeModel(t *testing.T) {
	t.Parallel()

	t.Run("a behavioural anchor with no guarantees entry fails runtime-guarantee", func(t *testing.T) {
		t.Parallel()
		r := validate(t, repoWithManifest(t, "  runtime-boundaries/no-technique: { claude-code: unsupported, pi: unsupported }\n", ""))
		wantCode(t, r, 1)
		match(t, r.Out(), `runtime-guarantee\s+agents\.yaml\s+runtime-boundaries/no-technique has no guarantees entry`)
	})

	t.Run("an enforced level whose primitive is not verified fails runtime-guarantee", func(t *testing.T) {
		t.Parallel()
		r := validate(t, repoWithManifest(t, hookLine(t), hookDocumented))
		wantCode(t, r, 1)
		match(t, r.Out(), `runtime-guarantee\s+agents\.yaml\s+safety/destructive-deny: claude-code is enforced but blocking-hook-agent is documented there, not verified`)
	})

	t.Run("a verified primitive whose source cites no spike record or real session fails runtime-matrix", func(t *testing.T) {
		t.Parallel()
		r := validate(t, repoWithManifest(t, `source: "test/testdata/audit/apply-bypass-archive: a real session calling`, `source: "a real session calling`))
		wantCode(t, r, 1)
		match(t, r.Out(), `runtime-matrix\s+agents\.yaml\s+claude-code\.mcp: verified needs a spike record \(test/testdata/spikes/<name>/record\.json\) or a real-session fixture`)
	})

	t.Run("a rendered runtime whose always-on bundle exceeds its instruction budget fails runtime-matrix", func(t *testing.T) {
		t.Parallel()
		r := validate(t, repoWithManifest(t, "  claude-code: { max_instruction_bytes: none, imports: true,", "  claude-code: { max_instruction_bytes: 1000, imports: false,"))
		wantCode(t, r, 1)
		match(t, r.Out(), `runtime-matrix\s+agents\.yaml\s+claude-code: the always-on bundle is \d+ bytes and claude-code takes at most 1000 bytes, inline \(no imports\)`)
	})

	t.Run("every Claude Code agent definition ends with a brief claude-code guarantees declaration", func(t *testing.T) {
		t.Parallel()
		home := renderedHome(t)
		for _, name := range []string{"jhon-orchestrator", "worker", "review-risk"} {
			md := claudeAgent(t, home, name)
			tail := md[strings.LastIndex(md, "\n## Runtime guarantees\n"):]
			match(t, tail, "(?m)^\n## Runtime guarantees\n\nRuntime guarantees in claude-code \\(agents\\.yaml `guarantees`\\): \\d+ enforced, \\d+ behavioural \\(measured by jhon audit\\), \\d+ unsupported\\.$", name)
			match(t, tail, "`jhon doctor --runtime claude-code` lists every rule")
			noMatch(t, tail, "`runtime-boundaries/stop`")
			if len(tail) >= 600 {
				t.Errorf("%s: %d bytes", name, len(tail))
			}
			if !strings.HasSuffix(tail, ".\n") {
				t.Errorf("%s: %q", name, tail[max(0, len(tail)-80):])
			}
		}
	})

	t.Run("a runtime tool needs its primitive: without plan mode the orchestrator has no ExitPlanMode", func(t *testing.T) {
		t.Parallel()
		home := renderManifestEdits(t, [2]string{`plan-mode: { capability: true, verification: documented, version: "2.1.280"`, `plan-mode: { capability: false, verification: documented, version: "2.1.280"`})
		fm := frontmatter(claudeAgent(t, home, "jhon-orchestrator"))
		noMatch(t, fm, `ExitPlanMode`)
		match(t, fm, `(?m)^  - AskUserQuestion$`)
	})

	t.Run("without blocking hooks in claude-code no agent renders a hooks block", func(t *testing.T) {
		t.Parallel()
		edits := [][2]string{{hookLine(t), strings.Replace(hookDocumented, "capability: true", "capability: false", 1)}}
		for _, rule := range []string{"framework-boundary/no-framework-write", "safety/destructive-deny", "safety/outward-ask"} {
			edits = append(edits, [2]string{rule + ": { by: blocking-hook-agent, claude-code: enforced, pi: enforced }", rule + ": { claude-code: unsupported, pi: unsupported }"})
		}
		for _, rule := range []string{"efficiency/writer-scope", "ldd/explore-scope"} {
			edits = append(edits, [2]string{rule + ": { by: blocking-hook-agent, claude-code: enforced, pi: enforced }", rule + ": { claude-code: unsupported, pi: unsupported }"})
		}
		home := renderManifestEdits(t, edits...)
		for _, name := range []string{"jhon-orchestrator", "worker", "review-risk", "explore"} {
			noMatch(t, frontmatter(claudeAgent(t, home, name)), `(?m)^hooks:`, name)
		}
		match(t, claudeAgent(t, home, "jhon-orchestrator"), `(?m)^Absent here: blocking-hook-agent\.$`)
	})

	// Docket AG addendum (2026-10-03): `verified` is judged by what was
	// executed and observed. A unit test shows Jhon's side; a primitive that
	// needs a refusal is verified only by a spike record holding every
	// spike_record_fields field.
	t.Run("a complete spike record verifies a primitive that needs a refusal", func(t *testing.T) {
		t.Parallel()
		repo := repoWithSpike(t, "hooks", spike(), hookVerified(t, "test/testdata/spikes/hooks/record.json"))
		wantCode(t, validate(t, repo), 0)
	})

	t.Run("a spike record missing a field fails runtime-matrix and verifies nothing", func(t *testing.T) {
		t.Parallel()
		partial := slices.DeleteFunc(spike(), func(e kv) bool { return e.k == "no_effect_check" })
		r := validate(t, repoWithSpike(t, "hooks", partial, hookVerified(t, "test/testdata/spikes/hooks/record.json")))
		wantCode(t, r, 1)
		match(t, r.Out(), `runtime-matrix\s+agents\.yaml\s+claude-code\.blocking-hook-agent: test/testdata/spikes/hooks/record\.json: lacks no_effect_check`)
	})

	// Docket AR: a filled form is not a proof. The record must correspond to
	// the claim that cites it: its runtime and runtime version are the citing
	// entry's, and its guard is the primitive's mechanism.
	placeholder := obj{}
	for _, e := range spike() {
		placeholder = append(placeholder, kv{e.k, "x"})
	}
	for _, c := range []struct {
		what    string
		patch   obj
		message string
	}{
		{"placeholder values", placeholder, `runtime x is not claude-code`},
		{"another runtime version", obj{{"runtime_version", "2.0.0"}}, `runtime_version 2\.0\.0 is not 2\.1\.280`},
		{"another primitive's mechanism", obj{{"guard", "tools: list"}}, `guard tools: list is not a mechanism of blocking-hook-agent`},
	} {
		t.Run("a spike record of "+c.what+" does not verify the primitive citing it", func(t *testing.T) {
			t.Parallel()
			r := validate(t, repoWithSpike(t, "hooks", patched(spike(), c.patch), hookVerified(t, "test/testdata/spikes/hooks/record.json")))
			wantCode(t, r, 1)
			match(t, r.Out(), `runtime-matrix\s+agents\.yaml\s+claude-code\.blocking-hook-agent: test/testdata/spikes/hooks/record\.json: .*`+c.message)
		})
	}

	t.Run("a guard's unit test or a real session does not verify blocking-hook-agent", func(t *testing.T) {
		t.Parallel()
		for _, source := range []string{"test/safety-guard.test.ts", "test/testdata/audit/apply-bypass-archive"} {
			r := validate(t, repoWithSpike(t, "unused", spike(), hookVerified(t, source)))
			wantCode(t, r, 1)
			match(t, r.Out(), `runtime-matrix\s+agents\.yaml\s+claude-code\.blocking-hook-agent: (verified needs a spike record|blocking-hook-agent needs a spike record)`, source)
		}
	})

	t.Run("spike_record_fields in contracts/vocabulary.yaml names every field a spike record holds", func(t *testing.T) {
		t.Parallel()
		match(t, support.Read(t, filepath.Join(repoRoot, "prose", "contracts", "vocabulary.yaml")),
			`(?m)^spike_record_fields: \[runtime, runtime_version, guard, guard_version, call, payload, decision, runtime_result, no_effect_check, control_case\]$`)
	})

	t.Run("the guarantee levels come from contracts/vocabulary.yaml", func(t *testing.T) {
		t.Parallel()
		repo := support.CopyRepo(t)
		editFile(t, filepath.Join(repo, "prose", "contracts", "vocabulary.yaml"),
			"guarantee_levels: [enforced, behavioural, unsupported]", "guarantee_levels: [enforced, behavioural, partial]")
		r := validate(t, repo)
		wantCode(t, r, 1)
		match(t, r.Out(), `runtime-guarantee\s+contracts/vocabulary\.yaml\s+guarantee_levels must be \[enforced, behavioural, unsupported\]`)
	})
}

func TestRenderRoster(t *testing.T) {
	t.Parallel()

	// An agent that writes an LDD artifact holds Edit and Write like any
	// writer: the write guard is its only limit, with no path confinement to
	// its own artifact. Planning runs inline in the orchestrator since the
	// lean roster.
	t.Run("artifact writers hold generic write, their writes guarded by the write, framework, secrets, and route guards alone", func(t *testing.T) {
		t.Parallel()
		home := renderedHome(t)
		for _, name := range []string{"explore", "verify"} {
			if !contains(manifestCaps(t, name), "write") {
				t.Errorf("%s: %v", name, manifestCaps(t, name))
			}
			fm := frontmatter(claudeAgent(t, home, name))
			tools := claudeTools(fm)
			for _, x := range []string{"Edit", "Write"} {
				if !contains(tools, x) {
					t.Errorf("%s: %s missing: %v", name, x, tools)
				}
			}
			names := []string{}
			for _, h := range preToolUse(t, fm) {
				if regexp.MustCompile(`\b(Edit|Write)\b`).MatchString(h.matcher) {
					names = append(names, guardName(h.command))
				}
			}
			equal(t, names, []string{"vcs-guard", "framework-guard", "secrets-guard", "route-guard"}, name)
		}
	})

	// The lean roster: three executors carry every delegated LDD phase, beside
	// the reviewers, the judges, the fix actor, and the primary agent.
	t.Run("the manifest holds the lean roster and none of the retired agents", func(t *testing.T) {
		t.Parallel()
		equal(t, sorted(allManifestAgents(t)), []string{
			"explore", "jd-fix-agent", "jd-judge-a", "jd-judge-b", "jhon-orchestrator",
			"review-readability", "review-refuter", "review-reliability", "review-resilience", "review-risk",
			"review-validator", "verify", "worker",
		})
		if _, err := os.Stat(filepath.Join(repoRoot, "prose", "coordinator.md")); err == nil {
			t.Error("the retired coordinator's body remains: prose/coordinator.md")
		}
		retired := regexp.MustCompile(`^(general|vcs-worker)\.md$`)
		for _, f := range readDirNames(t, filepath.Join(repoRoot, "prose", "agents")) {
			if strings.HasSuffix(f, ".md") && retired.MatchString(f) {
				t.Errorf("a retired agent's body remains: %s", f)
			}
		}
	})

	// explore also collects external evidence (the research phase).
	t.Run("explore holds the web tools beside the repository ones", func(t *testing.T) {
		t.Parallel()
		tools := claudeTools(frontmatter(claudeAgent(t, renderedHome(t), "explore")))
		for _, x := range []string{"Read", "WebFetch", "WebSearch"} {
			if !contains(tools, x) {
				t.Errorf("%s missing: %v", x, tools)
			}
		}
	})

	// A session `jhon start` opens runs as a primary agent, whose tool list is
	// all it holds: the owner's Microsoft 365 connector and web search are on
	// it, by server name; no sub-agent holds the connector.
	t.Run("the primary agent holds the Microsoft 365 connector and WebSearch, and no sub-agent holds the connector", func(t *testing.T) {
		t.Parallel()
		home := renderedHome(t)
		for _, name := range []string{"jhon-orchestrator"} {
			tools := claudeTools(frontmatter(claudeAgent(t, home, name)))
			for _, x := range []string{"mcp__claude_ai_Microsoft_365", "WebSearch"} {
				if !contains(tools, x) {
					t.Errorf("%s: %s missing: %v", name, x, tools)
				}
			}
			if contains(tools, "WebFetch") {
				t.Errorf("%s: search only", name)
			}
		}
		for _, name := range renderedAgents(t, home) {
			if strings.HasPrefix(name, "jhon-") {
				continue
			}
			if slices.ContainsFunc(claudeTools(frontmatter(claudeAgent(t, home, name))), func(s string) bool { return strings.HasPrefix(s, "mcp__claude_ai_") }) {
				t.Error(name)
			}
		}
	})

	// The judges cite changed-hunk proof, which needs `git diff`/`git show` on
	// the frozen candidate; they get the guarded inspectors and nothing wider.
	t.Run("the judges may inspect through the guarded Bash and still write nothing", func(t *testing.T) {
		t.Parallel()
		home := renderedHome(t)
		for _, name := range []string{"jd-judge-a", "jd-judge-b"} {
			for _, c := range []string{"read", "inspect", "memory", "codegraph"} {
				if !contains(manifestCaps(t, name), c) {
					t.Errorf("%s lacks %s", name, c)
				}
			}
			fm := frontmatter(claudeAgent(t, home, name))
			tools := lineTools(t, fm)
			if !contains(tools, "Bash") || contains(tools, "Edit") || contains(tools, "Write") {
				t.Errorf("%s: %v", name, tools)
			}
			// The inspect guard, the safety guard every agent with Bash
			// carries, and the framework guard every agent carries (docket AV2).
			matchers := []string{}
			for _, h := range preToolUse(t, fm) {
				matchers = append(matchers, h.matcher)
			}
			equal(t, matchers, []string{"Bash", "Bash", "Edit|MultiEdit|Write|Bash", "Read|Grep|Glob|Edit|MultiEdit|Write|NotebookEdit", "Edit|MultiEdit|Write|NotebookEdit|Bash|Agent"}, name)
			// Gaining inspect must not grant what the judge lacked before.
			if contains(tools, "WebFetch") {
				t.Errorf("%s: %v", name, tools)
			}
		}
	})

	t.Run("no onboarding agent is rendered", func(t *testing.T) {
		t.Parallel()
		// Onboarding is a conversational guide the orchestrator runs; every
		// real phase goes to its own phase agent, so there is no onboarding
		// executor to render.
		if exists(filepath.Join(renderedHome(t), ".claude", "agents", "onboard.md")) {
			t.Error("onboard.md rendered")
		}
	})
}

// A manifest agent removed from agents.yaml must not survive in
// ~/.claude/agents, and nothing this renderer never wrote may be deleted.

const ghost = "zz-ghost-agent"

var ghostEntry = "\n  - name: " + ghost + "\n    tier: mid\n    caps: [read]\n    description: \"ghost\"\n"

// repoWithGhost is a repo copy declaring one extra agent, with a body.
func repoWithGhost(t testing.TB) string {
	t.Helper()
	repo := support.CopyRepo(t)
	yaml := filepath.Join(repo, "prose", "agents.yaml")
	write(t, yaml, strings.TrimRight(support.Read(t, yaml), " \t\r\n")+ghostEntry)
	write(t, filepath.Join(repo, "prose", "agents", ghost+".md"), "You are not the orchestrator: never delegate.\n")
	return repo
}

// dropGhost removes the extra agent again, as a manifest edit would.
func dropGhost(t testing.TB, repo string) {
	t.Helper()
	yaml := filepath.Join(repo, "prose", "agents.yaml")
	write(t, yaml, strings.Replace(support.Read(t, yaml), ghostEntry, "\n", 1))
	if err := os.Remove(filepath.Join(repo, "prose", "agents", ghost+".md")); err != nil {
		t.Fatal(err)
	}
}

func TestRenderOwnership(t *testing.T) {
	t.Parallel()

	t.Run("an agent removed from the manifest is pruned; unrecorded agent files stay", func(t *testing.T) {
		t.Parallel()
		repo := repoWithGhost(t)
		home := t.TempDir()
		dir := filepath.Join(home, ".claude", "agents")
		r := install(t, home, repo)
		wantCode(t, r, 0)
		if !exists(filepath.Join(dir, ghost+".md")) {
			t.Fatal(r.Out())
		}
		write(t, filepath.Join(dir, "user-agent.md"), "not ours\n")

		dropGhost(t, repo)
		r = install(t, home, repo)
		wantCode(t, r, 0)
		if exists(filepath.Join(dir, ghost+".md")) {
			t.Error(r.Out())
		}
		if !exists(filepath.Join(dir, "user-agent.md")) || !exists(filepath.Join(dir, "jhon-orchestrator.md")) {
			t.Error("a file that should stay is gone")
		}
		if contains(strings.Split(support.Read(t, filepath.Join(dir, ".jhon-owned")), "\n"), ghost) {
			t.Error("the record still names the ghost")
		}
	})

	t.Run("--check reports a removed agent as drift without deleting it", func(t *testing.T) {
		t.Parallel()
		repo := repoWithGhost(t)
		home := t.TempDir()
		wantCode(t, install(t, home, repo), 0)
		dropGhost(t, repo)
		r := renderIn(t, home, repo, "--check")
		wantCode(t, r, 1)
		match(t, r.Out(), `DRIFT.*`+ghost)
		if !exists(filepath.Join(home, ".claude", "agents", ghost+".md")) {
			t.Error("the removed agent was deleted")
		}
	})

	t.Run("the first render with no ownership record deletes nothing and writes the record", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		dir := filepath.Join(home, ".claude", "agents")
		// A leftover from before the record existed: unrecorded, so never ours to remove.
		write(t, filepath.Join(dir, "old-onboard.md"), "stale but unrecorded\n")
		r := install(t, home, repoRoot)
		wantCode(t, r, 0)
		if !exists(filepath.Join(dir, "old-onboard.md")) {
			t.Error(r.Out())
		}
		recorded := strings.Split(strings.TrimSpace(support.Read(t, filepath.Join(dir, ".jhon-owned"))), "\n")
		if !contains(recorded, "jhon-orchestrator") || contains(recorded, "old-onboard") {
			t.Error(recorded)
		}
	})
}

// The manifest reader is minimal on purpose, so a line it does not understand
// must fail validation instead of being skipped: a misspelled key silently
// dropped is a capability, a body, or a tier the author believes is set.

// validateManifest validates a repo copy whose agents.yaml was passed through edit.
func validateManifest(t testing.TB, edit func(string) string) support.Result {
	t.Helper()
	return validateEdited(t, "agents.yaml", edit)
}

// validateEdited validates a repo copy after editing one file in it.
func validateEdited(t testing.TB, rel string, edit func(string) string) support.Result {
	t.Helper()
	repo := support.CopyRepo(t)
	file := treePath(repo, rel)
	before := support.Read(t, file)
	after := edit(before)
	if after == before {
		t.Fatalf("the edit must change %s", rel)
	}
	write(t, file, after)
	return validate(t, repo)
}

func rep(find, repl string) func(string) string {
	return func(s string) string { return strings.Replace(s, find, repl, 1) }
}

func TestRenderManifest(t *testing.T) {
	t.Parallel()

	t.Run("an unknown top-level manifest key fails the manifest rule", func(t *testing.T) {
		t.Parallel()
		r := validateManifest(t, rep("version: 1\n", "version: 1\nmodels: {}\n"))
		wantCode(t, r, 1)
		match(t, r.Out(), `manifest\s+agents\.yaml:\d+\s+unknown top-level key 'models'`)
	})

	t.Run("an unknown per-agent manifest key fails the manifest rule", func(t *testing.T) {
		t.Parallel()
		r := validateManifest(t, rep("  - name: worker\n    tier: strong\n", "  - name: worker\n    tier: strong\n    teir: strong\n"))
		wantCode(t, r, 1)
		match(t, r.Out(), `manifest\s+agents\.yaml:\d+\s+worker: unknown key 'teir'`)
	})

	t.Run("an agent whose tier is not declared fails the manifest rule", func(t *testing.T) {
		t.Parallel()
		r := validateManifest(t, rep("  - name: worker\n    tier: strong\n", "  - name: worker\n    tier: turbo\n"))
		wantCode(t, r, 1)
		match(t, r.Out(), `manifest\s+agents\.yaml\s+worker: tier 'turbo' undefined for claude-code`)
	})

	// Claude Code receives the concrete model of the agent's tier, pinned by
	// its exact model ID, and the tier's effort (docket AX2): a tier value is
	// `model` or `model@effort`, and `effort:` is rendered only when it is set.
	t.Run("Claude Code frontmatter carries the pinned model and the effort of each agent's tier", func(t *testing.T) {
		t.Parallel()
		home := renderedHome(t)
		pair := func(name string) string {
			md := claudeAgent(t, home, name)
			m, _ := fmField(md, "model")
			e, _ := fmField(md, "effort")
			return m + "@" + e
		}
		// Docket AX1: no role defaults to frontier; frontier is a per-review opt-in.
		// Every role defaults to Opus at medium effort (the owner's choice, 2026-10-28).
		strong := []string{"explore", "worker", "verify", "jd-judge-a", "jd-judge-b", "jd-fix-agent",
			"review-risk", "review-resilience", "review-readability", "review-reliability", "review-refuter", "review-validator", "jhon-orchestrator"}
		mid := []string{}
		cheap := []string{}
		for _, name := range strong {
			equal(t, pair(name), "claude-opus-5-5@medium", name)
		}
		for _, name := range mid {
			equal(t, pair(name), "claude-sonnet-5-5@medium", name)
		}
		for _, name := range cheap {
			equal(t, pair(name), "claude-haiku-5-5@medium", name)
		}
		equal(t, len(strong)+len(mid)+len(cheap), len(renderedAgents(t, home)))
	})

	t.Run("a tier without an effort renders no effort key", func(t *testing.T) {
		t.Parallel()
		repo := support.CopyRepo(t)
		yaml := filepath.Join(repo, "prose", "agents.yaml")
		src := support.Read(t, yaml)
		edited := strings.Replace(strings.Replace(src, "cheap: claude-haiku-5-5@medium", "cheap: claude-haiku-5-5", 1),
			"  - name: worker\n    tier: strong\n", "  - name: worker\n    tier: cheap\n", 1)
		if edited == src {
			t.Fatal("the anchor must match")
		}
		write(t, yaml, edited)
		home := t.TempDir()
		wantCode(t, install(t, home, repo), 0)
		md := claudeAgent(t, home, "worker")
		m, _ := fmField(md, "model")
		equal(t, m, "claude-haiku-5-5")
		if e, ok := fmField(md, "effort"); ok {
			t.Errorf("effort %q rendered:\n%s", e, frontmatter(md))
		}
	})

	t.Run("a Claude Code tier effort the runtime does not document fails the manifest rule", func(t *testing.T) {
		t.Parallel()
		r := validateManifest(t, rep("strong: claude-opus-5-5@medium", "strong: claude-opus-5-5@turbo"))
		wantCode(t, r, 1)
		match(t, r.Out(), `manifest\s+agents\.yaml\S*\s+tiers\.claude-code\.strong: effort 'turbo' is not one of low, medium, high, xhigh, max`)
	})

	// Every agent reads the project's CLAUDE.md: a worker that did not left
	// an enum outside the layer the project's conventions name.
	// T131 (L905, L908): no agent has a turn cap, the worker included (S35's
	// 80 stopped only sound runs); the context cap and the timeout bound a run.
	t.Run("no agent renders maxTurns, and none omits CLAUDE.md", func(t *testing.T) {
		t.Parallel()
		home := renderedHome(t)
		for _, name := range renderedAgents(t, home) {
			md := claudeAgent(t, home, name)
			if mt, ok := fmField(md, "maxTurns"); ok {
				t.Errorf("%s carries maxTurns %q", name, mt)
			}
			if oc, ok := fmField(md, "omitClaudeMd"); ok {
				t.Errorf("%s carries omitClaudeMd %q", name, oc)
			}
		}
	})

	t.Run("a maxTurns that is not a positive integer fails the manifest rule", func(t *testing.T) {
		t.Parallel()
		r := validateManifest(t, rep("  - name: worker\n    tier: strong\n", "  - name: worker\n    tier: strong\n    maxTurns: lots\n"))
		wantCode(t, r, 1)
		match(t, r.Out(), `worker: maxTurns 'lots' is not a positive integer`)
	})

	t.Run("an omitClaudeMd other than true fails the manifest rule", func(t *testing.T) {
		t.Parallel()
		r := validateManifest(t, rep("  - name: explore\n    tier: strong\n", "  - name: explore\n    tier: strong\n    omitClaudeMd: yes\n"))
		wantCode(t, r, 1)
		match(t, r.Out(), `explore: omitClaudeMd 'yes' must be true`)
	})

	t.Run("an agent effort the runtime does not document fails the manifest rule", func(t *testing.T) {
		t.Parallel()
		r := validateManifest(t, rep("  - name: worker\n    tier: strong\n", "  - name: worker\n    tier: strong\n    effort: turbo\n"))
		wantCode(t, r, 1)
		match(t, r.Out(), `manifest\s+agents\.yaml\S*\s+worker: effort 'turbo' is not one of low, medium, high, xhigh, max`)
	})

	t.Run("an agent effort on a tier whose Claude model takes none fails the manifest rule", func(t *testing.T) {
		t.Parallel()
		r := validateManifest(t, func(s string) string {
			return strings.Replace(strings.Replace(s, "cheap: claude-haiku-5-5@medium", "cheap: claude-haiku-5-5", 1),
				"  - name: worker\n    tier: strong\n", "  - name: worker\n    tier: cheap\n    effort: high\n", 1)
		})
		wantCode(t, r, 1)
		match(t, r.Out(), `manifest\s+agents\.yaml\S*\s+worker: tier cheap \(claude-haiku-5-5\) takes no effort`)
	})

	t.Run("a tier value with an empty model or effort fails the manifest rule", func(t *testing.T) {
		t.Parallel()
		r := validateManifest(t, rep("strong: claude-opus-5-5@medium", "strong: claude-opus-5-5@"))
		wantCode(t, r, 1)
		match(t, r.Out(), `manifest\s+agents\.yaml\S*\s+tiers\.claude-code\.strong: write the value as model or model@effort`)
		c := validateManifest(t, rep("cheap: claude-bridge/claude-haiku-5-5@medium", "cheap: @low"))
		wantCode(t, c, 1)
		match(t, c.Out(), `tiers\.pi\.cheap: write the value as model or model@effort`)
	})

	t.Run("an unparseable manifest line fails the manifest rule", func(t *testing.T) {
		t.Parallel()
		r := validateManifest(t, rep("  - name: worker\n", "  - name: worker\n    just some words\n"))
		wantCode(t, r, 1)
		match(t, r.Out(), `manifest\s+agents\.yaml:\d+\s+cannot parse`)
	})

	// A templated body is generated, so the manifest and the template must
	// agree on every value, and every rule that reads agent bodies must read
	// the expansion: a defect in a template is a defect in each agent it generates.
	t.Run("a template placeholder the manifest does not supply fails validation", func(t *testing.T) {
		t.Parallel()
		r := validateEdited(t, "agents/templates/review-lens.md", rep("## Scope", "## Scope for {{audience}}"))
		wantCode(t, r, 1)
		match(t, r.Out(), `agent-template\s+.*review-risk: template placeholder \{\{audience\}\} has no value`)
	})

	t.Run("a manifest var the template never uses fails validation", func(t *testing.T) {
		t.Parallel()
		r := validateEdited(t, "agents.yaml", rep("      judge: A\n", "      judge: A\n      colour: red\n"))
		wantCode(t, r, 1)
		match(t, r.Out(), `agent-template\s+.*jd-judge-a: var 'colour' is not used by template jd-judge`)
	})

	t.Run("a defect in a template is reported against every agent it generates", func(t *testing.T) {
		t.Parallel()
		r := validateEdited(t, "agents/templates/review-lens.md", rep("## Scope", "See `contracts/ghost.md`.\n\n## Scope"))
		wantCode(t, r, 1)
		for _, name := range []string{"review-risk", "review-resilience", "review-readability", "review-reliability"} {
			match(t, r.Out(), `broken-ref\s+agents/`+name+`\.md`)
		}
	})
}

// The always-on layer rides inline in each Jhon agent's definition, so the
// global CLAUDE.md carries no Jhon text. The install removes the block an
// earlier release rendered, its `<!-- jhon:start -->` and `<!-- jhon:end -->`
// markers included, and keeps every other byte. A file whose markers are
// unpaired, duplicated, or out of order is refused whole, because guessing
// where the block ends would rewrite someone else's text.

const (
	startMarker = "<!-- jhon:start -->"
	endMarker   = "<!-- jhon:end -->"
)

func surrounded(t testing.TB) string {
	t.Helper()
	return support.Read(t, filepath.Join(repoRoot, "test", "testdata", "claude-md", "surrounded.md"))
}

// homeWithClaudeMd is a temporary HOME whose ~/.claude/CLAUDE.md holds content.
func homeWithClaudeMd(t testing.TB, content string) (home, file string) {
	t.Helper()
	home = t.TempDir()
	file = filepath.Join(home, ".claude", "CLAUDE.md")
	write(t, file, content)
	return
}

func TestRenderClaudeMd(t *testing.T) {
	t.Parallel()

	t.Run("the install removes the Jhon block and its markers from CLAUDE.md and keeps every other byte", func(t *testing.T) {
		t.Parallel()
		src := surrounded(t)
		if !strings.HasPrefix(src, "@RTK.md\n") {
			t.Fatal("the fixture holds @RTK.md before the block")
		}
		home, file := homeWithClaudeMd(t, src)
		wantCode(t, install(t, home, repoRoot, "--only", "claude-code"), 0)
		equal(t, support.Read(t, file), "@RTK.md\n\n"+src[strings.Index(src, "<!-- ryoku-rashin:begin -->"):])
		again := install(t, home, repoRoot, "--only", "claude-code")
		wantCode(t, again, 0)
		match(t, again.Out(), `ok\s+~/\.claude/CLAUDE\.md`)
	})

	t.Run("a CLAUDE.md that is only the Jhon block becomes empty", func(t *testing.T) {
		t.Parallel()
		home, file := homeWithClaudeMd(t, startMarker+"\n# Jhon\n\n@~/.local/share/jhon/current/persona/jhon.md\n\n"+endMarker+"\n")
		wantCode(t, install(t, home, repoRoot, "--only", "claude-code"), 0)
		equal(t, support.Read(t, file), "")
	})

	t.Run("a CLAUDE.md without markers is the user's: left alone, and --check reports it ok", func(t *testing.T) {
		t.Parallel()
		src := "@RTK.md\n\nmy own instructions\n"
		home, file := homeWithClaudeMd(t, src)
		wantCode(t, install(t, home, repoRoot, "--only", "claude-code"), 0)
		equal(t, support.Read(t, file), src)
		match(t, render(t, home, "--check", "--only", "claude-code").Out(), `ok\s+~/\.claude/CLAUDE\.md`)
	})

	t.Run("--check reports a leftover Jhon block as DRIFT and writes nothing", func(t *testing.T) {
		t.Parallel()
		src := surrounded(t)
		home, file := homeWithClaudeMd(t, src)
		r := render(t, home, "--check", "--only", "claude-code")
		wantCode(t, r, 1)
		match(t, r.Out(), `DRIFT\s+~/\.claude/CLAUDE\.md`)
		equal(t, support.Read(t, file), src)
	})

	// Docket AV1: --validate is the pre-commit gate and judges the repository
	// alone; a leftover block is installed state, which --check reports.
	t.Run("--validate judges the repository alone; a leftover block is --check's DRIFT", func(t *testing.T) {
		t.Parallel()
		src := surrounded(t)
		h1, _ := homeWithClaudeMd(t, src)
		wantCode(t, render(t, h1, "--validate"), 0)
		h2, _ := homeWithClaudeMd(t, src)
		match(t, render(t, h2, "--check", "--only", "claude-code").Out(), `DRIFT\s+~/\.claude/CLAUDE\.md`)
	})

	for _, c := range []struct {
		name    string
		breakIt func(string) string
	}{
		{"no start marker", rep(startMarker, "")},
		{"no end marker", rep(endMarker, "")},
		{"a duplicated start marker", rep(endMarker, startMarker+"\n"+endMarker)},
		{"a duplicated end marker", func(s string) string { return s + "\n" + endMarker + "\n" }},
		{"the end marker before the start marker", func(s string) string {
			return rep("@@S@@", endMarker)(rep(endMarker, startMarker)(rep(startMarker, "@@S@@")(s)))
		}},
	} {
		t.Run(c.name+": the render fails, says why, and writes nothing", func(t *testing.T) {
			t.Parallel()
			src := c.breakIt(surrounded(t))
			home, file := homeWithClaudeMd(t, src)
			r := install(t, home, repoRoot)
			wantCode(t, r, 1)
			match(t, r.Out(), `claude-md-markers\s+~/\.claude/CLAUDE\.md\s+.*jhon:(start|end)`)
			equal(t, support.Read(t, file), src)
			// Only the release the install placed, beside the untouched CLAUDE.md.
			rest := []string{}
			for _, e := range entries(t, home) {
				if !strings.HasPrefix(e, ".local") {
					rest = append(rest, e)
				}
			}
			equal(t, rest, []string{".claude", filepath.Join(".claude", "CLAUDE.md")})
		})
	}

	t.Run("an absent CLAUDE.md is skipped with a note and never created", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		r := install(t, home, repoRoot, "--only", "claude-code")
		wantCode(t, r, 0)
		match(t, r.Out(), `skip\s+~/\.claude/CLAUDE\.md is absent`)
		if exists(filepath.Join(home, ".claude", "CLAUDE.md")) {
			t.Error("CLAUDE.md was created")
		}
	})

	// Every agent that loads CLAUDE.md files receives the persona and the
	// always-on contracts inline, in load order.
	t.Run("an agent that loads CLAUDE.md files carries the persona and every always-on contract inline", func(t *testing.T) {
		t.Parallel()
		home := renderedHome(t)
		orch := claudeAgent(t, home, "jhon-orchestrator")
		var order []int
		for _, f := range []string{"persona/jhon.md", "contracts/language.md", "contracts/runtime-boundaries.md", "contracts/tdd.md", "contracts/routing.md"} {
			first := strings.Split(strings.TrimSpace(support.Read(t, treePath(repoRoot, f))), "\n")[0]
			order = append(order, strings.Index(orch, first))
		}
		for _, at := range order {
			if at <= 0 {
				t.Fatalf("an always-on file is missing from the orchestrator: %v", order)
			}
		}
		if !slices.IsSorted(order) {
			t.Errorf("the always-on files are out of load order: %v", order)
		}
		noMatch(t, orch, `(?m)^@~/\.local/share/jhon/current/`, "an @import cannot load from an agent definition")
	})

	t.Run("a writer carries the always-on layer, as every agent does", func(t *testing.T) {
		t.Parallel()
		match(t, claudeAgent(t, renderedHome(t), "worker"), `\*\*A safety refusal is a stop\.\*\*`, "the worker's layer")
	})
}

func TestRenderGuards(t *testing.T) {
	t.Parallel()

	// No agent creates, deletes, replaces, or modifies a VCS directory
	// (contracts/ldd.md). Every agent that can Edit or Write carries
	// the guard, scoped to itself; an agent that cannot write carries none.
	t.Run("the vocabulary declares the VCS directories", func(t *testing.T) {
		t.Parallel()
		equal(t, vcsDirs(t), []string{".git", ".hg", ".svn"})
	})

	t.Run("every write-capable agent gets the VCS guard with --agent <self>; agents without write do not", func(t *testing.T) {
		t.Parallel()
		home := renderedHome(t)
		vcs := guardWords(home, "vcs-guard")
		writers, others := 0, 0
		for _, name := range allManifestAgents(t) {
			gs := guardsOf(t, home, name, vcs)
			if contains(manifestCaps(t, name), "write") {
				writers++
				if len(gs) != 1 {
					t.Fatalf("%s: expected one VCS guard, got %d", name, len(gs))
				}
				equal(t, gs[0].matcher, "Edit|MultiEdit|Write|NotebookEdit", name)
				equal(t, gs[0].command, quoted(append(append(append([]string{}, vcs...), "--agent", name), vcsDirs(t)...)), name)
			} else {
				others++
				equal(t, len(gs), 0, name+" cannot write and must carry no VCS guard")
			}
		}
		if writers < 5 || others < 5 {
			t.Errorf("writers %d, others %d", writers, others)
		}
	})

	// Destructive and outward-facing Bash is judged by one guard, which every
	// agent holding Bash carries, scoped to itself, the orchestrator included.
	// Local commits are not refused: any agent holding Bash may commit.
	t.Run("every agent with Bash carries the safety guard on Bash, scoped to itself; an agent without Bash carries none", func(t *testing.T) {
		t.Parallel()
		home := renderedHome(t)
		safety := guardWords(home, "safety-guard")
		var withBash []string
		for _, name := range allManifestAgents(t) {
			gs := guardsOf(t, home, name, safety)
			if contains(claudeTools(frontmatter(claudeAgent(t, home, name))), "Bash") {
				withBash = append(withBash, name)
				if len(gs) != 1 {
					t.Fatalf("%s: %v", name, gs)
				}
				equal(t, gs[0].matcher, "Bash", name)
				equal(t, gs[0].command, quoted(append(append([]string{}, safety...), "--agent", name)), name)
			} else {
				equal(t, len(gs), 0, name+" has no Bash and must carry no safety guard")
			}
		}
		for _, name := range []string{"jhon-orchestrator", "worker", "verify", "review-risk"} {
			if !contains(withBash, name) {
				t.Error(name)
			}
		}
	})

	t.Run("only the reviewers carry the read-only inspect guard", func(t *testing.T) {
		t.Parallel()
		home := renderedHome(t)
		reviewers := []string{"review-risk", "review-resilience", "review-readability", "review-reliability", "review-refuter", "review-validator", "jd-judge-a", "jd-judge-b"}
		for _, name := range allManifestAgents(t) {
			want := 0
			if contains(reviewers, name) {
				want = 1
			}
			equal(t, len(guardsOf(t, home, name, guardWords(home, "inspect-guard"))), want, name)
		}
	})

	// Docket AV2. framework-guard rides on every Jhon agent, the orchestrator
	// included, on Edit, Write, and Bash, with the protected paths as
	// arguments: the source root, the releases, current, the CLI link,
	// CLAUDE.md, and every projection the render owns.
	t.Run("framework-guard (AV2): every agent carries it on Edit, Write, and Bash with the protected paths, scoped to itself", func(t *testing.T) {
		t.Parallel()
		home := renderedHome(t)
		guard := guardWords(home, "framework-guard")
		share := filepath.Join(home, ".local", "share", "jhon")
		agents := allManifestAgents(t)
		expected := []string{
			repoRoot, filepath.Join(share, "releases"), filepath.Join(share, "current"), filepath.Join(home, ".local", "bin", "jhon"), filepath.Join(home, ".claude", "CLAUDE.md"),
			filepath.Join(home, ".config", "jhon", "profiles.json"),
			filepath.Join(home, ".claude", "agents", ".jhon-owned"),
		}
		for _, a := range sorted(agents) {
			expected = append(expected, filepath.Join(home, ".claude", "agents", a+".md"))
		}
		for _, f := range []string{"settings.json", "jhon.json", "mcp.json", "jhon-orchestrator.md", "jhon-orchestrator-writer.md", "agents", "prompts"} {
			expected = append(expected, filepath.Join(home, ".pi", "jhon", f))
		}
		for _, name := range agents {
			gs := guardsOf(t, home, name, guard)
			if len(gs) != 1 {
				t.Fatalf("%s: %v", name, gs)
			}
			equal(t, gs[0].matcher, "Edit|MultiEdit|Write|Bash", name)
			words := append(append(append([]string{}, guard...), "--agent", name), expected...)
			equal(t, gs[0].command, quoted(words), name)
		}
	})
}

// The guards ride only in each Jhon agent's frontmatter: a session started
// with `--agent <name>` runs its agent's hooks on its own calls (spike
// claude-code-main-agent-hooks), and a plain session is the user's. The
// install removes the entries an earlier release added, beside the user's own
// hooks, which it never touches.

func settingsPath(home string) string { return filepath.Join(home, ".claude", "settings.json") }

var foreignHook = map[string]any{"matcher": "Bash", "hooks": []any{map[string]any{"type": "command", "command": "rtk hook claude"}}}

// oldJhonHooks are the entries an earlier release added to settings.json for
// the top-level session.
func oldJhonHooks(t testing.TB, home string) []any {
	t.Helper()
	vcs := []string{hookPath(home, "vcs-guard.mjs")}
	vcs = append(vcs, vcsDirs(t)...)
	cmd := func(c string) []any { return []any{map[string]any{"type": "command", "command": c}} }
	return []any{
		map[string]any{"matcher": "Bash", "hooks": cmd("'" + hookPath(home, "safety-guard.mjs") + "'")},
		map[string]any{"matcher": "Edit|Write|NotebookEdit", "hooks": cmd(quoted(vcs))},
		map[string]any{"matcher": "Edit|Write|NotebookEdit|Bash", "hooks": cmd("'" + hookPath(home, "framework-guard.mjs") + "' '--agent' 'top-level'")},
	}
}

func writeJSON(t testing.TB, p string, v any, indent bool) string {
	t.Helper()
	var b []byte
	var err error
	if indent {
		b, err = json.MarshalIndent(v, "", "  ")
	} else {
		b, err = json.Marshal(v)
	}
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if indent {
		s += "\n"
	}
	write(t, p, s)
	return s
}

func preToolUseOf(t testing.TB, home string) any {
	t.Helper()
	return readJSON(t, settingsPath(home)).(map[string]any)["hooks"].(map[string]any)["PreToolUse"]
}

func TestRenderSettings(t *testing.T) {
	t.Parallel()

	t.Run("a settings.json with disableAllHooks true refuses the install before anything is written", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		text := writeJSON(t, settingsPath(home), map[string]any{"disableAllHooks": true, "hooks": map[string]any{"PreToolUse": []any{foreignHook}}}, true)
		r := install(t, home, repoRoot)
		wantCode(t, r, 1)
		match(t, r.Out(), `disableAllHooks`)
		equal(t, support.Read(t, settingsPath(home)), text)
		equal(t, exists(filepath.Join(home, ".claude", "agents", "jhon-orchestrator.md")), false, "an agent was projected")
		equal(t, exists(filepath.Join(home, ".agents", "skills")), false, "a skill was projected")
		equal(t, exists(filepath.Join(home, ".cache", "jhon", "install.json")), false, "the install was recorded")
	})

	t.Run("the install links no command into ~/.claude/commands, prunes every Jhon command link there, and keeps the user's", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		cmds := filepath.Join(home, ".claude", "commands")
		current := filepath.Join(home, ".local", "share", "jhon", "current")
		files := []string{"jhon-old-onboard.md", "jhon-old-new.md"}
		write(t, filepath.Join(cmds, "mine.md"), "my command\n")
		for _, f := range files {
			if err := os.Symlink(filepath.Join(current, "commands", f), filepath.Join(cmds, f)); err != nil {
				t.Fatal(err)
			}
		}
		r := install(t, home, repoRoot)
		wantCode(t, r, 0)
		for _, f := range files {
			match(t, r.Out(), `prune\s+~/\.claude/commands/`+regexp.QuoteMeta(f))
		}
		equal(t, readDirNames(t, cmds), []string{"mine.md"})
		equal(t, support.Read(t, filepath.Join(cmds, "mine.md")), "my command\n")
		if strings.Contains(install(t, home, repoRoot).Out(), "prune") {
			t.Error("a re-install pruned again")
		}
	})

	t.Run("the install removes the Jhon guards from settings.json, keeps the user's hooks and keys, and a re-install changes nothing", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		sessionStart := []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "echo hi"}}}}
		writeJSON(t, settingsPath(home), map[string]any{"theme": "dark", "hooks": map[string]any{
			"PreToolUse": append([]any{foreignHook}, oldJhonHooks(t, home)...), "SessionStart": sessionStart}}, true)
		wantCode(t, install(t, home, repoRoot), 0)
		s := readJSON(t, settingsPath(home)).(map[string]any)
		equal(t, s["theme"], "dark")
		equal(t, s["hooks"].(map[string]any)["SessionStart"], jsonOf(sessionStart))
		equal(t, s["hooks"].(map[string]any)["PreToolUse"], jsonOf([]any{foreignHook}))
		bytes := support.Read(t, settingsPath(home))
		again := install(t, home, repoRoot)
		wantCode(t, again, 0)
		equal(t, support.Read(t, settingsPath(home)), bytes, "a re-install rewrote settings.json")
		match(t, again.Out(), `ok\s+~/\.claude/settings\.json`)
	})

	t.Run("an absent settings.json is not created; --check reports a leftover Jhon guard as drift and writes nothing", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		wantCode(t, install(t, home, repoRoot), 0)
		if exists(settingsPath(home)) {
			t.Fatal("settings.json was created")
		}
		writeJSON(t, settingsPath(home), map[string]any{"hooks": map[string]any{"PreToolUse": append([]any{foreignHook}, oldJhonHooks(t, home)...)}}, false)
		match(t, render(t, home, "--check").Out(), `DRIFT\s+~/\.claude/settings\.json`)
		equal(t, len(preToolUseOf(t, home).([]any)), 4, "--check wrote settings.json")
	})

	t.Run("a settings.json holding only Jhon guards loses its emptied hooks keys", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		writeJSON(t, settingsPath(home), map[string]any{"theme": "dark", "hooks": map[string]any{"PreToolUse": oldJhonHooks(t, home)}}, false)
		wantCode(t, install(t, home, repoRoot), 0)
		equal(t, readJSON(t, settingsPath(home)), jsonOf(map[string]any{"theme": "dark"}))
	})

	t.Run("a stale Jhon entry is removed, and an empty matcher group left by it is dropped", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		stale := map[string]any{"matcher": "Bash", "hooks": []any{map[string]any{"type": "command", "command": "'" + hookPath(home, "old-guard.mjs") + "' --x"}}}
		mixed := map[string]any{"matcher": "Bash", "hooks": []any{
			map[string]any{"type": "command", "command": "rtk hook claude"},
			map[string]any{"type": "command", "command": "'" + hookPath(home, "safety-guard.mjs") + "' --old"}}}
		writeJSON(t, settingsPath(home), map[string]any{"hooks": map[string]any{"PreToolUse": []any{stale, mixed}}}, false)
		wantCode(t, install(t, home, repoRoot), 0)
		equal(t, preToolUseOf(t, home), jsonOf([]any{foreignHook}))
	})

	t.Run("a settings.json that is not valid JSON is never overwritten, and the install fails", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		write(t, settingsPath(home), "{ not json")
		r := install(t, home, repoRoot)
		if r.Code == 0 {
			t.Fatal(r.Out())
		}
		match(t, r.Out(), `settings\.json`)
		equal(t, support.Read(t, settingsPath(home)), "{ not json")
		equal(t, exists(filepath.Join(home, ".claude", "agents", "jhon-orchestrator.md")), false, "an agent was projected before the settings preflight")
	})
}

func TestRenderPlugin(t *testing.T) {
	t.Parallel()
	plugin := filepath.Join(repoRoot, "runtimes", "claude-code", "plugin")

	// One command, /jhon-ldd <action> [change], in place of the ten per-action commands of earlier releases.
	t.Run("the /jhon-ldd command is the release plugin's one user-invoked skill, named as the command, with every action", func(t *testing.T) {
		t.Parallel()
		equal(t, readJSON(t, filepath.Join(plugin, ".claude-plugin", "plugin.json")).(map[string]any)["name"], "jhon")
		names := readDirNames(t, filepath.Join(plugin, "skills"))
		equal(t, names, []string{"jhon-ldd"})
		b := support.Read(t, filepath.Join(plugin, "skills", "jhon-ldd", "SKILL.md"))
		for _, action := range []string{"new", "ff", "continue", "status", "explore", "research", "apply", "verify", "archive", "init"} {
			match(t, b, "(?m)^\\| `"+action+"` \\|", action)
		}
		for _, n := range names {
			fm := frontmatter(support.Read(t, filepath.Join(plugin, "skills", n, "SKILL.md")))
			match(t, fm, `(?m)^name: `+n+`$`, n)
			match(t, fm, `(?m)^disable-model-invocation: true$`, n)
			match(t, fm, `(?m)^description: \S`, n)
		}
		if exists(filepath.Join(repoRoot, "commands")) {
			t.Error("commands/ still ships")
		}
	})

	t.Run("the primary agent may call the plugin's read-only jhon MCP tools; no sub-agent may", func(t *testing.T) {
		t.Parallel()
		home := renderedHome(t)
		for _, name := range []string{"jhon-orchestrator"} {
			tools := claudeTools(frontmatter(claudeAgent(t, home, name)))
			for _, x := range []string{"mcp__plugin_jhon_jhon__audit", "mcp__plugin_jhon_jhon__status"} {
				if !contains(tools, x) {
					t.Errorf("%s lacks %s: %v", name, x, tools)
				}
			}
		}
		for _, name := range []string{"worker", "verify", "explore"} {
			if slices.ContainsFunc(claudeTools(frontmatter(claudeAgent(t, home, name))), func(s string) bool { return strings.HasPrefix(s, "mcp__plugin_jhon_") }) {
				t.Error(name)
			}
		}
	})

	// Under go test the binary is not built at build/jhon in the checkout, so
	// the command is resolved in an installed release, which is where it runs.
	t.Run("the release plugin declares the jhon MCP server, run from the release's own CLI", func(t *testing.T) {
		t.Parallel()
		equal(t, readJSON(t, filepath.Join(plugin, ".mcp.json")), jsonOf(map[string]any{"mcpServers": map[string]any{"jhon": map[string]any{
			"command": "${CLAUDE_PLUGIN_ROOT}/../../../build/jhon", "args": []any{"mcp"}}}}))
		current := filepath.Join(renderedHome(t), ".local", "share", "jhon", "current")
		if !exists(filepath.Join(current, "runtimes", "claude-code", "plugin", "..", "..", "..", "build", "jhon")) {
			t.Error("the plugin's command does not resolve in the release")
		}
	})
}

// A write render records when the framework was installed and from which
// commit, at ~/.cache/jhon/install.json, so `jhon sessions` and the guards can
// tell a session that started before it (docket AI1).

var recordPath = filepath.Join(".cache", "jhon", "install.json")

func readRecord(t testing.TB, home string) map[string]any {
	t.Helper()
	return readJSON(t, filepath.Join(home, recordPath)).(map[string]any)
}

// expectedCommit is the commit an install from the checkout records: its
// release's name (docket AV1).
func expectedCommit() string { return releaseName(repoRoot) }

func TestRenderInstallRecord(t *testing.T) {
	t.Parallel()

	t.Run("a write render records installed_at and the framework commit", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		before := time.Now()
		r := install(t, home, repoRoot)
		wantCode(t, r, 0)
		rec := readRecord(t, home)
		at, err := time.Parse(time.RFC3339Nano, rec["installed_at"].(string))
		if err != nil || at.Before(before.Add(-time.Second)) || at.After(time.Now().Add(time.Second)) {
			t.Errorf("installed_at %v is not the render's time (%v)", rec["installed_at"], err)
		}
		equal(t, rec["commit"], expectedCommit())
		match(t, r.Out(), `install:\n\s+write\s+~/\.cache/jhon/install\.json`)
	})

	t.Run("a render with nothing new keeps the recorded install time", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		wantCode(t, install(t, home, repoRoot), 0)
		first := readRecord(t, home)
		r := install(t, home, repoRoot)
		wantCode(t, r, 0)
		equal(t, readRecord(t, home), first)
		match(t, r.Out(), `install:\n\s+ok\s+~/\.cache/jhon/install\.json`)
	})

	t.Run("a render from another commit moves the install time, even with no drift", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		wantCode(t, install(t, home, repoRoot), 0)
		write(t, filepath.Join(home, recordPath), `{"installed_at":"2020-01-01T00:00:00.000Z","commit":"0000000"}`+"\n")
		wantCode(t, install(t, home, repoRoot), 0)
		rec := readRecord(t, home)
		if rec["installed_at"] == "2020-01-01T00:00:00.000Z" {
			t.Error("the install time did not move")
		}
		equal(t, rec["commit"], expectedCommit())
	})

	// Docket AV1 replaces the expectation this test held ("a copy outside git
	// records the commit as unknown"): an install now runs only from a
	// release, whose directory name is the captured commit, so a checkout or a
	// copy outside the releases root installs nothing at all.
	t.Run("--install from a copy outside the releases root refuses and writes nothing", func(t *testing.T) {
		t.Parallel()
		repo := support.CopyRepo(t)
		home := t.TempDir()
		r := renderIn(t, home, repo, "--install", "--source", repo)
		wantCode(t, r, 2)
		match(t, r.Out(), `not a release`)
		equal(t, len(entries(t, home)), 0)
	})
}

func TestRenderOrchestratorBody(t *testing.T) {
	t.Parallel()

	// Item 6: risk tier, question triggers, the record and the route, handoffs.
	t.Run("the orchestrator acts on jhon risk, keeps the record always and the rigor by route, and hands off the request verbatim", func(t *testing.T) {
		t.Parallel()
		b := flat(claudeAgent(t, renderedHome(t), "jhon-orchestrator"))
		match(t, b, "run `jhon risk`", "the risk tier drives verification")
		match(t, b, "`verify` and an automatic 4R with only the pertinent lenses", "L9: risk high gets verify and a 4R by need")
		match(t, b, "`review_required` always wins; Judgment Day only when the owner asks", "L9, S17: review_required wins, Judgment Day only on request")
		match(t, b, `The record is always; the rigor is the change's, not\s+yours`, "LDD: the record goes with every change; the route sets only the rigor")
		match(t, b, `the owner's request verbatim`, "handoffs carry the request")
		match(t, b, `also fails on the base is reported as pre-existing`, "blockers are measured against the base")
		match(t, b, `While planning \(exploring, proposing, writing specs, design, tasks, or the\s+next slice's plan\), ask the owner every open point and assume nothing`, "planning asks every open point (L178)")
		match(t, b, `Once the owner accepts the plan, ask only a real doubt it does not\s+answer, one that changes the scope`, "the question triggers ride in the always-on layer")
		match(t, b, `Every question names the answer you recommend and\s+why, from the project's context`, "each question carries a recommended answer (L179)")
		match(t, b, `Inside an authorized change the next step is not a question`, "a slice's next phase follows its report")
		match(t, b, `Size each worker slice to fit its context budget; the worker checkpoints its progress and partial state at its context notice and returns what is done and what remains: hand the remainder to a fresh worker`, "T131, L905, L908: no turn cap; the context budget sizes a slice")
		noMatch(t, b, `(?i)turn cap|\d+-turn|before the cap`, "T131: the orchestrator names no turn cap")
		match(t, b, `A slice that crosses layers runs as passes, one layer each, in\s+dependency order: pure domain; persistence \(migration, models, migration\s+test\); application \(commands, handlers, repository port and adapter\);\s+API and record \(route, schemas, permission rows, contract test, docs\)\.\s+One worker per pass, its brief naming its exact files from tasks\.md, its\s+Allowed edit surfaces and its one commit, each under the child\s+budget \(wrap-up 80K/40%, cap 150K/75%\); independent passes run in\s+parallel, each in its own worktree\.`, "T126, L797: a multi-layer slice runs as one worker per layer pass")
		match(t, b, `One session per change`, "S35: a fresh session after an archive or at half the context")
		match(t, b, `A suite the PR's CI runs whole is never run whole here too`, "in a client project a local full integration suite (35 to 56 min) repeated CI's")
		match(t, b, "For a wide or long run, tell the owner the `--dry-run` estimate, and when it should end, as you start the run, never a guess from the files changed", "the owner asked how it was going 4 times in a client project; L38, S22: an estimate only for wide or long runs")
		match(t, b, "with `delivery: change` in `openspec/config.yaml`, committed on the change's branch and merged once with rebase after the last", "one PR and one CI run per change")
	})

	t.Run("the orchestrator builds a handoff with jhon context --handoff", func(t *testing.T) {
		t.Parallel()
		home := renderedHome(t)
		for _, name := range []string{"jhon-orchestrator"} {
			match(t, flat(claudeAgent(t, home, name)), `jhon context <change> --phase none --handoff`, name)
		}
	})

	// Item 8: the /clear notice.
	t.Run("the main agent carries the context-notice Stop hook; no executor does", func(t *testing.T) {
		t.Parallel()
		home := renderedHome(t)
		stop := regexp.MustCompile(`(?m)^  Stop:\n    - hooks:\n        - type: command\n          command: ".*'hook' 'context-notice'"$`)
		for _, name := range renderedAgents(t, home) {
			fm := frontmatter(claudeAgent(t, home, name))
			equal(t, stop.MatchString(fm), name == "jhon-orchestrator", name)
		}
	})

	// Item 10: persona/jhon.md governs reply text to the user (its "Persona
	// Scope"); a sub-agent answers the orchestrator. Measured in a real
	// session: the persona was 5.2 KB of every sub-agent's 38 KB of loaded
	// instructions.
	t.Run("the persona rides only in the main agent; every agent that loads CLAUDE.md files carries the contracts", func(t *testing.T) {
		t.Parallel()
		home := renderedHome(t)
		for _, name := range []string{"jhon-orchestrator"} {
			match(t, claudeAgent(t, home, name), `## Response Length Contract`, name)
		}
		for _, name := range []string{"verify", "review-risk", "jd-judge-a", "explore"} {
			md := claudeAgent(t, home, name)
			noMatch(t, md, `## Response Length Contract`, name+" carries the persona")
			match(t, md, `\*\*A safety refusal is a stop\.\*\*`, name+" lacks the contracts")
		}
	})

	// T65 (L989-L990): the persona told the main agent to ask one question at
	// a time, and the owner got more than half of the questions in plain text.
	t.Run("the persona sends every question that waits for the user through the ask tool, in one round, on both runtimes", func(t *testing.T) {
		t.Parallel()
		_, pi := sharedPiHome(t)
		for _, c := range []struct{ runtime, prompt, tool, foreign string }{
			{"claude-code", claudeAgent(t, renderedHome(t), "jhon-orchestrator"), "AskUserQuestion", "ask_user"},
			{"pi", support.Read(t, filepath.Join(pi, "jhon-orchestrator.md")), "ask_user", "AskUserQuestion"},
		} {
			b := flat(c.prompt)
			match(t, b, "Every question that waits for the user goes through your ask tool \\(`"+c.tool+"`; without one, text\\): all of them in one round, up to four, each with options, recommended first, a typed answer allowed\\. Then stop and wait\\.", c.runtime+": the ask-tool rule, in its own tool's name")
			noMatch(t, b, c.foreign, c.runtime+": the other runtime's ask tool")
			match(t, b, `No option menus, exhaustive lists, or multiple approaches in reply text without a real fork\.`, c.runtime+": menus are ruled in reply text only")
			noMatch(t, b, `(?i)one question at a time`, c.runtime+": the old rule")
			noMatch(t, b, `## When Asking Questions`, c.runtime+": the old rule's duplicate")
			noMatch(t, b, `Do not offer option menus`, c.runtime+": menus ruled for every channel")
		}
	})

	// Item 10 (N4): hu-ab-001 ran three explores and two designs for one
	// change; design context peaks reached ~300K in every audited session.
	t.Run("explore extends an existing exploration, design reads within a budget, and the orchestrator reuses the explore", func(t *testing.T) {
		t.Parallel()
		read := func(p string) string { return flat(support.Read(t, treePath(repoRoot, p))) }
		match(t, read("skills/ldd/references/explore.md"), `already exists, read it first and extend it`)
		match(t, read("skills/ldd/references/design.md"), `Read budget:`)
		match(t, read("skills/ldd/references/design.md"), `exploration\.md`)
		match(t, read("orchestrator.md"), `One explore per change`)
	})

	// A client project, pi: a slice's tests ran as loose pytest calls before its commit,
	// so no run was recorded and deliver check had nothing to read.
	t.Run("the orchestrator runs a slice's tests through jhon executions, from the change's own worktree", func(t *testing.T) {
		t.Parallel()
		body := flat(support.Read(t, treePath(repoRoot, "orchestrator.md")))
		match(t, body, "before its commit, a slice's tests run as `jhon executions run <change> --affected <base>` from the worktree that holds the change \\(each lane's from its own\\), never as a loose `pytest` or `npm test`")
	})
}

// pi runs Jhon on its own agent directory, ~/.pi/jhon (internal/render,
// pi.go): the owner's ~/.pi/agent stays theirs. The spikes behind it are
// test/testdata/spikes/pi-blocking-hook, pi-named-subagents, and pi-agent-tool-allowlist.

func piHomeAt(t testing.TB, home string, prepare func(home string)) (pi, out string, err string) {
	write(t, filepath.Join(home, ".pi", "agent", "auth.json"), "{}\n")
	if prepare != nil {
		prepare(home)
	}
	r := install(t, home, repoRoot, "--only", "pi")
	if r.Code != 0 {
		return "", "", fmt.Sprintf("exit %d\n%s", r.Code, r.Out())
	}
	return filepath.Join(home, ".pi", "jhon"), r.Out(), ""
}

func piHome(t *testing.T, prepare func(home string)) (home, pi string) {
	t.Helper()
	home = t.TempDir()
	pi, _, err := piHomeAt(t, home, prepare)
	if err != "" {
		t.Fatal(err)
	}
	return home, pi
}

var piShared lazyHome

// sharedPiHome is a plain pi render, shared read-only.
func sharedPiHome(t *testing.T) (home, pi string) {
	t.Helper()
	home = piShared.get(t, func(home string) string {
		_, _, err := piHomeAt(t, home, nil)
		return err
	})
	return home, filepath.Join(home, ".pi", "jhon")
}

// piPackages are the pinned packages, pi-pretty (installed, loaded by jhon-shell), then jhon-shell,
// the release's own pi package. No gentle-pi: the shell carries everything it bridged.
func piPackages(current string) []any {
	pretty := map[string]any{"source": "npm:@heyhuynhgiabuu/pi-pretty@0.6.27", "extensions": []any{}, "skills": []any{}, "prompts": []any{}, "themes": []any{}}
	return []any{"npm:pi-claude-bridge@0.9.3", "npm:pi-web-access@0.43.0", pretty, filepath.Join(current, "shell")}
}

func TestRenderPi(t *testing.T) {
	t.Parallel()

	t.Run("pi: settings load plain pi with the model bridge and web access, the Jhon extension and skills from the release, and the orchestrator's model; mcp.json mounts Engram, codegraph, jhon, and context7", func(t *testing.T) {
		t.Parallel()
		home, pi := sharedPiHome(t)
		s := readJSON(t, filepath.Join(pi, "settings.json")).(map[string]any)
		current := filepath.Join(home, ".local", "share", "jhon", "current")
		equal(t, s["packages"], piPackages(current))
		equal(t, s["theme"], "rose-ink", "rose ink, with no desktop palette too (L50)")
		equal(t, readJSON(t, filepath.Join(pi, "mcp.json")), jsonOf(map[string]any{"mcpServers": map[string]any{
			"engram":    map[string]any{"command": "engram", "args": []any{"mcp", "--tools=agent"}, "exposure": "direct"},
			"codegraph": map[string]any{"command": "codegraph", "args": []any{"serve", "--mcp"}, "exposure": "direct"},
			"jhon":      map[string]any{"command": filepath.Join(current, "build", "jhon"), "args": []any{"mcp"}, "exposure": "direct"},
			"context7":  context7Entry}}))
		equal(t, s["extensions"], []any{"-builtin:codemode"})
		equal(t, s["skills"], []any{filepath.Join(current, "prose", "skills")})
		equal(t, []any{s["defaultProvider"], s["defaultModel"], s["defaultThinkingLevel"]}, []any{"claude-bridge", "claude-opus-5-5", "medium"})
		for _, f := range []string{"package.json", "extensions/jhon.ts"} {
			if !exists(filepath.Join(current, "shell", f)) {
				t.Error("the release carries the jhon-shell package:", f)
			}
		}
	})

	t.Run("pi: jhon.json names the release's binary, the orchestrator's prompt, and the guards' arguments, the pi files protected", func(t *testing.T) {
		t.Parallel()
		home, pi := sharedPiHome(t)
		c := readJSON(t, filepath.Join(pi, "jhon.json")).(map[string]any)
		equal(t, c["jhon"], filepath.Join(home, ".local", "share", "jhon", "current", "build", "jhon"))
		equal(t, c["orchestrator"], filepath.Join(pi, "jhon-orchestrator.md"))
		equal(t, c["vcsDirs"], jsonOf(vcsDirs(t)))
		equal(t, c["inspectAllow"], jsonOf(inspectPrefixes))
		protected, _ := c["protected"].([]any)
		for _, p := range []string{filepath.Join(pi, "settings.json"), filepath.Join(pi, "jhon.json"), filepath.Join(pi, "jhon-orchestrator.md"), filepath.Join(pi, "agents"), filepath.Join(pi, "prompts")} {
			if !slices.Contains(protected, any(p)) {
				t.Error(p)
			}
		}
		prompt := support.Read(t, c["orchestrator"].(string))
		match(t, prompt, `(?m)^# Orchestrator$`)
		match(t, prompt, `## Response Length Contract`, "the persona")
		match(t, prompt, `\*\*A safety refusal is a stop\.\*\*`, "the contracts")
		match(t, prompt, "Runtime guarantees in pi \\(agents\\.yaml `guarantees`\\): 6 enforced, 11 behavioural")
	})

	// T117b (L720): the writer-mode rules sit beside the prompt, protected,
	// and the prompt marks the region they replace.
	t.Run("pi: the writer-mode rules ship beside the orchestrator's prompt, which marks the region they replace", func(t *testing.T) {
		t.Parallel()
		_, pi := sharedPiHome(t)
		c := readJSON(t, filepath.Join(pi, "jhon.json")).(map[string]any)
		writer := filepath.Join(pi, "jhon-orchestrator-writer.md")
		protected, _ := c["protected"].([]any)
		if !slices.Contains(protected, any(writer)) {
			t.Error("unprotected:", writer)
		}
		w := flat(support.Read(t, writer))
		match(t, w, `^\*\*Writer mode\.\*\*`)
		match(t, w, "at most ~20 lines", "the inline bound")
		match(t, w, "the spec lines, the exact files and line ranges, the decisions already taken, and how to verify", "the brief")
		match(t, w, "Every sub-agent and long job runs in the background\\.", "background")
		// T117e (L746): the bound counts per owner request; reading past one batch goes to the brief.
		match(t, w, "The guard sums changed lines across calls since the owner's last message", "the per-request bound")
		match(t, w, "Reading beyond one read batch goes into the worker's brief", "the brief carries the reading")
		prompt := support.Read(t, filepath.Join(pi, "jhon-orchestrator.md"))
		b, e := strings.Index(prompt, "<!-- delegation:begin -->"), strings.Index(prompt, "<!-- delegation:end -->")
		equal(t, strings.Count(prompt, "<!-- delegation:begin -->"), 1, "begin markers")
		equal(t, strings.Count(prompt, "<!-- delegation:end -->"), 1, "end markers")
		if b < 0 || e < b {
			t.Errorf("markers out of order: begin %d, end %d", b, e)
		} else {
			// T117e (L746): inline mode is organic, delegating only on objective triggers.
			r := flat(prompt[b:e])
			for _, want := range []string{"never on price or size alone", `\*\*Read budget\.\*\*`, `\*\*Writer rule\.\*\*`, `\*\*Context backstop\.\*\*`, `\*\*Independence\.\*\*`, "small changes stay inline even when independent"} {
				match(t, r, want, "the organic triggers")
			}
		}
	})

	t.Run("pi: every agent but the primary ones is a pi definition with its tier's model and the tools its capabilities give there", func(t *testing.T) {
		t.Parallel()
		_, pi := sharedPiHome(t)
		_, delegable := manifestAgents(t)
		var defs []string
		for _, f := range readDirNames(t, filepath.Join(pi, "agents")) {
			if strings.HasSuffix(f, ".md") {
				defs = append(defs, strings.TrimSuffix(f, ".md"))
			}
		}
		equal(t, sorted(defs), sorted(delegable))
		fm := func(name string) string { return frontmatter(support.Read(t, filepath.Join(pi, "agents", name+".md"))) }
		match(t, fm("worker"), `(?m)^model: claude-bridge/claude-opus-5-5\nthinking: medium$`)
		match(t, fm("verify"), `(?m)^model: claude-bridge/claude-opus-5-5$`)
		// T131 (L905, L908): no agent has a turn cap; every executor runs until it returns.
		for _, name := range delegable {
			noMatch(t, fm(name), `(?m)^maxTurns:`, name)
		}
		equal(t, listItems(fm("worker")), []string{"read", "grep", "find", "ls", "edit", "write", "bash", "mcp__engram__mem_search", "mcp__engram__mem_get_observation",
			"mcp__engram__mem_save", "mcp__engram__mem_update", "mcp__engram__mem_judge", "mcp__codegraph__codegraph_explore", "todo", "mcp__context7__resolve_library_id", "mcp__context7__query_docs"})
		// T134 H7a (L1019): the executors keep their own todo list in pi; no other agent has it.
		for _, name := range delegable {
			holds := contains(listItems(fm(name)), "todo")
			if want := name == "worker" || name == "explore" || name == "verify"; holds != want {
				t.Errorf("pi %s: holds todo %v, want %v", name, holds, want)
			}
		}
		equal(t, listItems(fm("review-risk")), []string{"inspect", "mcp__codegraph__codegraph_explore"}, "a reviewer inspects without a shell")
		if ex := listItems(fm("explore")); !contains(ex, "web_search") || !contains(ex, "fetch_content") {
			t.Error(ex)
		}
		for _, name := range []string{"worker", "verify"} {
			match(t, support.Read(t, filepath.Join(pi, "agents", name+".md")), `\*\*A safety refusal is a stop\.\*\*`, name)
		}
	})

	t.Run("pi: the credentials and key remaps are links to the owner's, made only where nothing stands", func(t *testing.T) {
		t.Parallel()
		home, pi := piHome(t, func(h string) {
			write(t, filepath.Join(h, ".pi", "agent", "claude-bridge.json"), "{\"owner\":true,\"provider\":{\"plan\":\"max\",\"pathToClaudeCodeExecutable\":\"/opt/claude\"}}\n")
			write(t, filepath.Join(h, ".pi", "agent", "keybindings.json"), "{}\n")
		})
		for _, f := range []string{"auth.json", "keybindings.json"} {
			link, err := os.Readlink(filepath.Join(pi, f))
			if err != nil {
				t.Fatal(err)
			}
			equal(t, link, filepath.Join(home, ".pi", "agent", f))
		}
		// claude-bridge.json is the owner's, every key kept, its Claude Code the
		// provider wrapper, which runs the owner's executable with the
		// owner's hooks off only when the bridge runs it with no tools.
		bridge := readJSON(t, filepath.Join(pi, "claude-bridge.json")).(map[string]any)
		equal(t, bridge["owner"], true)
		provider := bridge["provider"].(map[string]any)
		equal(t, provider["plan"], "max")
		equal(t, provider["pathToClaudeCodeExecutable"], filepath.Join(pi, "claude-code-provider"))
		wrapper := support.Read(t, filepath.Join(pi, "claude-code-provider"))
		match(t, wrapper, `^#!/bin/sh\n`)
		match(t, wrapper, `real='/opt/claude'`)
		fi, err := os.Stat(filepath.Join(pi, "claude-code-provider"))
		if err != nil || fi.Mode()&0o111 == 0 {
			t.Fatalf("the wrapper is not executable: %v", err)
		}
	})

	t.Run("pi: an earlier link to the owner's claude-bridge.json is replaced, the owner's file untouched, and the wrapper runs the provider with the owner's hooks off", func(t *testing.T) {
		t.Parallel()
		var owner string
		_, pi := piHome(t, func(h string) {
			owner = filepath.Join(h, ".pi", "agent", "claude-bridge.json")
			fake := filepath.Join(h, "fake-claude")
			write(t, fake, "#!/bin/sh\nprintf '%s|' \"$@\"\n")
			if err := os.Chmod(fake, 0o755); err != nil {
				t.Fatal(err)
			}
			write(t, owner, "{\"provider\":{\"pathToClaudeCodeExecutable\":\""+fake+"\"}}\n")
			if err := os.MkdirAll(filepath.Join(h, ".pi", "jhon"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(owner, filepath.Join(h, ".pi", "jhon", "claude-bridge.json")); err != nil {
				t.Fatal(err)
			}
		})
		fi, err := os.Lstat(filepath.Join(pi, "claude-bridge.json"))
		if err != nil || fi.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("still a link: %v", err)
		}
		match(t, support.Read(t, owner), `^\{"provider":\{"pathToClaudeCodeExecutable":"[^"]+fake-claude"\}\}\n$`, "the owner's file is untouched")
		run := func(args ...string) string {
			out, err := exec.Command(filepath.Join(pi, "claude-code-provider"), args...).Output()
			if err != nil {
				t.Fatal(err)
			}
			return string(out)
		}
		equal(t, run("--tools=", "--model", "m"), "--settings|{\"disableAllHooks\":true}|--tools=|--model|m|", "the provider: the owner's hooks off, the rest of the settings kept")
		equal(t, run("--tools=", "--settings", `{"autoMemoryEnabled":false,"x":[1]}`), `--tools=|--settings|{"disableAllHooks":true,"autoMemoryEnabled":false,"x":[1]}|`, "inside the SDK's own --settings, never a second one")
		equal(t, run("--settings", "{}", "--tools="), `--settings|{"disableAllHooks":true}|--tools=|`)
		equal(t, run("--tools=Bash,Read", "--model", "m"), "--tools=Bash,Read|--model|m|", "AskClaude, with tools: as asked")
	})

	t.Run("pi: an agent the manifest no longer has is pruned; a file Jhon never wrote stays; a second render changes nothing", func(t *testing.T) {
		t.Parallel()
		home, pi := piHome(t, func(h string) {
			write(t, filepath.Join(h, ".pi", "jhon", "agents", ".jhon-owned"), "old-agent\n")
			write(t, filepath.Join(h, ".pi", "jhon", "agents", "old-agent.md"), "old\n")
			write(t, filepath.Join(h, ".pi", "jhon", "agents", "mine.md"), "the owner's\n")
		})
		equal(t, exists(filepath.Join(pi, "agents", "old-agent.md")), false)
		equal(t, support.Read(t, filepath.Join(pi, "agents", "mine.md")), "the owner's\n")
		again := install(t, home, repoRoot, "--only", "pi")
		wantCode(t, again, 0)
		noMatch(t, again.Out(), `\b(write|prune|link) `)
	})

	t.Run("pi: without ~/.pi nothing is created", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		r := install(t, home, repoRoot, "--only", "pi")
		wantCode(t, r, 0)
		match(t, r.Out(), `skip +~/\.pi is absent; not created`)
		equal(t, exists(filepath.Join(home, ".pi")), false)
	})

	// L194: pi's /jhon-ldd is jhon-shell's command (shell/lib/ldd-command.ts),
	// which lists its actions; the prompt template that could not goes.
	t.Run("pi: no prompt template is written; the jhon-ldd.md an earlier release wrote is pruned, the owner's own kept", func(t *testing.T) {
		t.Parallel()
		_, pi := sharedPiHome(t)
		equal(t, exists(filepath.Join(pi, "prompts", "jhon-ldd.md")), false)
		home, pi := piHome(t, func(home string) {
			prompts := filepath.Join(home, ".pi", "jhon", "prompts")
			write(t, filepath.Join(prompts, "jhon-ldd.md"), "---\ndescription: \"old\"\n---\nArguments: `$ARGUMENTS`.\n")
			write(t, filepath.Join(prompts, ".jhon-owned"), "jhon-ldd\n")
			write(t, filepath.Join(prompts, "mine.md"), "the owner's\n")
		})
		equal(t, exists(filepath.Join(pi, "prompts", "jhon-ldd.md")), false)
		equal(t, exists(filepath.Join(pi, "prompts", "mine.md")), true)
		again := install(t, home, repoRoot, "--only", "pi")
		wantCode(t, again, 0)
		noMatch(t, again.Out(), `prompts`)
	})

	t.Run("pi: the theme is rose ink, its fixed values unless the settings follow the desktop palette (L50, L197)", func(t *testing.T) {
		t.Parallel()
		// follow is the owner's settings as the migration writes them (L199).
		const follow = "schema = 1\n\n[integrations]\ndashboard = true\ntelemetry = true\n\n[integrations.theme]\npalette = \"ryoku\"\n"
		for _, c := range []struct {
			desktop  bool
			settings string
			want     string
		}{
			{false, "", "#e7bbc8"},
			{true, "", "#e7bbc8"},
			{true, "[integrations.theme]\npalette = \"rose-ink\"\n", "#e7bbc8"},
			// A file from before T15.7: follow_desktop picks nothing since T29 E6 (L329).
			{true, "[integrations.theme]\nfollow_desktop = true\n", "#e7bbc8"},
			{true, follow, "#aabbcc"},
		} {
			home, pi := piHome(t, func(h string) {
				if c.desktop {
					write(t, filepath.Join(h, ".cache", "ryoku", "colors.json"), `{"primary":"#aabbcc"}`)
				}
				if c.settings != "" {
					write(t, filepath.Join(h, ".config", "jhon", "settings.toml"), c.settings)
				}
			})
			s := readJSON(t, filepath.Join(pi, "settings.json")).(map[string]any)
			equal(t, s["theme"], "rose-ink")
			equal(t, s["packages"], piPackages(filepath.Join(home, ".local", "share", "jhon", "current")))
			th := readJSON(t, filepath.Join(pi, "themes", "rose-ink.json")).(map[string]any)
			equal(t, th["vars"].(map[string]any)["primary"], c.want, fmt.Sprintf("desktop %v, settings %q", c.desktop, c.settings))
		}
	})

	// pi rewrites settings.json at startup with its own bookkeeping keys and
	// no trailing newline (pi 1.5.0, L176/T52): not drift, and kept on install.
	piWritten := func(t *testing.T, path string, edit func(s map[string]any)) {
		t.Helper()
		s := readJSON(t, path).(map[string]any)
		s["lastChangelogVersion"] = "1.5.0"
		s["extensionChangelogVersions"] = map[string]any{`{"source":"npm:pi-web-access@0.43.0","scope":"user"}`: "0.43.0"}
		if edit != nil {
			edit(s)
		}
		b, err := json.MarshalIndent(s, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		write(t, path, string(b))
	}
	checkPi := func(t *testing.T, home string) support.Result {
		t.Helper()
		return render(t, home, "--check", "--only", "pi")
	}

	t.Run("pi: the keys pi writes to settings.json itself (lastChangelogVersion, extensionChangelogVersions) are no drift, and a re-install keeps them", func(t *testing.T) {
		t.Parallel()
		home, pi := piHome(t, nil)
		path := filepath.Join(pi, "settings.json")
		piWritten(t, path, nil)
		before := support.Read(t, path)
		r := checkPi(t, home)
		wantCode(t, r, 0)
		match(t, r.Out(), `ok\s+~/\.pi/jhon/settings\.json`)
		noMatch(t, r.Out(), `DRIFT`)
		equal(t, support.Read(t, path), before, "--check wrote settings.json")
		wantCode(t, install(t, home, repoRoot, "--only", "pi"), 0)
		s := readJSON(t, path).(map[string]any)
		equal(t, s["lastChangelogVersion"], "1.5.0")
		equal(t, s["extensionChangelogVersions"], map[string]any{`{"source":"npm:pi-web-access@0.43.0","scope":"user"}`: "0.43.0"})
		equal(t, s["theme"], "rose-ink")
	})

	t.Run("pi: the first install sets the theme to rose-ink; a later install keeps the theme the owner chose (T15.18), with no drift", func(t *testing.T) {
		t.Parallel()
		for _, theme := range []string{"kanagawa", "dark"} {
			home, pi := piHome(t, nil)
			path := filepath.Join(pi, "settings.json")
			equal(t, readJSON(t, path).(map[string]any)["theme"], "rose-ink", "first install")
			piWritten(t, path, func(s map[string]any) { s["theme"] = theme })
			before := support.Read(t, path)
			r := checkPi(t, home)
			wantCode(t, r, 0)
			noMatch(t, r.Out(), `DRIFT`)
			wantCode(t, install(t, home, repoRoot, "--only", "pi"), 0)
			equal(t, readJSON(t, path).(map[string]any)["theme"], theme, theme)
			equal(t, support.Read(t, path), before, "the install rewrote the owner's settings.json", theme)
		}
	})

	t.Run("pi: a Jhon-owned settings.json key pi or the owner changed (defaultProvider, packages) is still drift; the install restores it and keeps pi's keys", func(t *testing.T) {
		t.Parallel()
		for _, c := range []struct {
			key  string
			edit func(s map[string]any)
		}{
			{"defaultProvider", func(s map[string]any) { s["defaultProvider"] = "anthropic" }},
			{"packages", func(s map[string]any) { s["packages"] = []any{"npm:pi-claude-bridge@0.9.3"} }},
		} {
			home, pi := piHome(t, nil)
			path := filepath.Join(pi, "settings.json")
			want := readJSON(t, path).(map[string]any)
			piWritten(t, path, c.edit)
			before := support.Read(t, path)
			r := checkPi(t, home)
			wantCode(t, r, 1)
			match(t, r.Out(), `DRIFT\s+~/\.pi/jhon/settings\.json`, c.key)
			equal(t, support.Read(t, path), before, "--check wrote settings.json", c.key)
			wantCode(t, install(t, home, repoRoot, "--only", "pi"), 0)
			s := readJSON(t, path).(map[string]any)
			equal(t, s[c.key], want[c.key], c.key)
			equal(t, s["lastChangelogVersion"], "1.5.0", c.key)
			wantCode(t, checkPi(t, home), 0)
		}
	})
}

// Every projection reads the owner's active profile (internal/profiles); a
// broken one stops the install before anything is written, naming the file.
func TestRenderProfiles(t *testing.T) {
	t.Parallel()

	t.Run("a broken profiles file stops the install before anything is written", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		write(t, filepath.Join(home, ".config", "jhon", "profiles.json"), `{"active":"x","profiles":{"x":{"tiers":{"claude-code":{"mid":"claude-sonnet-5-5@turbo"}}}}}`)
		r := install(t, home, repoRoot)
		wantCode(t, r, 1)
		match(t, r.Out(), `profile: 1 problem\(s\) in ~/\.config/jhon/profiles\.json \(active: x\)`)
		match(t, r.Out(), `x\.tiers\.claude-code\.mid: effort 'turbo'`)
		equal(t, exists(filepath.Join(home, ".claude", "agents")), false)
	})

	t.Run("the active profile moves a rendered agent's model in every runtime; its own override beats the role's effort", func(t *testing.T) {
		t.Parallel()
		// jd-fix-agent declares its own effort, which the profile's override for it replaces.
		repo := support.CopyRepo(t)
		editFile(t, filepath.Join(repo, "prose", "agents.yaml"), "  - name: jd-fix-agent\n    tier: strong\n", "  - name: jd-fix-agent\n    tier: strong\n    effort: low\n")
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, ".pi"), 0o777); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(home, ".config", "jhon", "profiles.json"), `{"active":"x","profiles":{"x":{"tiers":{"pi":{"strong":"claude-bridge/claude-fable-5-1@high"}},"agents":{"jd-fix-agent":{"claude-code":"claude-opus-5-5@max"}}}}}`)
		wantCode(t, install(t, home, repo), 0)
		match(t, frontmatter(claudeAgent(t, home, "jd-fix-agent")), `(?m)^model: claude-opus-5-5\neffort: max$`)
		match(t, support.Read(t, filepath.Join(home, ".pi", "jhon", "agents", "verify.md")), `(?m)^model: claude-bridge/claude-fable-5-1\nthinking: high$`)
	})

	t.Run("settings' pi.profile picks the rendered profile over profiles.json's active (T56b)", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, ".pi"), 0o777); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(home, ".config", "jhon", "profiles.json"), `{"active":"y","profiles":{"x":{"tiers":{"pi":{"strong":"claude-bridge/claude-fable-5-1@high"}}},"y":{}}}`)
		write(t, filepath.Join(home, ".config", "jhon", "settings.toml"), "schema = 1\n[pi]\nprofile = \"x\"\n")
		wantCode(t, install(t, home, repoRoot), 0)
		match(t, support.Read(t, filepath.Join(home, ".pi", "jhon", "agents", "verify.md")), `(?m)^model: claude-bridge/claude-fable-5-1\nthinking: high$`)
	})
}

// 4R is the pertinent lenses, one bounded correction by a worker, then
// verify (S18, L19); the grant classes review and verified stay retired
// (S19). The refuter and validator agents return as the fallback after the
// lens agents (T118f, L730): both render read-only, and the contract runs
// them in "Fallback: lens agents".
func TestRenderSimplifiedFourR(t *testing.T) {
	t.Parallel()
	retired := regexp.MustCompile("jhon grant (?:verified|review)\\b|grant `(?:verified|review)`")
	var docs []string
	err := filepath.WalkDir(filepath.Join(repoRoot, "prose"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && (strings.HasSuffix(p, ".md") || strings.HasSuffix(p, ".yaml")) {
			docs = append(docs, p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	docs = append(docs, filepath.Join(repoRoot, "README.md"))
	for _, p := range docs {
		for i, line := range strings.Split(support.Read(t, p), "\n") {
			if retired.MatchString(line) {
				rel, _ := filepath.Rel(repoRoot, p)
				t.Errorf("%s:%d names a grant class S19 retires: %s", rel, i+1, line)
			}
		}
	}
	home := renderedHome(t)
	for name, role := range map[string]string{"review-refuter": "refuter", "review-validator": "validator"} {
		fm := frontmatter(claudeAgent(t, home, name))
		if regexp.MustCompile(`(?m)^tools:.*\b(Edit|Write|MultiEdit)\b|^  - (Edit|Write|MultiEdit)$`).MatchString(fm) {
			t.Errorf("%s can write: %s", name, fm)
		}
		body := claudeAgent(t, home, name)
		match(t, body, `jhon review freeze --base <ref>`, name+" freezes")
		match(t, body, `"role":"`+role+`","results":\[\{"finding_id"`, name+" answers in its role schema")
	}
	fallback := support.Read(t, filepath.Join(repoRoot, "prose", "contracts", "review-4r.md"))
	if i := strings.Index(fallback, "## Fallback: lens agents"); i >= 0 {
		fallback = flat(fallback[i:strings.Index(fallback, "## Lenses")])
	}
	match(t, fallback, "Then `review-refuter` \\(each finding `refuted` or `stands`\\) and `review-validator`", "the fallback runs the refuter, then the validator")
	match(t, fallback, "`jhon review record --subject` with the native path's JSON", "the fallback records as the native path does")
	reduction := support.Read(t, filepath.Join(repoRoot, "prose", "contracts", "review-4r.md"))
	if i := strings.Index(reduction, "## Reduction"); i >= 0 {
		reduction = flat(reduction[i:])
	}
	match(t, reduction, "Deterministic severe findings block", "deterministic severe findings block")
	match(t, reduction, `[Ii]nferential severe findings are listed in\s+review-4r\.md for .verify. to confirm or drop`, "inferential ones wait for verify")
	match(t, reduction, "one bounded correction by a `worker`", "the correction is a worker's")
	match(t, reduction, "`## Allowed edit surfaces`", "the worker is held to its scope")
	match(t, reduction, "`verify` confirms", "verify confirms the correction")
	if strings.Contains(reduction, "jd-fix-agent") {
		t.Error("the 4R reduction names jd-fix-agent, which serves Judgment Day only")
	}
}

// The 4R is native first: one tool call, tool-less lens completions, a
// refuter and a validator, a record bound to the subject; the lens agents
// are only the fallback (T118b, L726, L727).
func TestRenderFourRNativeFirst(t *testing.T) {
	t.Parallel()
	c := flat(support.Read(t, filepath.Join(repoRoot, "prose", "contracts", "review-4r.md")))
	native, fallback := strings.Index(c, "## Native review"), strings.Index(c, "## Fallback: lens agents")
	if native < 0 || fallback < native {
		t.Fatalf("the contract must describe the native review before the lens-agent fallback (native %d, fallback %d)", native, fallback)
	}
	match(t, c, "one tool call \\(`jhon_review` in pi", "one tool call")
	match(t, c, "completions in parallel, then a refuter and a validator", "lenses in parallel, then refuter and validator")
	match(t, c, "`jhon review record` re-freezes and writes review-4r\\.md bound to the frozen subject", "the record is bound to the subject")
	match(t, c, "~100K tokens means the slice is too big: split it", "an oversized lens prompt splits the slice")
	match(t, c, "When the native review cannot run \\(no model or registry, or Claude Code\\), launch the read-only `review-<lens>` agents", "agents are the fallback")
	match(t, c, "Independence stays with `verify`", "independence stays with verify")
	o := flat(support.Read(t, filepath.Join(repoRoot, "prose", "orchestrator.md")))
	match(t, o, "A 4R is one call to the native review, the lens agents only when it cannot run", "the orchestrator calls the native review first")
}
