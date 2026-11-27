// T271 rule 2, credentials by real reach (S10): a word counts as
// credentials only when it reaches a credentials file, a path that is one or
// a glob that matches one there. A reader program's operand that does is
// refused to every agent in every mode, with the fix (the template, or the
// owner runs it). A pathspec, glob or option that names a file to leave it
// out never counts, nor does a search that merely recurses into a folder, a
// template, or a program the guard does not read.

package guards

import (
	"strconv"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// credFiles is a project holding credentials files beside ordinary ones.
func credFiles(t *testing.T) sgEnv {
	t.Helper()
	s := newSG(t)
	support.Write(t, s.p, map[string]string{".env": "KEY=1\n", ".env.example": "KEY=\n", "conf/id_ed25519": "k\n",
		"conf/credentials.json": "{}\n", "src/a.go": "package a\n", "src/b.json": "{}\n"})
	return s
}

func TestT271CredentialContentsRefusedInBash(t *testing.T) {
	t.Parallel()
	s := credFiles(t)
	for _, command := range []string{
		"cat .env", "grep -n KEY .env", "head -1 conf/*", "tail -n 2 .env*", "jq . conf/credentials.json",
		"git show HEAD:.env", "git diff HEAD -- .env", "rg KEY -g 'id_ed25519*' .", "grep -rn KEY --include='credentials*' .",
		"find . -name 'id_ed25519*' -exec cat {} +", "rg KEY -g '.env*' .", "git grep KEY -- '**/.env'", "echo .env | xargs cat", "cd conf && cat id_ed25519",
	} {
		t.Run(strconv.Quote(command), func(t *testing.T) {
			t.Parallel()
			for _, o := range []sgOpts{top(), {}} {
				r := s.bash(t, command, o)
				if r.Code != 2 || !strings.Contains(r.Stderr, "credentials") || !strings.Contains(r.Stderr, ".env.example") {
					t.Errorf("%s (top %v): exit %d, want the credentials refusal with its fix: %s%s", command, o.top, r.Code, r.Stdout, r.Stderr)
				}
			}
		})
	}
}

func TestT271CredentialExclusionsAndTemplatesPass(t *testing.T) {
	t.Parallel()
	s := credFiles(t)
	for _, command := range []string{
		"cat .env.example", "grep -n KEY .env.example", "cat src/*", "rg KEY -g '!.env*' -g '!conf/**' .", "rg KEY --glob='!id_*' src",
		"grep -rn KEY --exclude='.env*' --exclude-dir=conf .", "git diff HEAD -- . ':!.env' ':(exclude)conf' ':^*.json'",
		"find . -not -name 'id_*' -name '*.go' -exec cat {} +", "find . ! -name '.env*' -name '*.go' -exec cat {} +",
		"grep -rn KEY .", "rg KEY", "ls -la conf", "wc -c .env", "stat .env", "rg -n KEY -g '*.go' src",
		"git grep KEY -- ':!*.env'", "grep -r --exclude=.env KEY .", "find . -not -name '.env'", "python3 x.py .env",
	} {
		t.Run(strconv.Quote(command), func(t *testing.T) {
			t.Parallel()
			r := s.bash(t, command, top())
			if r.Code != 0 {
				t.Errorf("%s: exit %d, want it to pass: %s", command, r.Code, r.Stderr)
			}
		})
	}
}

// A selection glob (rg -g, grep --include, find -name feeding a reader, a
// git pathspec) counts only when its plainest instance is a credentials
// file by name, decided with no walk: `*.json` is `word.json`, a recursion
// narrowed to ordinary files, whatever the tree holds. A glob that counts
// is refused only when such a file exists under its root, tool data aside;
// a template is none.
func TestT271SelectionGlobsCountByName(t *testing.T) {
	t.Parallel()
	s := credFiles(t)
	for _, command := range []string{"rg KEY -g '*.json' .", "grep -rn KEY --include='*.json' .", "find . -name '*.json' -exec cat {} +",
		"git grep KEY -- '*.json'", "rg KEY -g 'id_*' .", "rg KEY -g '*.{go,json}' src conf"} {
		code(t, s.bash(t, command, top()), 0, command)
	}
	for _, command := range []string{"rg KEY -g '{*.go,id_ed25519}' conf", "rg KEY -g '.env*' .", "find . -name 'credentials*' -exec cat {} +"} {
		code(t, s.bash(t, command, top()), 2, command)
	}
	tmpl := newSG(t)
	support.Write(t, tmpl.p, map[string]string{".env.example": "KEY=\n", "svc/.env.sample": "KEY=\n", ".codegraph/x/.env": "KEY=1\n"})
	for _, command := range []string{"rg KEY -g '.env*' .", "grep -rn KEY --include='.env*' ."} {
		code(t, tmpl.bash(t, command, top()), 0, command+" (templates and tool data only)")
	}
	// The Grep tool keeps its walk (T87b): an extension glob that reaches a credentials file is refused.
	grep := map[string]any{"tool_name": "Grep", "tool_input": map[string]any{"pattern": "KEY", "path": s.p, "glob": "*.json"}, "cwd": s.p, "agent_id": "a1", "agent_type": "worker"}
	code(t, hook(t, s.env, "secrets-guard", []string{"--agent", "*"}, grep), 2, "Grep glob *.json beside credentials.json")
}

// The file tools read a search's exclusion as Bash does: `!glob` leaves the
// files out. Naming one to read stays refused.
func TestT271GrepExclusionIsNoCredentialsRead(t *testing.T) {
	t.Parallel()
	s := credFiles(t)
	grep := func(glob string) int {
		payload := map[string]any{"tool_name": "Grep", "tool_input": map[string]any{"pattern": "KEY", "path": s.p, "glob": glob}, "cwd": s.p, "agent_id": "a1", "agent_type": "worker"}
		return hook(t, s.env, "secrets-guard", []string{"--agent", "*"}, payload).Code
	}
	for _, g := range []string{"!**/.env*", "!.env*", "!**/id_*"} {
		if got := grep(g); got != 0 {
			t.Errorf("Grep glob %s: exit %d, want 0", g, got)
		}
	}
	for _, g := range []string{".env*", "**/id_*"} {
		if got := grep(g); got != 2 {
			t.Errorf("Grep glob %s: exit %d, want 2", g, got)
		}
	}
	read := map[string]any{"tool_name": "Read", "tool_input": map[string]any{"file_path": s.p + "/.env"}, "cwd": s.p, "agent_id": "a1", "agent_type": "worker"}
	code(t, hook(t, s.env, "secrets-guard", []string{"--agent", "*"}, read), 2, "Read .env")
}

// A write or a deletion of a credentials file stays refused (S10), and a
// mode that passes asks passes no credentials read.
func TestT271CredentialWritesAndModesStayRefused(t *testing.T) {
	t.Parallel()
	s := credFiles(t)
	for _, command := range []string{"rm conf/id_ed25519", "echo x > .env", "cp src/a.go conf/id_ed25519"} {
		code(t, s.bash(t, command, top()), 2, command)
	}
	writeGrant(t, s.grants, "destroy", s.p, 30, nil)
	code(t, s.bash(t, "cat .env", top()), 2, "cat .env with a live grant")
}
