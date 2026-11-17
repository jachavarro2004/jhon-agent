// A temporary directory the same command line made with `mktemp -d` is the
// agent's own scratch: removing it, or a path under it, resolves to that
// directory (T64c). Every other spelling keeps "its target cannot be
// resolved".

package guards

import (
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func scratchTree(t *testing.T) sgEnv {
	t.Helper()
	s := newSG(t)
	support.Write(t, s.p, map[string]string{"cli.py": "print(1)\n", "data/supplier.csv": "a,b\n"})
	return s
}

var scratchRemovals = []string{
	"python3 -m unittest 2>&1 | tail -5 && d=$(mktemp -d) && python3 cli.py convert data/supplier.csv $d/out.jsonl && cat $d/out.jsonl && rm -r $d",
	"d=$(mktemp -d) && rm -rf $d", `d=$(mktemp -d); rm -rf "$d"`, "d=$(mktemp -d) && rm -rf ${d}", `d=$(mktemp -d) || rm -rf "${d}"`,
	"d=$(mktemp -d) && cat $d/x && rm -rf $d", "tmp=$(mktemp -d -t jhon.XXXXXX) && rm -rf $tmp",
	"d=$(mktemp -d -p /tmp) && rm -r $d", "d=$(mktemp -d /tmp/x.XXXXXX) && rm -r $d", `d=$(mktemp -d "$TMPDIR/x.XXXXXX") && rm -rf "$d"`,
	"d=$(mktemp -d) && rmdir $d", "rm -r $(mktemp -d)", `rm -rf "$(mktemp -d)"`,
	// A cd does not move a plain mktemp -d (T109 WU1, L793 pin).
	"cd /etc && L=$(mktemp -d) && rm -r $L",
}

// ownersSeedLine is the owner's refused line (L629, T87b): a newline inside
// a double-quoted argument is literal, so `rm -rf $tmp` resolves. Its
// `> $tmp/seed.py`, the line's first use of the directory, writes a file
// under it, which resolves (T94, L635); any later write there stays
// unresolvable (L514).

const ownersSeedLine = `cd . && rg -ln "bia_regulator" alembic/ ; git log --oneline -1 -- cli.py; tmp=$(mktemp -d); git show HEAD:cli.py > $tmp/seed.py; python3 -c "
import importlib.util,sys
s=importlib.util.spec_from_file_location('s','$tmp/seed.py');m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
print([p for p in m.PERMISSIONS if p[1]=='update-status' and 'regulat' in p[0]])"; rm -rf $tmp`

func TestScratchDirMadeByMktempIsRemovable(t *testing.T) {
	t.Parallel()
	s := scratchTree(t)
	for _, c := range scratchRemovals {
		s.code(t, c, sgOpts{}, 0, "a sub-agent removes the line's own mktemp directory")
		s.code(t, c, top(), 0, "the top level removes the line's own mktemp directory")
		for _, child := range []bool{false, true} {
			if r := s.piCall(t, c, child); r.Code != 0 {
				t.Errorf("pi (child %v): %q exits %d: %s%s", child, c, r.Code, r.Stderr, r.Stdout)
			}
		}
	}
}

var scratchRefusals = []string{
	"d=$(mktemp -d); d=/; rm -rf $d", "d=$(mktemp -d) ; cd $d ; rm -rf ..", "d=$(mktemp -d) && rm -rf $d/../..", "d=$(mktemp -d) && rm -rf $d $HOME",
	"d=$(mktemp -d) && rm -rf $e", "rm -rf $(mktemp -d)/../x", "read d; rm -rf $d", "d=$(mktemp) && rm -rf $d/..",
	"f() { d=$(mktemp -d); }; f; rm -rf $d/x", "(d=$(mktemp -d)); rm -rf $d/x", "{ d=$(mktemp -d); } && rm -rf $d/x",
	"d=$(mktemp -d); rm -rf $d/x", "true || d=$(mktemp -d) && rm -rf $d/x", "rm -rf $d/x; d=$(mktemp -d)", "d=$(mktemp -d) & rm -rf $d/x",
	"d=$(mktemp -d) && eval true && rm -rf $d/x", "d=$(mktemp -d) && export d && rm -rf $d/x", "d=$(mktemp -d x.XXXXXX) && rm -rf $d/x",
	"d=$(mktemp -d /srv/x.XXXXXX) && rm -rf $d/x", "d=$(mktemp -d) && d=$(mktemp -d) && rm -rf $d/x", "d=$(mktemp -d) && unset d && rm -rf $d/x",
	"d=$(mktemp -d) && bash -c 'rm -rf $d/x'", "d=$(mktemp -d) && rm -rf $d/*/..", "d=$(mktemp -d $(echo /)) && rm -rf $d/x",
	"d=$(mktemp -d) && rm -rf $d$e", "x=$(rm -rf $(mktemp -d))", "rm -rf $(mktemp -d) /", "d=$(mktemp -d) && sudo rm -rf $d/x",
	"d=$(mktemp -d) && rm -rf ${d:-/}/x", "d=$(mktemp -d) | rm -rf $d/x",
	// The verifier's (L514): only `$d` whole, as an rm or rmdir operand, resolves.
	"d=$(mktemp -d) && ln -s $HOME $d/h && rm -r $d/h/x", `d=$(mktemp -d) && ln -s "$HOME" $d/h && rm -r $d/h/x`,
	"d=$(mktemp -d) && ln -s ${HOME} $d/h && rm -r $d/h/x", "d=$(mktemp -d) && ln -s $PWD/.. $d/h && rm -r $d/h/x",
	"d=$(mktemp -d) && ln -s data $d/h && rm -r $d/h/x", "d=$(mktemp -d) && ln -s $HOME $d/h && rm -r $d/h/",
	"d=$(mktemp -d) && rm -r $d && ln -s $HOME/proj $d && rm -r $d/x", "d=$(mktemp -d) && rm -r $d && ln -s $HOME/proj $d && rm -r $d",
	"d=$(mktemp -d) && rm -r $d && mv $HOME/proj $d && rm -r $d", "d=$(mktemp -d) && rmdir $d && cp -r $HOME/proj $d && rm -r $d",
	`d=$(mktemp -d) && ln -s $HOME/proj $d/h && rm -r "$d"/*/..`, "d=$(mktemp -d) && tar -C $d -xf x.tar && rm -r $d/x",
	"d=$(mktemp -d) && unzip x.zip -d $d && rm -r $d/x", "d=$(mktemp -d) && python3 mk.py $d && rm -r $d/x",
	"d=$(mktemp -d) && cp -as $HOME/proj $d/p && rm -r $d/p/x", "d=$(mktemp -d) && mount --bind $HOME $d && rm -r $d/x",
	"d=$(mktemp -d) && cd / && IFS=/ && rm -r $d/x", "d=$(mktemp -d) && IFS=/ && rm -r $d", "d=$(mktemp -d) && export IFS && rm -r $d",
	"d=$(mktemp -d) && IFS=/ read x && rm -r $d", "d=$(mktemp -d) && declare IFS=/ && rm -r $d", "d=$(mktemp -d) && local IFS=/ && rm -r $d",
	"d=$(mktemp -d) && typeset IFS=/ && rm -r $d",
	"d=$(mktemp -d) && rm -r $d/{x,../..}", "d=$(mktemp -d) && rm -r $d/{..,x}", "d=$(mktemp -d) && rm -r $d/.?",
	"d=$(mktemp -d) && rm -r $d/x", "d=$(mktemp -d) && rm -r $d/", "d=$(mktemp -d) && rm -r ${d}/", `d=$(mktemp -d) && rm -r "$d"/*`,
	"d=$(mktemp -d) && unlink $d/x", "d=$(mktemp -d) && find $d -delete", "d=$(mktemp -d) && rm -rf $d $d/x",
	"rm -r $d/x", "rm -r $d/",
	// The owner's line with a substitution inside its quoted argument.
	strings.Replace(ownersSeedLine, "\nimport", "\n$(rm -rf /x)import", 1), strings.Replace(ownersSeedLine, "\nimport", "\n`true`import", 1),
	// The T87b verifier's (L636): a reassignment after a quoted newline.
	"tmp=$(mktemp -d); echo \"a\n\"; tmp=$HOME; rm -rf $tmp", "tmp=$(mktemp -d); echo \"a\n\\\"; tmp=/; echo \\\"\"; rm -rf $tmp",
	"tmp=$(mktemp -d); echo \"a\n${tmp:=/}\"; rm -rf $tmp",
}

func TestScratchDirOwnersSeedLineVariantsRefuseTheRm(t *testing.T) {
	t.Parallel()
	s := scratchTree(t)
	for _, c := range []string{strings.Replace(ownersSeedLine, "tmp=$(mktemp -d)", "tmp=$(pwd)", 1), strings.Replace(ownersSeedLine, "; rm -rf $tmp", "; tmp=/; rm -rf $tmp", 1),
		strings.Replace(ownersSeedLine, "tmp=$(mktemp -d); ", "", 1)} {
		c = strings.Replace(c, " > $tmp/seed.py", "", 1)
		r := s.bash(t, c, sgOpts{})
		code(t, r, 2, c)
		matches(t, r.Stderr, `rm -r \$\(…\)|rm -r \$tmp|rm -r /`, c)
	}
}

func TestScratchDirOwnersSeedLine(t *testing.T) {
	t.Parallel()
	s := scratchTree(t)
	s.code(t, ownersSeedLine, top(), 0, "the top level")
	r := s.bash(t, ownersSeedLine, sgOpts{})
	if strings.Contains(r.Stderr, "rm -r") {
		t.Errorf("the line's own mktemp directory is refused: %s", r.Stderr)
	}
	// Its first use of the fresh directory, a redirect, resolves under it (T94, L635).
	code(t, r, 0, "a sub-agent, the line with the write")
	without := strings.Replace(ownersSeedLine, " > $tmp/seed.py", "", 1)
	s.code(t, without, sgOpts{}, 0, "a sub-agent, the line without the write")
	if r := s.piCall(t, without, true); r.Code != 0 {
		t.Errorf("pi child: exits %d: %s", r.Code, r.Stderr)
	}
}

func TestScratchDirRemovalKeepsRefusingTheRest(t *testing.T) {
	t.Parallel()
	s := scratchTree(t)
	for _, c := range scratchRefusals {
		s.code(t, c, sgOpts{}, 2, "not provably the line's own mktemp directory")
		if r := s.piCall(t, c, true); r.Code != 2 {
			t.Errorf("pi child: %q exits %d: %s", c, r.Code, r.Stderr)
		}
	}
	// The HOME target is refused on its own, the scratch one is not named.
	r := s.bash(t, "d=$(mktemp -d) && rm -rf $d $HOME", sgOpts{})
	matches(t, r.Stderr, `rm -r \$HOME: it reaches the home directory`, "HOME")
	if strings.Contains(r.Stderr, "rm -r $d:") {
		t.Errorf("the scratch directory is refused: %s", r.Stderr)
	}
}

// sweepLines are the safety-guard refusals the bench met before T64c's list
// rule (baseline-752cb382, candidate-2166f831, quick-f772832e,
// candidate-15806fe7; L519), each as run. candidate-2166f831's
// account-import line is not here: its `> $d/old_accounts.py` writes under
// the scratch directory, which stays unresolvable (L514), and is asked.
var sweepLines = []string{
	"git status; git log --oneline | head; ls -R | grep -v '^\\.' | head -50; cat *.py; cat data/*.json; cat openspec/config.yaml; ls tests 2>/dev/null; jhon status; jhon risk",
	"jhon executions run invoice-csv-export 2>&1 | grep -E \"^(FAIL|ERROR|Ran|FAILED|OK)|^fail|^pass\"; d=$(mktemp -d); python3 cli.py export data/invoices.json $d/a.csv --since 2026-02-01; echo \"exit $?\"; cat $d/a.csv; python3 cli.py export data/invoices.json $d/b.csv --since 2026-13-01; echo \"exit $?\"; ls $d; rm -r $d",
	"python3 -m unittest 2>&1 | tail -3; d=$(mktemp -d); python3 cli.py export data/invoices.json \"$d/x.csv\" --since 2026-02-31; echo \"exit=$? exists=$(test -e \"$d/x.csv\" && echo yes || echo no)\"; python3 cli.py export data/invoices.json \"$d/x.csv\" --since 2026-02-01; echo \"exit=$?\"; cat \"$d/x.csv\"; rm -r \"$d\"",
	"python3 -m unittest -v 2>&1 | tail -30; t=$(mktemp -d); python3 cli.py convert data/supplier.csv $t/out.jsonl; echo \"exit $?\"; cat $t/out.jsonl; rm -r $t",
	"git status && git log --oneline | head && ls -R | grep -v '^\\.git' | head -50 && cat *.py data/*.json openspec/config.yaml 2>/dev/null; jhon status; jhon risk",
	"git status --short && ls -R | grep -v pycache | head -50 && cat *.py; cat data/*.json; ls tests 2>/dev/null && cat tests/*.py",
	"git status --short && ls -la && cat *.py | head -200 && ls tests 2>/dev/null; find . -name \"*.json\" -not -path \"./.git/*\" | head",
	"jhon executions run catalog-convert 2>&1 | tail -8 && d=$(mktemp -d) && python3 cli.py convert data/supplier.csv $d/out.jsonl && cat $d/out.jsonl && rm -r $d",
	"python3 -m unittest 2>&1 | tail -5 && d=$(mktemp -d) && python3 cli.py convert data/supplier.csv $d/out.jsonl && cat $d/out.jsonl && rm -r $d",
	"git status --short && git log --oneline | head -5 && ls -R | grep -v '^\\.git' | head -50 && cat *.py; cat data/*.json; ls tests 2>/dev/null",
}

// sweepTree is a repository shaped like the bench templates, with no credentials file.
func sweepTree(t *testing.T) sgEnv {
	t.Helper()
	s := newSG(t)
	support.Write(t, s.p, map[string]string{"migrate.py": "x = 1\n", "store.py": "y = 2\n", "cli.py": "print(1)\n", "accounts.py": "z = 3\n",
		"data/contacts.json": "[]\n", "data/invoices.json": "[]\n", "data/supplier.csv": "a,b\n", "openspec/config.yaml": "schema: x\n",
		"tests/test_a.py": "w = 4\n"})
	return s
}

func TestCredentialAndScratchSweepLinesPass(t *testing.T) {
	t.Parallel()
	s := sweepTree(t)
	for _, c := range sweepLines {
		s.code(t, c, sgOpts{}, 0, "a bench line, no credentials file")
		s.code(t, c, top(), 0, "a bench line at the top level, no credentials file")
		if r := s.piCall(t, c, true); r.Code != 0 {
			t.Errorf("pi child: %q exits %d: %s", c, r.Code, r.Stderr)
		}
	}
}

// The `;` forms of what L514 refuses stay refused.
func TestScratchDirInASemicolonListKeepsRefusing(t *testing.T) {
	t.Parallel()
	s := sweepTree(t)
	for _, c := range []string{"d=$(mktemp -d); IFS=/; rm -r $d", "d=$(mktemp -d); ln -s $HOME $d/h; rm -r $d", "d=$(mktemp -d); mv cli.py $d; rm -r $d",
		"d=$(mktemp -d); rm -r $d; ln -s $HOME $d; rm -r $d", "d=$(mktemp -d); rm -r $d; cat $d", "d=$(mktemp -d); rm -r $d; rm -r $d",
		"(d=$(mktemp -d)); rm -r $d", "{ d=$(mktemp -d); }; rm -r $d", "true || d=$(mktemp -d); rm -r $d", "false && d=$(mktemp -d); rm -r $d",
		"x && d=$(mktemp -d); rm -r $d", "d=$(mktemp -d) | cat; rm -r $d", "d=$(mktemp -d) & rm -r $d", "d=$(mktemp -d)\nrm -r $d",
		"d=$(mktemp -d); echo 'x $d'; rm -r $d", `d=$(mktemp -d); echo "a\\b"; rm -r $d`, "d=$(mktemp -d); cd $d; rm -r $d",
		"d=$(mktemp -d); rm -r $d/x", "d=$(mktemp -d); rm -r $d/", "d=$(mktemp -d); export IFS; rm -r $d"} {
		s.code(t, c, sgOpts{}, 2, "a ; list the scratch rule does not prove")
	}
}
