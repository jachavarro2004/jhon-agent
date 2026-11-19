// A glob whose name could match a credentials file is refused (today's
// rule), save on an allow-list (T28, L217-L223, L242-L243): one simple
// command of a read-only program whose glob operands the shell expands, run
// in the project, every glob relative, and none of them reaching a real
// credentials file (a name CredentialFile reads, never a template such as
// credentials.example.json). Anything the guard cannot prove keeps today's
// rule. The cases every verifier found on feat/guard-glob (L230, L233, L242)
// are refused here with real fixtures.

package guards

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// credTree is a project with JSON files and no credentials file.
func credTree(t *testing.T) sgEnv {
	t.Helper()
	s := newSG(t)
	support.Write(t, s.p, map[string]string{"i18n/en.json": "{}\n", "i18n/es/app.json": "{}\n", "snapshots/a.json": "{}\n"})
	return s
}

func TestCredentialGlobPassesWithNoCredentialsFile(t *testing.T) {
	t.Parallel()
	s := credTree(t)
	for _, c := range []string{"cat i18n/**.json", "cat snapshots/*.json", "jq . snapshots/*.json", "jq . **/*.json", "head -n 2 i18n/*/*.json",
		"tail -n 1 snapshots/*.json", "wc -l i18n/**.json", "cat *.pem", "cat config/*.key", "cat secrets/*", "yq -o json . snapshots/*.json"} {
		s.code(t, c, sgOpts{}, 0, "an allowed reader whose glob expands to no credentials file")
	}
}

func TestCredentialGlobRefusedWhenItExpandsToOne(t *testing.T) {
	t.Parallel()
	s := credTree(t)
	support.Write(t, s.p, map[string]string{"credentials.json": "{}\n", "deep/sub/id_rsa": "k\n", "certs/server.pem": "k\n"})
	for _, c := range []string{"cat **/*.json", "cat *.json", "jq . ./*.json", "cat deep/**/id_*", "cat */*.pem", "cat id_*", "head deep/*/id_*"} {
		s.code(t, c, sgOpts{}, 2, "a glob that expands to a credentials file")
	}
	s.code(t, "cat i18n/**.json", sgOpts{}, 0, "a glob whose expansion holds no credentials file")
}

// The owner's commands are refused as soon as a real credential sits
// anywhere their glob reaches.
func TestCredentialGlobOwnersCasesRefusedBesideACredential(t *testing.T) {
	t.Parallel()
	s := credTree(t)
	support.Write(t, s.p, map[string]string{"i18n/deep/x/credentials.json": "{}\n", "snapshots/credentials.json": "{}\n"})
	for _, c := range []string{"cat i18n/**.json", "cat snapshots/*.json", "jq . snapshots/*.json", "wc -c i18n/**.json"} {
		s.code(t, c, sgOpts{}, 2, "an allowed command whose glob reaches a credentials file")
	}
}

func TestCredentialGlobTemplatesStayReadable(t *testing.T) {
	t.Parallel()
	s := credTree(t)
	support.Write(t, s.p, map[string]string{"snapshots/credentials.example.json": "{}\n", "keys/id_rsa.sample": "x\n", ".env.example": "X=\n", "api/.env.local.sample": "X=\n"})
	for _, c := range []string{"cat snapshots/*.json", "jq . snapshots/*.json", "cat keys/id_*", "cat .env*", "cat api/.env*"} {
		s.code(t, c, sgOpts{}, 0, "only templates")
	}
	support.Write(t, s.p, map[string]string{".env": "X=1\n", "keys/id_rsa": "k\n"})
	s.code(t, "cat .env*", sgOpts{}, 2, "a real dotenv file beside the template")
	s.code(t, "cat keys/id_*", sgOpts{}, 2, "a real key beside the template")
	// A literal path keeps its rules: the template passes, the file does not.
	s.code(t, "cat .env.example", sgOpts{}, 0, "a literal template")
	s.code(t, "cat .env", sgOpts{}, 2, "a literal credentials file")
	s.code(t, "grep KEY .env*", sgOpts{}, 2, "a program outside the allow-list")
}

