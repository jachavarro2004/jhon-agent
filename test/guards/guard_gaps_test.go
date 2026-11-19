// Guard gaps the T15.20 verifiers found on the base (T21 G2, T28): a
// sourced script, the Glob tool, Grep's `~`. A script's text is judged as a
// command line is; the probe is a destructive line, since reading a
// credentials file is not limited (L1366).

package guards

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// A sourced script runs in the shell itself: its text is judged as `bash
// <script>` judges it.
func TestSourcedScriptIsJudged(t *testing.T) {
	t.Parallel()
	s := credTree(t)
	support.Write(t, s.p, map[string]string{"deploy/far.sh": "git push --force\n", "deploy/ok.sh": "echo hi\n"})
	for _, c := range []string{"bash deploy/far.sh", "source deploy/far.sh", ". deploy/far.sh", "source ./deploy/far.sh"} {
		s.code(t, c, sgOpts{}, 2, "a script that runs a destructive command")
	}
	s.code(t, "source deploy/ok.sh", sgOpts{}, 0, "a harmless sourced script")
}

// The Glob tool is guarded as Grep's glob is (Claude Code's Glob, pi's find).
func TestGlobToolIsGuarded(t *testing.T) {
	t.Parallel()
	p := realTemp(t)
	support.GitInit(t, p)
	support.Write(t, p, map[string]string{"i18n/en.json": "{}\n", "keys/id_rsa.sample": "x\n"})
	for _, in := range []map[string]any{{"pattern": "**/*.json"}, {"pattern": "i18n/*.json"}, {"pattern": "keys/id_*"}, {"pattern": "**/*.go"}, {"pattern": "*.json", "path": "i18n"}} {
		if got := toolCall(t, p, "Glob", in); got != 0 {
			t.Errorf("Glob %v with no credentials file: exit %d, want 0", in, got)
		}
	}
	for _, in := range []map[string]any{{"pattern": "/srv/**/id_*"}, {"pattern": "id_*", "path": "~/keys"}, {"pattern": "*.json", "path": "~"},
		{"pattern": "{a,b}/*.json"}, {"pattern": "**/id_*", "path": "~"}} {
		if got := toolCall(t, p, "Glob", in); got != 2 {
			t.Errorf("Glob %v outside the project: exit %d, want 2", in, got)
		}
	}
	support.Write(t, p, map[string]string{"deep/sub/id_rsa": "k\n"})
	for _, in := range []map[string]any{{"pattern": "**/id_*"}, {"pattern": "deep/**/id_*"}} {
		if got := toolCall(t, p, "Glob", in); got != 2 {
			t.Errorf("Glob %v beside a credentials file: exit %d, want 2", in, got)
		}
	}
}

// Grep's path `~` is the home directory, outside the project: its glob is
// read by its name.
func TestGrepHomePathIsTheHome(t *testing.T) {
	t.Parallel()
	p := realTemp(t)
	support.GitInit(t, p)
	support.Write(t, p, map[string]string{"~/keys/a.json": "{}\n"})
	for _, in := range []map[string]any{{"path": "~/keys", "glob": "*.json"}, {"path": "~", "glob": "id_*"}, {"path": "~/keys/id_rsa"}} {
		in["pattern"] = "x"
		if got := toolCall(t, p, "Grep", in); got != 2 {
			t.Errorf("Grep %v: exit %d, want 2", in, got)
		}
	}
}

// Every form that sources a script judges its text: `builtin`, `eval`, a
// shell's -c string, and a script it sources itself.
func TestSourcedScriptEveryForm(t *testing.T) {
	t.Parallel()
	s := credTree(t)
	support.Write(t, s.p, map[string]string{"far.sh": "git push --force\n", "far2.sh": "source far.sh\n"})
	for _, c := range []string{"builtin source far.sh", "builtin . far.sh", "true && builtin source far.sh",
		"eval source far.sh", "bash -c 'source far.sh'", "source far2.sh"} {
		s.code(t, c, sgOpts{}, 2, "a sourced script that runs a destructive command")
	}
}

// Sourcing past the bound, or a sourced script that cannot be read, is not
// passed unseen.
func TestSourcedScriptChainFailsClosed(t *testing.T) {
	t.Parallel()
	s := credTree(t)
	files := map[string]string{"s9.sh": "echo hi\n", "gone.sh": "source missing.sh\n", "locked.sh": "echo hi\n", "shut.sh": "source locked.sh\n"}
	for i := range 9 {
		files["s"+strconv.Itoa(i)+".sh"] = "source s" + strconv.Itoa(i+1) + ".sh\n"
	}
	support.Write(t, s.p, files)
	if err := os.Chmod(filepath.Join(s.p, "locked.sh"), 0); err != nil {
		t.Fatal(err)
	}
	s.code(t, "source s8.sh", sgOpts{}, 0, "a short chain of harmless sourced scripts")
	for _, c := range []string{"source s0.sh", "source shut.sh"} {
		s.code(t, c, sgOpts{}, 2, "a sourced script the guard does not read")
	}
	// A nested optional source of a missing file runs nothing (L295).
	s.code(t, "source gone.sh", sgOpts{}, 0, "a nested source of a missing file")
}

