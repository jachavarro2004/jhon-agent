// `jhon risk` (plan item 6A): the risk tier of a change, by fixed rules over
// its diff. passive: no change, or only docs and tests. high: a migration or
// destructive SQL, a security path, a contract others consume, delivery or
// infrastructure, code with no test touched, or a diff that cannot be read.
// medium: everything else. A project's openspec/config.yaml `risk_rules`
// (glob: level) overrides the default for the files it matches.
//
// The diff is the working tree, untracked files included, against the base:
// `--base <ref>`, else the merge base with the default branch, else HEAD.
package ldd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// riskRepo is a repository whose main holds src/app.ts and
// test/app.test.ts, checked out on branch `feature`.
func riskRepo(t *testing.T) string {
	t.Helper()
	root := support.GitRepo(t, temp(t))
	support.Write(t, root, map[string]string{
		"src/app.ts":       "export const one = 1;\n",
		"test/app.test.ts": "import { one } from '../src/app';\n",
		"README.md":        "# app\n",
	})
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "base")
	support.Git(t, root, "checkout", "-q", "-b", "feature")
	return root
}

func put(t *testing.T, root, rel, text string) {
	t.Helper()
	support.Write(t, root, map[string]string{rel: text})
}

type riskReport struct {
	Tier    string `json:"tier"`
	Base    string `json:"base"`
	Reasons []struct {
		Level string `json:"level"`
		File  string `json:"file"`
		Why   string `json:"why"`
	} `json:"reasons"`
}

func risk(t *testing.T, root string, args ...string) riskReport {
	t.Helper()
	r := jhonGit(t, root, append([]string{"risk", "--json"}, args...)...)
	if r.Code != 0 {
		t.Fatalf("jhon risk: exit %d\n%s", r.Code, r.Out())
	}
	var out riskReport
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		t.Fatalf("%v\n%s", err, r.Stdout)
	}
	return out
}

func why(r riskReport) string {
	var lines []string
	for _, x := range r.Reasons {
		lines = append(lines, x.Level+" "+x.File+": "+x.Why)
	}
	return strings.Join(lines, "\n")
}

func TestRiskPassive(t *testing.T) {
	t.Parallel()
	root := riskRepo(t)
	equal(t, risk(t, root).Tier, "passive")
	put(t, root, "README.md", "# app\n\nmore\n")
	put(t, root, "docs/guide.md", "guide\n")
	equal(t, risk(t, root).Tier, "passive", why(risk(t, root)))
	put(t, root, "test/app.test.ts", "import { one } from '../src/app';\n// more\n")
	equal(t, risk(t, root).Tier, "passive")
}

func TestRiskMedium(t *testing.T) {
	t.Parallel()
	root := riskRepo(t)
	put(t, root, "src/app.ts", "export const one = 2;\n")
	put(t, root, "test/app.test.ts", "import { one } from '../src/app';\n// one is 2\n")
	equal(t, risk(t, root).Tier, "medium", why(risk(t, root)))
	other := riskRepo(t)
	put(t, other, "package.json", "{\"dependencies\":{\"x\":\"1\"}}\n")
	r := risk(t, other)
	equal(t, r.Tier, "medium", why(r))
	match(t, why(r), `package\.json: dependency manifest`)
}

func TestRiskCodeWithoutTest(t *testing.T) {
	t.Parallel()
	root := riskRepo(t)
	put(t, root, "src/app.ts", "export const one = 2;\n")
	r := risk(t, root)
	equal(t, r.Tier, "high")
	match(t, why(r), `src/app\.ts: code changed and no test touched`)
}

func TestRiskHighPaths(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ file, text, re string }{
		{"db/migrations/0042_drop_col.sql", "ALTER TABLE users DROP COLUMN email;\n", `migration`},
		{"prisma/schema.prisma", "model User { id Int @id }\n", `migration`},
		{"src/auth/login.ts", "export const login = () => true;\n", `security path`},
		{".env.production", "KEY=x\n", `security path`},
		{"api/openapi.yaml", "openapi: 3.0.0\n", `contract`},
		{"proto/user.proto", "syntax = \"proto3\";\n", `contract`},
		{".github/workflows/ci.yml", "on: push\n", `delivery`},
		{"Dockerfile", "FROM node:22\n", `delivery`},
		{"infra/main.tf", "resource \"x\" \"y\" {}\n", `delivery`},
	} {
		t.Run(c.file+" is high", func(t *testing.T) {
			t.Parallel()
			root := riskRepo(t)
			put(t, root, c.file, c.text)
			put(t, root, "test/app.test.ts", "import { one } from '../src/app';\n// touched\n")
			r := risk(t, root)
			equal(t, r.Tier, "high", why(r))
			match(t, why(r), c.re)
		})
	}
}

func TestRiskDestructiveSQL(t *testing.T) {
	t.Parallel()
	root := riskRepo(t)
	put(t, root, "src/app.ts", "export const purge = 'DELETE FROM users';\n")
	put(t, root, "test/app.test.ts", "import { one } from '../src/app';\n// touched\n")
	r := risk(t, root)
	equal(t, r.Tier, "high")
	match(t, why(r), `src/app\.ts: destructive data statement`)
}