func TestCredentialGlobFailsClosed(t *testing.T) {
	t.Parallel()
	s := credTree(t)
	// Outside the project the glob is read by its name: `*.json` can match credentials.json.
	// T87b (L631): an extension glob outside the project is walked; it is
	// refused once the walk meets a credentials file. A glob under home is the sandbox's (T128f).
	s.code(t, s.expand("cat <SCRATCH>/*.json"), sgOpts{}, 0, "a glob outside the project, no credentials file")
	s.code(t, "cat ../*.json", sgOpts{}, 0, "a glob that climbs out of the project, no credentials file")
	s.code(t, "cat proj/*.json", sgOpts{cwd: s.scratch}, 0, "a glob run outside any project, no credentials file")
	support.Write(t, s.other, map[string]string{"credentials.json": "{}\n"})
	s.code(t, s.expand("cat <SCRATCH>/*.json"), sgOpts{}, 2, "a glob outside the project beside a credentials file")
	s.code(t, "cat ../*.json", sgOpts{}, 2, "a glob that climbs out of the project beside a credentials file")
	s.code(t, "cat other/*.json", sgOpts{cwd: s.scratch}, 2, "a glob run outside any project beside a credentials file")
	// A linked directory is not followed: it may lead anywhere.
	if err := os.Symlink(s.other, filepath.Join(s.p, "linked")); err != nil {
		t.Fatal(err)
	}
	s.code(t, "cat linked/*.json", sgOpts{}, 2, "a glob through a linked directory")
	s.code(t, "cat **/*.json", sgOpts{}, 2, "a recursive glob that meets a linked directory")
	// An unreadable directory on the way.
	locked := filepath.Join(s.p, "locked")
	support.Write(t, locked, map[string]string{"a.json": "{}\n"})
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if _, err := os.ReadDir(locked); err == nil {
		t.Skip("the directory stays readable (running as root)")
	}
	s.code(t, "cat locked/*.json", sgOpts{}, 2, "a glob into an unreadable directory")
}

// A linked file is read by its target's name too.
func TestCredentialGlobALinkedFileNamesItsTarget(t *testing.T) {
	t.Parallel()
	s := credTree(t)
	support.Write(t, s.other, map[string]string{"id_rsa": "k\n"})
	if err := os.Symlink(filepath.Join(s.other, "id_rsa"), filepath.Join(s.p, "snapshots", "b.json")); err != nil {
		t.Fatal(err)
	}
	s.code(t, "cat snapshots/*.json", sgOpts{}, 2, "a linked file whose target is a credentials file")
}

func TestCredentialRefusalNamesItsClass(t *testing.T) {
	t.Parallel()
	s := credTree(t)
	support.Write(t, s.p, map[string]string{"cert.pem": "k\n"})
	for _, c := range []string{"cat .env", "cat *.pem"} {
		r := s.bash(t, c, sgOpts{})
		code(t, r, 2, c)
		matches(t, r.Stderr, "^safety-guard: refused: ", c)
		matches(t, r.Stderr, "Credentials are the owner's alone, and never handed to an agent\\. Stop and report this refusal verbatim\\.", c)
		if regexp.MustCompile(`destructive or irreversible`).MatchString(r.Stderr) {
			t.Errorf("%s: a credentials refusal says destructive: %s", c, r.Stderr)
		}
	}
	r := s.bash(t, "cat *.pem", sgOpts{})
	matches(t, r.Stderr, "`\\*\\.pem` matches a credentials file, which the owner handles", "a glob is named as a pattern")
	if regexp.MustCompile(`\*\.pem holds credentials`).MatchString(r.Stderr) {
		t.Errorf("a glob refusal calls the glob a file: %s", r.Stderr)
	}
	r = s.bash(t, "cat .env; git push --force", sgOpts{})
	code(t, r, 2, "both")
	matches(t, r.Stderr, "destructive or irreversible", "a destructive part keeps its words")
	matches(t, r.Stderr, "Credentials are the owner's alone", "and the credentials part its own")
	matches(t, r.Stderr, "the owner runs it with `!` in Claude Code, or in their own terminal in pi", "the runtime-neutral tail")
}