// A sourced or shell-run script whose name is a substitution is a program
// the guard cannot read: it asks, as `eval "$(…)"` does (L270).
func TestSourcedSubstitutionAsks(t *testing.T) {
	t.Parallel()
	s := credTree(t)
	support.Write(t, s.other, map[string]string{"id_rsa": "k\n"})
	support.Write(t, s.p, map[string]string{"far.sh": "cd " + s.other + "\nfind . -name 'id_*' -exec cat {} \\;\n", ".venv/bin/activate": "export VIRTUAL_ENV=.venv\n"})
	for _, c := range []string{"source <(cat far.sh)", ". <(cat far.sh)", "bash <(cat far.sh)", `source "$(echo far.sh)"`, "source $(echo far.sh)",
		". `echo far.sh`", "sh <(cat far.sh)", "true && builtin source <(cat far.sh)"} {
		r := s.bash(t, c, top())
		code(t, r, 0, c)
		matches(t, askedWhy(t, r.Stdout, c), "a value the guard cannot read", c)
	}
	for _, c := range []string{"source .venv/bin/activate", `echo "$(git rev-parse HEAD)"`, "diff <(sort a.txt) <(sort b.txt)", "cat <(echo hi)"} {
		r := s.bash(t, c, top())
		code(t, r, 0, c)
		noDecision(t, r.Stdout, c)
	}
}

// node and bun load the modules -r, --require, --import, --loader or
// --preload name, or NODE_OPTIONS names, before the script: code the guard
// does not read, so the call asks (L265).
func TestNodePreloadAsks(t *testing.T) {
	t.Parallel()
	s := credTree(t)
	support.Write(t, s.p, map[string]string{"ok.cjs": "module.exports = {};\n", "far.mjs": "import { execSync } from 'node:child_process';\nexecSync('git push --force');\n", "ok.mjs": "console.log(1);\n"})
	for _, c := range []string{"node -r ./far.mjs ok.mjs", "node --require ./far.mjs ok.mjs", "node --import ./far.mjs ok.mjs", "node --loader ./far.mjs ok.mjs",
		"bun --preload ./far.mjs ok.mjs", "NODE_OPTIONS='--require ./far.mjs' node ok.mjs", "export NODE_OPTIONS='--import ./far.mjs'; node ok.mjs"} {
		r := s.bash(t, c, top())
		code(t, r, 0, c)
		matches(t, askedWhy(t, r.Stdout, c), "preloads", c)
	}
	for _, c := range []string{"node ok.mjs", "node --experimental-strip-types --test ok.mjs", "NODE_OPTIONS=--max-old-space-size=4096 node ok.mjs"} {
		r := s.bash(t, c, top())
		code(t, r, 0, c)
		noDecision(t, r.Stdout, c)
	}
	s.code(t, "node far.mjs", top(), 2, "the script itself is read")
}

// A preload flag anywhere before the script, past an option's value or a
// bun subcommand, asks; so does an env file, which may set NODE_OPTIONS
// (L294).
func TestNodePreloadAnywhereAsks(t *testing.T) {
	t.Parallel()
	s := credTree(t)
	support.Write(t, s.p, map[string]string{"far.mjs": "import { execSync } from 'node:child_process';\nexecSync('git push --force');\n", "ok.mjs": "console.log(1);\n", "ok.cjs": "module.exports = {};\n",
		"cfg.txt": "NODE_OPTIONS=--require ./far.mjs\n"})
	for _, c := range []string{"bun run --preload ./ok.cjs ok.mjs", "bun test --preload ./ok.cjs", "bun x -r ./ok.cjs tsc", "bun -r ./ok.cjs ok.mjs",
		"node -C dev -r ./ok.cjs ok.mjs", "node --conditions dev --require ./ok.cjs ok.mjs", "node --title x -r ./ok.cjs ok.mjs",
		"nodejs -r ./ok.cjs ok.mjs", "tsx --import ./ok.cjs ok.mjs", "deno run --preload ./ok.cjs ok.mjs"} {
		r := s.bash(t, c, top())
		code(t, r, 0, c)
		matches(t, askedWhy(t, r.Stdout, c), "preloads", c)
	}
	for _, c := range []string{"node --env-file=cfg.txt ok.mjs", "node --env-file cfg.txt ok.mjs", "node --env-file-if-exists=cfg.txt ok.mjs", "nodejs --env-file=cfg.txt ok.mjs"} {
		r := s.bash(t, c, top())
		code(t, r, 0, c)
		matches(t, askedWhy(t, r.Stdout, c), "env file", c)
	}
	for _, c := range []string{"node ok.mjs", "nodejs ok.mjs", "node ok.mjs -r x", "node --test ok.mjs", "bun run build", "npx vitest", "node -C dev ok.mjs"} {
		r := s.bash(t, c, top())
		code(t, r, 0, c)
		noDecision(t, r.Stdout, c)
	}
}

// A script a shell runs or sources by a name the guard cannot resolve, a
// parameter or a descriptor, is a program it does not read (L294).
func TestUnresolvedScriptNameAsks(t *testing.T) {
	t.Parallel()
	s := credTree(t)
	support.Write(t, s.p, map[string]string{"far.sh": "git push --force\n", ".venv/bin/activate": "export VIRTUAL_ENV=.venv\n"})
	for _, c := range []string{`bash -c 'source "$1"' far.sh`, `bash -c 'bash $2' x far.sh`, "bash /dev/fd/3 3< far.sh", "source ${X:-far.sh}",
		". $X", `bash -c 'bash -c "source \$0"' far.sh`} {
		r := s.bash(t, c, top())
		code(t, r, 0, c)
		askedWhy(t, r.Stdout, c)
	}
	// The words after a -c string are its $0, $1, …: the script they name is read.
	for _, c := range []string{`bash -c 'source "$0"' far.sh`, `bash -c 'bash "$1"' x far.sh`, `sh -c '. "${0}"' far.sh`} {
		s.code(t, c, top(), 2, "a script a -c string's parameter names")
	}
	for _, c := range []string{"source .venv/bin/activate", `bash -c 'echo "$0"' x`, "X=.venv/bin/activate; source $X", `bash -c 'source "$0"' .venv/bin/activate`} {
		r := s.bash(t, c, top())
		code(t, r, 0, c)
		noDecision(t, r.Stdout, c)
	}
}
