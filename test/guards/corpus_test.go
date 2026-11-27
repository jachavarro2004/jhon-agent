// The guard regression corpus (T34): real commands from audited sessions
// and every case the guard verifiers found, each with the decision the guard
// gives today, replayed through the built binary on every run. A line
// flagged `known` is a documented hole: it pins today's decision, and its
// subtest name says so; fixing the hole is a change of its decision and
// flag.
//
// Each file under corpus/ is tab-separated, `#` starts a comment, and a line
// is: guard, tool, input, decision, fixture, source, known, note.
//   - guard: safety-guard, secrets-guard or framework-guard.
//   - tool: Bash (input is the command) or Read/Grep/Glob (input is the
//     tool_input as JSON).
//   - input: a Go-quoted string, with the placeholders <P> (the project),
//     <OTHER> (a repository beside it), <SCRATCH> (their parent), <HOME>
//     (the guard's HOME) and <FW> (the framework's source).
//   - decision: allow, ask or refuse, as the main session in default mode
//     sees it (safety-guard asks only a caller the owner can answer).
//   - fixture: none, cred-near (credentials files inside the project),
//     cred-far (credentials files outside it, beside and above), framework
//     (the call runs in the project), framework-cwd (it runs in <FW>).
//   - source: where the case comes from (audit-session, L230, L265, …).
//   - known: `known` for a documented hole, `-` otherwise.
//
// The 6-column files beside them (guarddiffLines) hold no decision: they are
// lines only tools/guarddiff runs (its README), with the two above.

package guards

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

type corpusCase struct {
	at, guard, tool, input, want, fixture, source, note string
	known                                               bool
}

var (
	corpusGuards    = map[string]bool{"safety-guard": true, "secrets-guard": true, "framework-guard": true}
	corpusDecisions = map[string]bool{"allow": true, "ask": true, "refuse": true}
	corpusFixtures  = map[string]bool{"none": true, "cred-near": true, "cred-far": true, "framework": true, "framework-cwd": true}
	guarddiffLines  = map[string]bool{"fuzz.tsv": true, "matrix.tsv": true, "rows.tsv": true, "inspect.tsv": true, "workingset.tsv": true}
)