// A glob that matches a credentials file only by its extension (`*.json`
// matches credentials.json and data.json alike) is refused only where it can
// reach one: a home credentials directory, or a walk of its directory that
// meets a credentials file or cannot finish (T87b, L630-L631). A glob that
// names a credentials file by name (`.env*`, `*.pem`, `id_*`, `cred*`) keeps
// today's rule.
var extensionGlobLines = []string{"rg -n x .github/*.json", "rg -ln x .github/*.json | head", "grep -r x --include=*.json", "grep -r x --include='*.json' .",
	"ls *.json", "ls .github/*.json", "cat data/*.json", "cat *.json | head", "cd data && cat *.json", "rg -g '*.json' x", "find . -name '*.json' | head",
	"git add -- '*.json'", "grep -l x data/*/*.json", "cat <SCRATCH>/*.json"}

func extTree(t *testing.T) sgEnv {
	t.Helper()
	s := newSG(t)
	support.Write(t, s.p, map[string]string{".github/a.json": "{}\n", "data/contacts.json": "[]\n", "data/x/b.json": "{}\n", "a.json": "{}\n"})
	return s
}

func TestExtensionGlobPassesWhereNoCredentialIsReachable(t *testing.T) {
	t.Parallel()
	s := extTree(t)
	for _, c := range extensionGlobLines {
		s.code(t, s.expand(c), sgOpts{}, 0, "an extension glob that reaches no credentials file")
		s.code(t, s.expand(c), top(), 0, "the same at the top level")
	}
}

func TestExtensionGlobRefusedWhereACredentialIsReachable(t *testing.T) {
	t.Parallel()
	s := extTree(t)
	support.Write(t, s.p, map[string]string{".github/credentials.json": "{}\n", "data/x/credentials.json": "{}\n", "credentials.json": "{}\n"})
	support.Write(t, s.scratch, map[string]string{"credentials.json": "{}\n"})
	for _, c := range extensionGlobLines {
		if strings.HasPrefix(c, "ls ") {
			continue // ls names files and reads none: it passes beside a credentials file, as before
		}
		r := s.bash(t, s.expand(c), sgOpts{})
		code(t, r, 2, c)
		matches(t, r.Stderr, "matches a credentials file, which the owner handles", c)
	}
}

func TestExtensionGlobUnresolvableOrHomeStaysRefused(t *testing.T) {
	t.Parallel()
	s := extTree(t)
	support.Write(t, s.p, map[string]string{"dirs.txt": "x\n"})
	for _, c := range []string{"cat $PWD/*.json", "cat :*.json", "cat @*.json", "git add ':(glob)**/*.json'", "cat {a,b}/*.json", "rg -g '*.json' x <OTHER> /srv",
		"grep -r --include=*.json x ~", "xargs -a dirs.txt grep -r --include=*.json k", "cat dirs.txt | xargs grep -r --include=*.json k",
		"git -C <OTHER> ls-files '*.json'", "find -L . -name '*.json'", "tar -T dirs.txt --wildcards '*.json' -cf -", "rg -g '*.json' x $HOME",
		"find . -name '*.json' -exec cat {} +", "cp a.json b.json; cat *.json", "yq -i . *.json", "node x.mjs; cat *.json", "ln -s <OTHER> l; cat l/*.json"} {
		s.code(t, s.expand(c), sgOpts{}, 2, "an extension glob the guard cannot resolve, or a home credentials directory")
	}
	// Name globs keep today's rule with no credentials file anywhere.
	for _, c := range []string{"cat .env* | head", "rg -g 'cred*' x", "grep -r --include='id_*' x .", "cat *.pem | head", "cat .env"} {
		s.code(t, c, sgOpts{}, 2, "a glob that names a credentials file")
	}
}
