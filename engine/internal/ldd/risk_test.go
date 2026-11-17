package ldd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobRegex(t *testing.T) {
	for _, c := range []struct {
		glob, path string
		want       bool
	}{
		{"src/legacy/**", "src/legacy/a/b.ts", true},
		{"**/*.rs", "a.rs", true},
		{"**/*.rs", "x/y/a.rs", true},
		{"*.md", "docs/a.md", false},
		{"?.md", "a.md", true},
		{"a.b", "axb", false},
		{"a+(b)", "a+(b)", true},
		{"**", "a\nb", false},
	} {
		if got := GlobRegex(c.glob).MatchString(c.path); got != c.want {
			t.Errorf("GlobRegex(%q) on %q = %v", c.glob, c.path, got)
		}
	}
}

func why(rs []Reason) string {
	var out []string
	for _, r := range rs {
		out = append(out, string(r.Level)+" "+r.File+": "+r.Why)
	}
	return strings.Join(out, "\n")
}

func TestClassify(t *testing.T) {
	for _, c := range []struct {
		file, added, want string
	}{
		{"db/migrations/0042.sql", "", "high db/migrations/0042.sql: migration or schema"},
		{"prisma/schema.prisma", "", "high prisma/schema.prisma: migration or schema"},
		{"src/auth/login.ts", "", "high src/auth/login.ts: security path"},
		{"src/İam/x.ts", "", "medium src/İam/x.ts: code"},
		{".env.production", "", "high .env.production: security path"},
		{"api/OpenAPI.yaml", "", "high api/OpenAPI.yaml: contract others consume"},
		{"docs/openapi.json", "", "high docs/openapi.json: contract others consume"},
		{".github/workflows/ci.yml", "", "high .github/workflows/ci.yml: delivery or infrastructure"},
		{"DOCKERFILE.dev", "", "high DOCKERFILE.dev: delivery or infrastructure"},
		{"src/app.ts", "x = 'Delete  From users'\n", "high src/app.ts: destructive data statement"},
		{"src/app.ts", "x = 'DROP TABLEs'\n", "medium src/app.ts: code"},
		{"src/app.ts", "x = 'DROP DATABAſE x'\n", "medium src/app.ts: code"},
		{"package.json", "", "medium package.json: dependency manifest"},
		{"src/authz/tests/test_roles.py", "", "passive src/authz/tests/test_roles.py: test"},
		{"CHANGELOG", "", "passive CHANGELOG: documentation"},
		{"data.bin", "", "medium data.bin: other file"},
	} {
		// A test file touched keeps code at medium.
		got := Classify([]Changed{{c.file, c.added}, {"x_test.go", ""}}, nil)
		if g := why(got[:1]); g != c.want {
			t.Errorf("Classify(%q) = %q, want %q", c.file, g, c.want)
		}
	}
	if got := why(Classify([]Changed{{"src/app.ts", ""}}, nil)); got != "high src/app.ts: code changed and no test touched" {
		t.Errorf("code with no test touched: %q", got)
	}
	rules := []Rule{{"src/legacy/**", Passive}, {"src/**", High}}
	if got := why(Classify([]Changed{{"src/legacy/a.ts", ""}, {"src/b.md", ""}}, rules)); got != "passive src/legacy/a.ts: project rule src/legacy/**\nhigh src/b.md: project rule src/**" {
		t.Errorf("rules, first match wins: %q", got)
	}
}

// gitT runs a test git command in dir, without inherited GIT_* variables.
func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=jhon-test", "-c", "user.email=jhon-test@example.invalid",
		"-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main", "-C", dir}, args...)...)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestRiskOf(t *testing.T) {
	plain, _ := filepath.EvalSymlinks(t.TempDir())
	if r, err := RiskOf(plain, nil); err != nil || r.Tier != High || !strings.Contains(why(r.Reasons), "cannot be read: not a git repository") {
		t.Errorf("outside a repository: %+v, %v", r, err)
	}
	root := plain
	gitT(t, root, "init", "-q")
	write(t, filepath.Join(root, "src/app.ts"), "export const one = 1;\n")
	write(t, filepath.Join(root, "test/app.test.ts"), "x\n")
	gitT(t, root, "add", "-A")
	gitT(t, root, "commit", "-q", "-m", "base")
	gitT(t, root, "checkout", "-q", "-b", "feature")
	if r, _ := RiskOf(root, nil); r.Tier != Passive || r.Base != "HEAD" || why(r.Reasons) != "passive : no change" {
		t.Errorf("no change: %+v", r)
	}
	write(t, filepath.Join(root, "src/app.ts"), "export const one = 2;\n")
	gitT(t, root, "commit", "-q", "-am", "change")
	write(t, filepath.Join(root, "new.sql"), "select 1;\n")
	r, _ := RiskOf(root, nil)
	if r.Tier != High || r.Base != "main" || why(r.Reasons) != "high src/app.ts: code changed and no test touched\nhigh new.sql: migration or schema" {
		t.Errorf("against the merge base with main: %+v", r)
	}
	head := "HEAD"
	if r, _ := RiskOf(root, &head); r.Base != "HEAD" || why(r.Reasons) != "high new.sql: migration or schema" {
		t.Errorf("--base HEAD sees only the working tree: %+v", r)
	}
	for asked, msg := range map[string]string{"nope": "nope is not a commit", "": "HEAD has no commit"} {
		if r, _ := RiskOf(root, &asked); r.Tier != High || r.Base != asked || r.Reasons[0].Why != "the diff cannot be read: "+msg {
			t.Errorf("--base %q: %+v", asked, r)
		}
	}
	write(t, filepath.Join(root, "openspec/config.yaml"), "risk_rules:\n  \"*.sql\": passive\n  \"src/**\": bogus\n")
	if r, _ := RiskOf(root, &head); why(r.Reasons) != "passive new.sql: project rule *.sql\nmedium openspec/config.yaml: other file" {
		t.Errorf("a rule makes the file passive, an unknown level is ignored: %+v", r)
	}
}