func TestRiskCommittedBranchChanges(t *testing.T) {
	t.Parallel()
	root := riskRepo(t)
	put(t, root, "src/app.ts", "export const one = 2;\n")
	support.Git(t, root, "commit", "-q", "-am", "change")
	r := risk(t, root)
	equal(t, r.Tier, "high", why(r))
	equal(t, r.Base, "main")
	equal(t, risk(t, root, "--base", "HEAD").Tier, "passive", "--base HEAD sees only the working tree")
}

func TestRiskProjectRules(t *testing.T) {
	t.Parallel()
	root := riskRepo(t)
	put(t, root, "openspec/config.yaml", "risk_rules:\n  \"src/legacy/**\": passive\n  \"docs/**\": high\n")
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "config")
	support.Git(t, root, "branch", "-f", "main", "HEAD")
	put(t, root, "src/legacy/old.ts", "export const x = 1;\n")
	equal(t, risk(t, root).Tier, "passive", why(risk(t, root)))
	put(t, root, "docs/guide.md", "guide\n")
	r := risk(t, root)
	equal(t, r.Tier, "high", why(r))
	match(t, why(r), `docs/guide\.md: project rule docs/\*\*`)
}

func TestRiskNotARepository(t *testing.T) {
	t.Parallel()
	r := risk(t, temp(t))
	equal(t, r.Tier, "high")
	match(t, why(r), `cannot be read`)
}

func TestRiskTextReport(t *testing.T) {
	t.Parallel()
	root := riskRepo(t)
	put(t, root, "src/app.ts", "export const one = 2;\n")
	r := jhonGit(t, root, "risk")
	equal(t, r.Code, 0, r.Stderr)
	match(t, r.Stdout, `(?m)^risk: high \(base HEAD\)$`)
	match(t, r.Stdout, `(?m)^  high +src/app\.ts: code changed and no test touched$`)
}

func TestRiskTestsAndDocsUnderSecurityPath(t *testing.T) {
	t.Parallel()
	root := riskRepo(t)
	put(t, root, "src/authz/tests/test_roles.py", "def test_roles(): pass\n")
	put(t, root, "src/authz/docs/permissions.md", "# permissions\n")
	r := risk(t, root)
	equal(t, r.Tier, "passive", why(r))
	put(t, root, "src/authz/roles.py", "ROLES = []\n")
	match(t, why(risk(t, root)), `(?m)^high src/authz/roles\.py: security path$`)
}

// A change's own record is passive (S13): it never lifts a docs-only change,
// and it never lowers the tier code gives one.
func TestRiskChangeRecordIsPassive(t *testing.T) {
	t.Parallel()
	root := riskRepo(t)
	put(t, root, "openspec/changes/c1/execution-record.json", "{\"runs\":[]}\n")
	put(t, root, "openspec/changes/c1/tasks.md", "- [ ] one\n")
	put(t, root, "docs/guide.md", "guide\n")
	r := risk(t, root)
	equal(t, r.Tier, "passive", why(r))
	match(t, why(r), `(?m)^passive openspec/changes/c1/execution-record\.json: change record$`)
	put(t, root, "src/app.ts", "export const one = 2;\n")
	r = risk(t, root)
	equal(t, r.Tier, "high", why(r))
	match(t, why(r), `(?m)^high src/app\.ts: code changed and no test touched$`)
}

// Only the record's own files (Markdown, JSON, YAML) are passive in a change
// folder (S13, L31 W4): a .env, a destructive migration, or code there gets
// the default rules.
func TestRiskChangeRecordFilesOnly(t *testing.T) {
	t.Parallel()
	root := riskRepo(t)
	put(t, root, "openspec/changes/c1/execution-record.json", "{\"runs\":[]}\n")
	put(t, root, "openspec/changes/c1/tasks.md", "- [ ] one\n")
	put(t, root, "openspec/changes/c1/delivery.json", "{}\n")
	r := risk(t, root)
	equal(t, r.Tier, "passive", why(r))
	match(t, why(r), `(?m)^passive openspec/changes/c1/delivery\.json: change record$`)

	env := riskRepo(t)
	put(t, env, "openspec/changes/c1/.env", "TOKEN=x\n")
	r = risk(t, env)
	equal(t, r.Tier, "high", why(r))
	match(t, why(r), `(?m)^high openspec/changes/c1/\.env: security path$`)

	sql := riskRepo(t)
	put(t, sql, "openspec/changes/c1/001_drop.sql", "DROP TABLE users;\n")
	r = risk(t, sql)
	equal(t, r.Tier, "high", why(r))
	match(t, why(r), `(?m)^high openspec/changes/c1/001_drop\.sql: migration or schema$`)

	code := riskRepo(t)
	put(t, code, "openspec/changes/c1/tool.go", "package c1\n")
	put(t, code, "test/app.test.ts", "import { one } from '../src/app';\n// touched\n")
	r = risk(t, code)
	equal(t, r.Tier, "medium", why(r))
	match(t, why(r), `(?m)^medium openspec/changes/c1/tool\.go: code$`)
}