func readCorpus(t *testing.T) []corpusCase {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("corpus", "*.tsv"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no corpus files under corpus/: %v", err)
	}
	var out []corpusCase
	for _, f := range files {
		if guarddiffLines[filepath.Base(f)] {
			continue
		}
		fh, err := os.Open(f)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for n := 1; sc.Scan(); n++ {
			line := sc.Text()
			if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
				continue
			}
			at := filepath.Base(f) + ":" + strconv.Itoa(n)
			col := strings.Split(line, "\t")
			if len(col) != 8 {
				t.Fatalf("%s: %d columns, want 8", at, len(col))
			}
			input, err := strconv.Unquote(col[2])
			if err != nil {
				t.Fatalf("%s: the input is not a Go-quoted string: %v", at, err)
			}
			c := corpusCase{at: at, guard: col[0], tool: col[1], input: input, want: col[3], fixture: col[4], source: col[5], known: col[6] == "known", note: col[7]}
			if !corpusGuards[c.guard] || !corpusDecisions[c.want] || !corpusFixtures[c.fixture] || (col[6] != "known" && col[6] != "-") || c.source == "" {
				t.Fatalf("%s: a malformed line: %q", at, line)
			}
			if c.guard != "safety-guard" && c.want == "ask" {
				t.Fatalf("%s: only safety-guard asks", at)
			}
			out = append(out, c)
		}
		fh.Close()
		if err := sc.Err(); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// corpusTree is the project every safety and secrets case runs in:
// credTree's JSON files, the scripts the cases name, and the fixture's
// credentials files.
func corpusTree(t *testing.T, fixture string) sgEnv {
	t.Helper()
	s := credTree(t)
	farScript := "cd " + s.other + "\nfind . -name 'id_*' -exec cat {} \\;\n"
	files := map[string]string{
		"dirs.txt": "x\n", "far.sh": farScript, "far2.sh": "source far.sh\n", "deploy/far.sh": farScript, "deploy/ok.sh": "echo hi\n",
		// T271: a template, which a credentials-named glob reaches without reading credentials.
		"deploy/.env.example":             "KEY=\n",
		".venv/bin/activate":              "VIRTUAL_ENV=.venv\nexport VIRTUAL_ENV\nPATH=\"$VIRTUAL_ENV/bin:$PATH\"\nexport PATH\n",
		"deploy/env.sh":                   "export STAGE=dev\n[ -f local.sh ] && source local.sh\n",
		"deploy/verify-deploy-script.mjs": scriptGlobbing("i18n/**.json", ""),
		"deploy/v.py":                     "import glob, subprocess\nfiles = glob.glob('i18n/**.json', recursive=True)\nsubprocess.run(['echo', str(len(files))])\n",
		"s9.sh":                           "echo hi\n", "gone.sh": "source missing.sh\n",
		// The L265 advisories: code the guard does not read reaches a credentials file above the project.
		"deploy/pre.cjs": "require('child_process').execSync('cat ../id_rsa');\n", "deploy/ok.cjs": "module.exports = {};\n",
		"deploy/far.mjs": "import { execSync } from 'node:child_process';\nexecSync('cat ../id_rsa');\n", "deploy/ok.mjs": "console.log(1);\n",
		"deploy/up.mjs":                         scriptGlobbing("*", "process.chdir([String.fromCharCode(46, 46)].join(''));\n"),
		"colorsys.py":                           "import subprocess\nsubprocess.run(['cat', '../id_rsa'])\n",
		"tools/run.mjs":                         scriptGlobbing("i18n/**.json", ""),
		"tools/node_modules/fast-glob/index.js": "require('child_process').execSync('cat ../id_rsa');\nmodule.exports = { sync: () => [] };\n",
		// The L294 env file: node --env-file loads it, and its NODE_OPTIONS preloads a module.
		"cfg.txt": "NODE_OPTIONS=--require ./deploy/pre.cjs\n",
		// L295: a nested source of a file that exists but cannot be read.
		"shut.sh": "source locked.sh\n", "locked.sh": "echo hi\n",
		// A nested script that writes a file and then sources it (an L295 advisory).
		"genc.sh": "cp far.sh g3.sh; source g3.sh\n",
		// The session's bootstrap script, a stub of what it runs, so its calls judge a real script.
		".github/scripts/bootstrap-registry.sh": "#!/usr/bin/env bash\nset -euo pipefail\n" +
			"ensure_repository() { aws ecr describe-repositories --repository-names \"$1\" >/dev/null || aws ecr create-repository --repository-name \"$1\"; }\n" +
			"push_rule_json() { echo \"{\\\"account\\\": \\\"$1\\\", \\\"repo\\\": \\\"$2\\\"}\"; }\n" +
			"main() { ensure_repository acme/a; }\n[ \"${BASH_SOURCE[0]}\" = \"$0\" ] && main \"$@\"\n",
	}
	for i := range 9 {
		files["s"+strconv.Itoa(i)+".sh"] = "source s" + strconv.Itoa(i+1) + ".sh\n"
	}
	support.Write(t, s.p, files)
	if err := os.Chmod(filepath.Join(s.p, "locked.sh"), 0); err != nil {
		t.Fatal(err)
	}
	// The audited session's deploy helpers, written under /tmp there and named <TMP> here.
	support.Write(t, s.tmp, map[string]string{"acme-deploy/run-task.sh": "#!/usr/bin/env bash\naws ecs run-task --cluster c --task-definition \"$1\"\n",
		"acme-deploy/scale.sh":         "#!/usr/bin/env bash\nfor s in \"$@\"; do aws ecs update-service --cluster c --service \"$s\" --desired-count 1; done\n",
		"acme-deploy/retag-taskdef.sh": "#!/usr/bin/env bash\naws ecs register-task-definition --cli-input-json \"$1\"\n"})
	switch fixture {
	case "cred-near":
		support.Write(t, s.p, map[string]string{"i18n/deep/x/credentials.json": "{}\n", "snapshots/credentials.json": "{}\n",
			"deep/sub/id_rsa": "k\n", "deep/c/credentials.json": "{}\n", "keys/id_rsa.old": "k\n"})
	case "cred-far":
		support.Write(t, s.other, map[string]string{"id_rsa": "k\n", "n/credentials.json": "{}\n"})
		support.Write(t, s.scratch, map[string]string{"id_rsa": "k\n"})
	}
	return s
}

// decide runs c through its guard and names the decision it printed.
func (c corpusCase) decide(t *testing.T) (string, support.Result) {
	t.Helper()
	var r support.Result
	switch c.guard {
	case "framework-guard":
		e := newFwEnv(t)
		input := strings.NewReplacer("<FW>", e.fw, "<P>", e.project, "<HOME>", e.home).Replace(c.input)
		who := fwWho{}
		if c.fixture == "framework-cwd" {
			who.cwd = e.fw
		}
		r = e.guard(t, c.tool, c.toolInput(t, input), who)
	default:
		s := corpusTree(t, c.fixture)
		input := strings.NewReplacer("<HOME>", sgHome).Replace(s.expand(c.input))
		if c.guard == "safety-guard" {
			r = s.bash(t, input, top())
		} else {
			payload := map[string]any{"tool_name": c.tool, "tool_input": c.toolInput(t, input), "cwd": s.p, "agent_id": "a1", "agent_type": "worker"}
			r = hook(t, s.env, "secrets-guard", []string{"--agent", "*"}, payload)
		}
	}
	switch {
	case r.Code == 2:
		return "refuse", r
	case r.Code != 0:
		t.Fatalf("%s: exit %d: %s", c.at, r.Code, r.Stderr)
	}
	if d, _, ok := decision(t, r.Stdout); ok && d == "ask" {
		return "ask", r
	}
	return "allow", r
}

func (c corpusCase) toolInput(t *testing.T, input string) map[string]any {
	t.Helper()
	if c.tool == "Bash" {
		return cmd(input)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("%s: the %s input is not a JSON object: %v", c.at, c.tool, err)
	}
	return m
}

func (c corpusCase) name() string {
	n := c.source + "/"
	if c.known {
		n += "known_hole_"
	}
	return n + c.at
}

func TestGuardCorpus(t *testing.T) {
	t.Parallel()
	for _, c := range readCorpus(t) {
		t.Run(c.name(), func(t *testing.T) {
			t.Parallel()
			got, r := c.decide(t)
			if got == c.want {
				return
			}
			what := "a regression"
			if c.known {
				what = "a documented hole changed its decision: if it is fixed, update its line's decision and drop `known`"
			}
			t.Errorf("%s (%s, %s): %q decides %s, the corpus says %s: %s\n%s%s", c.at, what, c.note, c.input, got, c.want, c.guard, r.Stderr, r.Stdout)
		})
	}
}
