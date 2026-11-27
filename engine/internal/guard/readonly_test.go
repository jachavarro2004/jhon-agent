package guard

import (
	"strings"
	"testing"
)

// ReadOnly reads a line by what it runs (T271): every construct and program
// on the closed list passes, anything else is refused with a fix.
func TestReadOnly(t *testing.T) {
	inEnv := func(n string) bool { return n == "present" || n == "PATH" || n == "HOME" }
	forms := [][]string{{"codegraph", "callers"}}
	for _, c := range []struct {
		line string
		pass bool
	}{
		{"git log --oneline | head -5 && git status --short", true},
		{`git grep -n "a\|b" HEAD -- 'src/*.go'`, true},
		{"for f in a b; do wc -l \"$f\"; done 2>/dev/null", true},
		{"git diff --output-indicator-new=+ HEAD", true},
		{"git diff --no-ext-diff --no-textconv HEAD", true},
		{"codegraph callers x | head", true},
		{"find . -name '*.go' -exec grep -l x {} +", true},
		{"x=1; echo \"$x\"", true},
		{"git branch -a --contains HEAD", true},
		{"git config --get-regexp '^user'", true},
		{"git remote -v", true},
		{"git remote get-url origin", true},
		{"git diff --out=f", false},
		{"git diff --ext-diff", false},
		{"codegraph init", false},
		{"present=1; ls", false},
		{"PATH=/x ls", false},
		{"git remote add o u", false},
		{"git config --unset user.name", false},
		{"git branch -D x", false},
		{"tee a", false},
		{"sort -T /tmp a", false},
		{"date -s now", false},
		{"xxd -r a", false},
		{"find . -exec wc {} + -delete", false},
		{"ls > a", false},
		{"ls >&a", false},
		{"ls <>a", false},
		{"echo ${x:=1}", false},
		{"rg $x", false},
		{"rg -- $x", true},
	} {
		why := ReadOnly(c.line, inEnv, forms)
		if (why == "") != c.pass {
			t.Errorf("%q: read %q, want pass %v", c.line, why, c.pass)
		}
		if why != "" && !strings.Contains(why, ". Instead: ") {
			t.Errorf("%q: the refusal names no fix: %q", c.line, why)
		}
	}
}

// A selection glob is credentials by its plainest instance, read with no
// file (T271).
func TestNamesCredentials(t *testing.T) {
	for glob, want := range map[string]bool{
		".env*": true, "**/.env": true, "*.pem": true, "id_rsa*": true, "*.env": true, "id_[r]sa": true, "credentials*": true,
		// word filling .env.example* gives .env.exampleword, no template: it counts, and its walk finds only templates.
		".env.example*": true,
		"*.json":        false, "**/*.json": false, "cred*": false, "id_*": false, "*.json*": false, "[": false,
	} {
		if got := namesCredentials(glob); got != want {
			t.Errorf("%s: %v, want %v", glob, got, want)
		}
	}
}

// Exclusions, templates and recursion name no credentials file (T271).
func TestReaderOperands(t *testing.T) {
	files, sel := readerOperands("rg", []string{"KEY", "-g", "!*.env", "--glob=id_*", "-t", "py", "src"}, contentReaders["rg"])
	if strings.Join(files, ",") != "src" || strings.Join(sel, ",") != "id_*" {
		t.Errorf("rg: files %q, selectors %q", files, sel)
	}
	files, sel = readerOperands("grep", []string{"-rn", "-e", "KEY", "--exclude=.env", "--include", "cred*", "a", "b"}, contentReaders["grep"])
	if strings.Join(files, ",") != "a,b" || strings.Join(sel, ",") != "cred*" {
		t.Errorf("grep: files %q, selectors %q", files, sel)
	}
	if files, _ := readerOperands("rg", []string{"--files", ".env"}, contentReaders["rg"]); files != nil {
		t.Errorf("rg --files reads %q", files)
	}
	for spec, want := range map[string]bool{":!x": false, ":^x": false, ":(exclude)x": false, ":(glob)**/x": true, "x": true} {
		if _, ok := pathspec(spec); ok != want {
			t.Errorf("pathspec %q: %v", spec, ok)
		}
	}
	if got := braces("a{b,c{d,e}}f"); strings.Join(got, " ") != "abf acdf acef" {
		t.Errorf("braces: %q", got)
	}
}
