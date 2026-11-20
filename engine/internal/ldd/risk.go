package ldd

import (
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/discover"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// This file is the risk tier of a change, by fixed rules over its diff
// (plan item 6A); `jhon risk`
// prints it, `jhon context` and `jhon deliver` read it.
//
// The diff is the working tree, untracked files included, against the base:
// a ref the caller names, else the merge base of HEAD with the default
// branch (origin/HEAD, origin/main, origin/master, main, master), else HEAD.
//
// Each changed file gets a level and a reason:
//   - high: a migration or schema file (a .sql file, schema.prisma, any
//     file under a migrations, migration, alembic, flyway, liquibase or
//     db/migrate folder; outside vendor/ and node_modules/, any file under a
//     migrate folder and code, no test, whose base name starts with migrate
//     or migration) unless its text is clearly additive (risk_migration.go),
//     a destructive data statement added
//     (DROP, TRUNCATE, DELETE FROM), a security path (auth, permissions,
//     crypto, secrets, tokens, .env), a contract others consume (OpenAPI,
//     AsyncAPI, proto, GraphQL), delivery or infrastructure (CI, Dockerfile,
//     compose, Terraform, Helm, Kubernetes, deploy/, infra/), and code changed
//     when no test file is touched;
//   - passive: documentation, tests, and the change's own record files
//     (Markdown, JSON, YAML under openspec/changes/**, and the specs it is
//     archived into, openspec/specs/**), which never raise the tier or the
//     route; any other file there gets the rules above and below;
//   - medium: other code (with a test touched), dependency manifests, an
//     additive migration the other rules leave below high, and any other
//     file.
//
// A project's openspec/config.yaml `risk_rules` mapping (`"<glob>": <level>`,
// first match wins) replaces the default level of the files it matches. The
// tier is the highest level; no change is passive. A diff that cannot be
// read is high. Read-only.

// A Level is one of Levels.
type Level string

// The levels, lowest first.
const (
	Passive Level = "passive"
	Medium  Level = "medium"
	High    Level = "high"
)

// Levels are the risk levels in order.
var Levels = []Level{Passive, Medium, High}

// A Reason is the level of one changed file and why; File is "" for the
// change as a whole.
type Reason struct {
	Level Level  `json:"level"`
	File  string `json:"file"`
	Why   string `json:"why"`
}

// Risk is a change's tier, the base its diff was read against, and the reasons.
type Risk struct {
	Tier    Level    `json:"tier"`
	Base    string   `json:"base"`
	Reasons []Reason `json:"reasons"`
	// Facets are the Prism facets the diff calls for (facetsOf).
	Facets []string `json:"-"`
}

// A Changed file is its path, relative to the git top level, and the lines
// the change adds to it.
type Changed struct {
	File  string
	Added string
}

// riskGit runs read-only git in dir; ok is false when it cannot run or fails.
func riskGit(dir string, args ...string) (string, bool) {
	out, err := project.ReadGitCmd(dir, args...).Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

// gitReads is one reading of a repository (a `jhon status`): each read-only
// git it runs once, its answer remembered, so the route, its risk, and the
// slice in progress share one diff. nil remembers nothing. A reading never
// outlives its command, so a later one sees the repository as it is then.
type gitReads map[string]gitRead

type gitRead struct {
	out string
	ok  bool
}

// git is riskGit, remembered in g.
func (g gitReads) git(dir string, args ...string) (string, bool) {
	if g == nil {
		return riskGit(dir, args...)
	}
	key := dir + "\x00" + strings.Join(args, "\x00")
	if r, ok := g[key]; ok {
		return r.out, r.ok
	}
	out, ok := riskGit(dir, args...)
	g[key] = gitRead{out, ok}
	return out, ok
}

// ── rules ───────────────────────────────────────────────────────────────────

// The case-insensitive rules match the path lowered in ASCII only, which is
// JavaScript's i flag without u: Go's (?i) also folds ſ to s and K (Kelvin)
// to k.
var (
	migration   = lazyre.New(`(^|/)(migrations?|alembic|flyway|liquibase)/|(^|/)db/migrate/|(^|/)schema\.prisma$|\.sql$`)
	migrateDir  = lazyre.New(`(^|/)migrate/`)
	migrateName = lazyre.New(`(^|/)migrat(e|ion)[^/]*$`)
	vendored    = lazyre.New(`(^|/)(vendor|node_modules)/`)
	destructive = lazyre.New(`\b(?:drop` + js.Space + `+(?:table|column|database|schema|index)|truncate` + js.Space + `+(?:table` + js.Space + `+)?\w|delete` + js.Space + `+from)\b`)
	contractRe  = lazyre.New(`(^|/)[^/]*(openapi|swagger|asyncapi)[^/]*\.(ya?ml|json)$|\.proto$|\.(graphql|gql)$`)
	delivery    = lazyre.New(`(^|/)dockerfile[^/]*$|(^|/)(docker-)?compose[^/]*\.ya?ml$|(^|/)\.github/workflows/|(^|/)\.gitlab-ci\.ya?ml$|(^|/)jenkinsfile$|(^|/)\.circleci/|(^|/)azure-pipelines[^/]*\.ya?ml$|\.tf$|\.tfvars$|(^|/)(helm|k8s|kubernetes|kustomize|deploy|infra)/`)
	manifest    = lazyre.New(`(^|/)(package\.json|go\.mod|requirements[^/]*\.txt|pyproject\.toml|Pipfile|Cargo\.toml|pom\.xml|build\.gradle(\.kts)?|Gemfile|composer\.json|[^/]*\.csproj)$`)
	docs        = lazyre.New(`\.(md|mdx|txt|rst|adoc)$|(^|/)docs?/|(^|/)(license|changelog|notice)[^/]*$`)
	testRe      = lazyre.New(`(^|/)(tests?|__tests__|specs?)/|\.(test|spec)\.[^/]+$|_test\.[a-z]+$|(^|/)test_[^/]+\.py$`)
	code        = lazyre.New(`\.(ts|tsx|js|jsx|mjs|cjs|go|py|java|kt|kts|rb|rs|cs|php|swift|scala|c|cc|cpp|h|hpp|m|ex|exs|erl|clj|dart|lua|sh|vue|svelte)$`)
	record      = lazyre.New(`^(?:` + layout.DirsPattern() + `)/(changes|specs)/.*\.(md|json|ya?ml)$`)
	dotEnv      = lazyre.New(`(^|/)\.env(\.|$)`)
	wordSep     = lazyre.New(`[/._-]`)
)

var securityWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`auth authn authz authentication authorization security permission permissions rbac acl
		crypto secret secrets token tokens session sessions password passwords oauth jwt iam policy policies credentials`) {
		securityWords[w] = true
	}
}

