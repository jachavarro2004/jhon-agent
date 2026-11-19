// What falls outside T28's allow-list keeps today's rule: a glob whose name
// could match a credentials file is refused, whether or not one exists.

package guards

import (
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// Programs that search or move on their own, and lines that are more than
// one simple command (L230, L233, L242).
var programForms = []string{`find . -name 'id_*' -exec cat {} \;`, `find . -path '*id_*' -exec cat {} \;`, `find . -iname 'ID_*' -exec cat {} \;`,
	`rg -g 'id_*' x`, `rg --glob 'cred*' x`, `grep -r --include='cred*' x .`, `tar cf - --wildcards 'id_*' .`,
	`git add 'id_*'`, `git ls-files 'cred*'`, `find . -name 'id_*' -print0 | xargs -0 cat`,
	`find . -name '*.json' -exec cat {} +`}

var elsewhereForms = []string{`find <OTHER> -name 'id_*' -exec cat {} \;`, `find .. -name 'id_*' -exec cat {} \;`,
	`find / -name 'id_*' -exec cat {} \;`, `find ~ -name 'id_*' -exec cat {} \;`, `rg -g 'id_*' . <OTHER>`,
	`rg --files -g 'cred*' <OTHER>`, `grep -r --include='id_*' k <OTHER>`, `tar cf - -C <OTHER> --wildcards 'id_*' .`,
	`git -C <OTHER> ls-files 'id_*'`, `find -L . -name 'id_*'`, `git --work-tree=<OTHER> ls-files 'id_*'`,
	`rg -g 'id_*' x $HOME`, "cd . && rg -g 'cred*' x `pwd`/..", `echo; find <OTHER> -name x; rg -g 'id_*' x`}

var magicForms = []string{`git grep k -- ':(glob)**/id_*'`, `git add ':(glob)**/id_*'`, `git ls-files ':(glob)**/id_*'`,
	`git archive HEAD -- ':(glob)**/cred*'`, `git add ':/*id_*'`, `git add ':(top)*id_*'`, `git grep -- ':(icase)**/ID_*'`}

// hiddenForms take their search paths from a file or a pipe (L242); a
// cd to outside the project is the sandbox's (T128f).
var hiddenForms = []string{`xargs -a dirs.txt grep -r --include='id_*' k`,
	`xargs -a dirs.txt rg -uu -g 'id_*' k`, `cat dirs.txt | xargs -I{} rg -g 'cred*' k {}`, `tar -T dirs.txt --wildcards 'id_*' -cf -`}

// shapeForms break one of the allow-list's conditions on an allowed reader.
var shapeForms = []string{"cat `echo x` *.json", "sudo cat *.json", "xargs cat *.json", "eval cat *.json", "cat $PWD/*.json", "cat :*.json", "cat @*.json",
	"wc --files0-from=list *.json", "jq -f prog.jq *.json", "jq --rawfile x a.txt . *.json", `jq 'import "a" as $a; .' *.json`,
	"jq --from-file prog.jq *.json", "yq -i . *.json", `yq 'load("x")' *.json`, "jq -L . . *.json"}

// extensionForms were shapeForms and programForms until T87b (L630-L631):
// their `*.json` names a credentials file only by its extension, on a line
// that writes nothing, so it is walked; refused once a credentials file is
// within reach.
var extensionForms = []string{"git add -- '*.json'", "grep -l x i18n/*/*.json", "cd i18n && cat *.json", "X=1 cat *.json", "cat *.json | head", "cat *.json; true",
	"(cat *.json)", "{ cat *.json; }", "cat $(echo *.json)", "env cat *.json", "command cat *.json", "builtin cat *.json", "exec cat *.json", "timeout 5 cat *.json",
	"nice cat *.json", "cat *.json &", "cat *.json && true", "cat *.json || true", "cat <(cat *.json)", "cat *.json <<EOF\nx\nEOF", "cat ./../proj/*.json",
	"/usr/bin/cat *.json", "tail -n x *.json", "less *.json", "sort *.json", "cat \\\n*.json"}

func TestCredentialGlobExtensionFormsAreWalked(t *testing.T) {
	t.Parallel()
	s := credTree(t)
	for _, c := range extensionForms {
		s.code(t, c, sgOpts{}, 0, "an extension glob on a line that writes nothing, no credentials file")
	}
	support.Write(t, s.p, map[string]string{"i18n/x/credentials.json": "{}\n"})
	for _, c := range extensionForms {
		s.code(t, c, sgOpts{}, 2, "an extension glob within reach of a credentials file")
	}
}

func TestCredentialGlobOutsideTheAllowListIsRefused(t *testing.T) {
	t.Parallel()
	s := credTree(t)
	support.Write(t, s.p, map[string]string{"dirs.txt": "x\n"})
	support.Write(t, s.other, map[string]string{"id_rsa": "k\n", "n/credentials.json": "{}\n"})
	for _, forms := range [][]string{programForms, elsewhereForms, magicForms, hiddenForms, shapeForms} {
		for _, c := range forms {
			s.code(t, s.expand(c), sgOpts{}, 2, "outside the allow-list, with no credentials file in the project")
		}
	}
	support.Write(t, s.p, map[string]string{"deep/sub/id_rsa": "k\n", "deep/c/credentials.json": "{}\n"})
	for _, forms := range [][]string{programForms, magicForms, hiddenForms} {
		for _, c := range forms {
			s.code(t, s.expand(c), sgOpts{}, 2, "outside the allow-list, beside a nested credentials file")
		}
	}
}

// A glob that cannot name a credentials file passes as it always did.
func TestCredentialGlobThatNamesNoneStillPasses(t *testing.T) {
	t.Parallel()
	s := credTree(t)
	for _, c := range []string{`git ls-files 'i18n/*'`, `find <OTHER> -name '*.go'`, `rg -g '*.ts' x <OTHER>`, "cat i18n/* | head"} {
		s.code(t, s.expand(c), sgOpts{}, 0, "a glob that cannot name a credentials file")
	}
}

// A script's text: `node <script>` (python3, bun) passes when every glob
// string it holds is relative and reaches no credentials file, and its text
// can name no other directory.
func TestCredentialGlobInAScriptsText(t *testing.T) {
	t.Parallel()
	s := credTree(t)
	support.Write(t, s.p, map[string]string{"deploy/verify-deploy-script.mjs": scriptGlobbing("i18n/**.json", ""), "deploy/env.mjs": scriptGlobbing(".env*", ""),
		"deploy/v.py": "import glob, subprocess\nfiles = glob.glob('i18n/**.json', recursive=True)\nsubprocess.run(['echo', str(len(files))])\n"})
	s.code(t, "node deploy/verify-deploy-script.mjs", sgOpts{}, 0, "a script whose text globs i18n/**.json")
	s.code(t, "bun deploy/verify-deploy-script.mjs", sgOpts{}, 0, "bun, the same script")
	s.code(t, "python3 deploy/v.py", sgOpts{}, 0, "a python script whose text globs i18n/**.json")
	s.code(t, "node deploy/env.mjs", sgOpts{}, 0, "a script whose text globs .env* with no dotenv file")
	support.Write(t, s.p, map[string]string{"sub/i18n/credentials.json": "{}\n"})
	s.code(t, "node deploy/verify-deploy-script.mjs", sgOpts{}, 2, "a script's glob reaches a credentials file at any depth")
	s.code(t, "python3 deploy/v.py", sgOpts{}, 2, "a python script's glob reaches a credentials file")
}

func scriptGlobbing(glob, extra string) string {
	return "import { execSync } from 'node:child_process';\nimport fg from 'fast-glob';\n" + extra +
		"const files = fg.sync('" + glob + "');\nexecSync('echo ' + files.length);\n"
}

func TestCredentialGlobInAScriptOutsideTheAllowList(t *testing.T) {
	t.Parallel()
	s := credTree(t)
	bad := map[string]string{
		"abs.mjs": scriptGlobbing("i18n/**.json", "const x = '/srv';\n"), "home.mjs": scriptGlobbing("i18n/**.json", "import os from 'node:os'; os.homedir();\n"),
		"up.mjs": scriptGlobbing("i18n/**.json", "const x = '../';\n"), "env.mjs": scriptGlobbing("i18n/**.json", "process.env.HOME;\n"),
		"dir.mjs": scriptGlobbing("i18n/**.json", "const d = __dirname;\n"), "cwd.mjs": scriptGlobbing("i18n/**.json", "fg.sync('x', { cwd: 'sub' });\n"),
		"local.mjs": scriptGlobbing("i18n/**.json", "import h from './helper.mjs';\n"), "cd.mjs": scriptGlobbing("i18n/**.json", "execSync('cd sub');\n"),
		"find.mjs": scriptGlobbing("i18n/**.json", "execSync('find sub');\n"), "var.mjs": scriptGlobbing("${d}/i18n/**.json", ""),
		"argv.mjs": scriptGlobbing("i18n/**.json", "process.argv;\n"),
		"home.py":  "import subprocess, glob\nsubprocess.run(['cat', 'i18n/**.json'])\nglob.glob('~/x')\n",
	}
	for f, text := range bad {
		support.Write(t, s.p, map[string]string{"deploy/" + f: text})
	}
	support.Write(t, s.other, map[string]string{"v.mjs": scriptGlobbing("i18n/**.json", "")})
	support.Write(t, s.p, map[string]string{"deploy/ok.mjs": scriptGlobbing("i18n/**.json", "")})
	for f := range bad {
		prog := "node"
		if strings.HasSuffix(f, ".py") {
			prog = "python3"
		}
		s.code(t, prog+" deploy/"+f, sgOpts{}, 2, "a script the allow-list cannot prove")
	}
	for _, c := range []string{"node <OTHER>/v.mjs", "node --input-type=module deploy/ok.mjs", "node -e \"require('fast-glob').sync('i18n/**.json'); require('child_process').execSync('echo')\"",
		"node deploy/ok.mjs | cat", "X=1 node deploy/ok.mjs", "node deploy/ok.mjs 2>&1; true"} {
		s.code(t, s.expand(c), sgOpts{}, 2, "an interpreter the allow-list cannot prove")
	}
}

// A glob CredentialFile reads by its name (`x*` with a key's extension), a
// name the program would read as an option, and a Python module the
// script's directory shadows: none is proven.
func TestCredentialGlobEdgesKeepTheNameRule(t *testing.T) {
	t.Parallel()
	s := credTree(t)
	support.Write(t, s.p, map[string]string{"keys/xa.p12": "k\n", "-n.json": "{}\n",
		"deploy/v.py": "import glob, subprocess\nsubprocess.run(['cat', 'i18n/**.json'])\n", "deploy/glob.py": "\n"})
	s.code(t, "cat keys/x*.p12", sgOpts{}, 2, "a glob CredentialFile reads, beside a key")
	s.code(t, "wc -l *.json", sgOpts{}, 2, "a file whose name is an option")
	s.code(t, "python3 deploy/v.py", sgOpts{}, 2, "a script whose import its directory shadows")
}