// asciiLower lowers A-Z only.
func asciiLower(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + 'a' - 'A'
		}
		return r
	}, s)
}

// matchI is a case-insensitive rule's test of s.
func matchI(re interface{ MatchString(string) bool }, s string) bool {
	return re.MatchString(asciiLower(s))
}

// isMigration is a data migration the folder and SQL rules miss: a file
// under a migrate folder at any depth, or code whose base name starts with
// migrate or migration and is no test; vendored code is not the change's.
func isMigration(f string) bool {
	if matchI(vendored, f) {
		return false
	}
	return matchI(migrateDir, f) || matchI(migrateName, f) && code.MatchString(f) && !testRe.MatchString(f)
}

func isSecurityPath(f string) bool {
	if dotEnv.MatchString(f) {
		return true
	}
	return slices.ContainsFunc(wordSep.Split(js.Lower(f), -1), func(w string) bool { return securityWords[w] })
}

// GlobRegex is a glob over a repository path as a regexp: `**` any depth
// (a following `/` included), `*` and `?` within one segment.
func GlobRegex(glob string) *regexp.Regexp {
	glob = strings.ToValidUTF8(glob, "�")
	var re strings.Builder
	for i := 0; i < len(glob); i++ {
		c := glob[i]
		switch {
		case c == '*' && i+1 < len(glob) && glob[i+1] == '*':
			re.WriteString(js.Dot + "*")
			i++
			if i+1 < len(glob) && glob[i+1] == '/' {
				i++
			}
		case c == '*':
			re.WriteString("[^/]*")
		case c == '?':
			re.WriteString("[^/]")
		default:
			re.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	// QuoteMeta of single bytes leaves a multi-byte character whole.
	return regexp.MustCompile("^" + re.String() + "$")
}

type defaulted struct {
	Reason
	code bool
}

// A content is a changed file's whole text; ok is false when it cannot be read.
type content func(Changed) (b []byte, ok bool)

// addedText is a file's content as its added lines.
func addedText(c Changed) ([]byte, bool) { return []byte(c.Added), true }

// defaultLevel is the level and reason of one file by the default rules, and whether it is code.
func defaultLevel(c Changed) defaulted { return defaultLevelOf(c, addedText) }

func defaultLevelOf(c Changed, read content) defaulted {
	f := c.File
	r := func(level Level, why string) defaulted { return defaulted{Reason{level, f, why}, false} }
	// A migration is high wherever it lives, except as a record file, unless
	// its whole text is clearly additive: then the other rules judge it, its
	// down part left out as there, and below high it is medium; a test or a document is passive even under a
	// security path, since only code there changes behavior.
	switch {
	case record.MatchString(f):
		return r(Passive, "change record")
	case matchI(migration, f), isMigration(f):
		if b, ok := read(c); !additiveMigration(f, b, ok) {
			return r(High, "migration or schema")
		}
		d := otherLevel(Changed{f, upPart(c.Added)})
		if d.Level != High {
			d.Level, d.Why = Medium, "migration (additive)"
		}
		return d
	}
	return otherLevel(c)
}

// otherLevel is defaultLevelOf for a file that is no migration or record.
func otherLevel(c Changed) defaulted {
	f := c.File
	r := func(level Level, why string) defaulted { return defaulted{Reason{level, f, why}, false} }
	switch {
	case testRe.MatchString(f):
		return r(Passive, "test")
	case matchI(docs, f) && !matchI(contractRe, f):
		return r(Passive, "documentation")
	case isSecurityPath(f):
		return r(High, "security path")
	case matchI(contractRe, f):
		return r(High, "contract others consume")
	case matchI(delivery, f):
		return r(High, "delivery or infrastructure")
	case matchI(destructive, c.Added):
		return r(High, "destructive data statement")
	case manifest.MatchString(f):
		return r(Medium, "dependency manifest")
	case code.MatchString(f):
		return defaulted{Reason{Medium, f, "code"}, true}
	}
	return r(Medium, "other file")
}

// A Rule is one `risk_rules` entry: a glob and the level of the files it matches.
type Rule struct {
	Glob  string
	Level Level
}

// Classify is the risk of a set of changed files under a project's rules, in
// order; a migration's content is its added lines.
func Classify(changed []Changed, rules []Rule) []Reason {
	return classify(changed, rules, addedText)
}

// classify is Classify, a migration's content read by read.
func classify(changed []Changed, rules []Rule, read content) []Reason {
	testTouched := slices.ContainsFunc(changed, func(c Changed) bool { return testRe.MatchString(c.File) })
	out := make([]Reason, 0, len(changed))
	for _, c := range changed {
		if i := slices.IndexFunc(rules, func(r Rule) bool { return GlobRegex(r.Glob).MatchString(c.File) }); i >= 0 {
			out = append(out, Reason{rules[i].Level, c.File, "project rule " + rules[i].Glob})
			continue
		}
		d := defaultLevelOf(c, read)
		if d.code && !testTouched {
			out = append(out, Reason{High, c.File, "code changed and no test touched"})
			continue
		}
		out = append(out, d.Reason)
	}
	return out
}

func tierOf(reasons []Reason) Level {
	t := Passive
	for _, r := range reasons {
		if slices.Index(Levels, r.Level) > slices.Index(Levels, t) {
			t = r.Level
		}
	}
	return t
}

// ── the diff ────────────────────────────────────────────────────────────────

// baseOf is the base to diff against: the commit and its label; ok is false
// when the asked ref, or HEAD, is not a commit.
func baseOf(top string, asked *string) (commit, label string, ok bool) {
	return gitReads(nil).baseOf(top, asked)
}

func (g gitReads) baseOf(top string, asked *string) (commit, label string, ok bool) {
	if asked != nil {
		c, ok := g.git(top, "rev-parse", "--verify", "--quiet", *asked+"^{commit}")
		if !ok || c == "" {
			return "", "", false
		}
		return js.Trim(c), *asked, true
	}
	head, ok := g.git(top, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if head = js.Trim(head); !ok || head == "" {
		return "", "", false
	}
	for _, ref := range []string{"origin/HEAD", "origin/main", "origin/master", "main", "master"} {
		if _, ok := g.git(top, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); !ok {
			continue
		}
		mb, ok := g.git(top, "merge-base", "HEAD", ref)
		if mb = js.Trim(mb); ok && mb != "" && mb != head {
			return mb, ref, true
		}
	}
	return head, "HEAD", true
}

func nonEmpty(s string) []string {
	return slices.DeleteFunc(strings.Split(s, "\n"), func(l string) bool { return l == "" })
}

// changedFiles is the files changed against base, untracked ones included,
// with their added lines; ok is false when git cannot say.
func (g gitReads) changedFiles(top, base string) ([]Changed, bool) {
	names, ok1 := g.git(top, "diff", "--name-only", "--no-renames", base)
	untracked, ok2 := g.git(top, "ls-files", "--others", "--exclude-standard")
	patch, ok3 := g.git(top, "diff", "-U0", "--no-renames", base)
	if !ok1 || !ok2 || !ok3 {
		return nil, false
	}
	added := map[string]string{}
	cur := ""
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "+++ ") {
			cur = ""
			if line != "+++ /dev/null" && len(line) > 6 {
				cur = line[6:]
			}
			continue
		}
		if cur != "" && strings.HasPrefix(line, "+") {
			added[cur] += line[1:] + "\n"
		}
	}
	out := []Changed{}
	for _, f := range nonEmpty(names) {
		out = append(out, Changed{f, added[f]})
	}
	for _, f := range nonEmpty(untracked) {
		p := filepath.Join(top, f)
		text := ""
		// Unreadable, or larger than 1 MiB: no content rule.
		if st, err := os.Stat(p); err == nil && st.Size() <= 1024*1024 {
			if b, err := os.ReadFile(p); err == nil {
				text = string(b)
			}
		}
		out = append(out, Changed{f, text})
	}
	return out, true
}

// readWhole is a changed file's whole text in the working tree: none (ok
// false) when it is gone, no regular file, larger than 1 MiB or unreadable.
func readWhole(top, f string) ([]byte, bool) {
	p := filepath.Join(top, f)
	st, err := os.Lstat(p)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 1024*1024 {
		return nil, false
	}
	b, err := os.ReadFile(p)
	return b, err == nil
}

// rulesOf is the project's risk_rules whose level is one of Levels.
func rulesOf(top string) ([]Rule, error) {
	if !project.UsesLdd(top) {
		return nil, nil
	}
	b, err := project.ReadLddConfig(top)
	if err != nil {
		return nil, err
	}
	m := discover.ConfigMap(b, "risk_rules")
	var out []Rule
	if m != nil {
		for _, k := range m.Keys {
			if l := Level(m.Values[k]); slices.Contains(Levels, l) {
				out = append(out, Rule{k, l})
			}
		}
	}
	return out, nil
}

// RiskOf is the risk of the change in the repository holding dir, against
// asked (nil: the default branch's merge base). The error is a config that
// exists and cannot be read.
func RiskOf(dir string, asked *string) (Risk, error) {
	return gitReads(nil).riskOf(dir, asked)
}

func (g gitReads) riskOf(dir string, asked *string) (Risk, error) {
	unreadable := func(why string) Risk {
		base := ""
		if asked != nil {
			base = *asked
		}
		reasons := []Reason{{High, "", "the diff cannot be read: " + why}}
		return Risk{Tier: High, Base: base, Reasons: reasons, Facets: facetsOf(nil, reasons)}
	}
	top, _ := g.git(dir, "rev-parse", "--show-toplevel")
	if top = js.Trim(top); top == "" {
		return unreadable("not a git repository"), nil
	}
	commit, label, ok := g.baseOf(top, asked)
	if !ok {
		if asked != nil && *asked != "" {
			return unreadable(*asked + " is not a commit"), nil
		}
		return unreadable("HEAD has no commit"), nil
	}
	changed, ok := g.changedFiles(top, commit)
	if !ok {
		return unreadable("git diff failed"), nil
	}
	reasons := []Reason{{Passive, "", "no change"}}
	if len(changed) > 0 {
		rules, err := rulesOf(top)
		if err != nil {
			return Risk{}, err
		}
		reasons = classify(changed, rules, func(c Changed) ([]byte, bool) { return readWhole(top, c.File) })
	}
	return Risk{Tier: tierOf(reasons), Base: label, Reasons: reasons, Facets: facetsOf(changed, reasons)}, nil
}
