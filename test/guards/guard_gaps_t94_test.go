// T94 (L635, L636, L638, L647, L652, L656): guard gaps closed by tightening.
// Inline interpreter code that writes, git apply, patch and a variable write
// target count as writes for T89's first-edit rule and scope-guard; a
// sub-agent's redirect into the line's fresh mktemp directory resolves; an
// edit tool in a home credentials directory is refused; `<rev>:<path>`
// naming a credentials file is judged by its path; MultiEdit is routed like
// Edit.

package guards

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// unresolvedNote is scope-guard's advisory on a bounded writer's shell write
// whose target it cannot resolve (T153, L957: it ran, and is not refused).
const unresolvedNote = `^note \(not a refusal; the call ran\): the command writes .*a target the guard cannot resolve.*, which the guard cannot hold to the allowed edit surfaces`

// Rule 1 (L656) and rule 6 (L652): a write the parser cannot name a file
// for, in the repository, counts as the first code edit, which runs with
// route-guard's set-up note since T153 (L957); a write it resolves outside
// the repository, or code that writes only to standard output, has none.
func TestNoLayoutUnresolvedWritesRefused(t *testing.T) {
	t.Parallel()
	repo, env := noLayoutRepo(t)
	before, err := os.ReadFile(filepath.Join(repo, "src", "app.py"))
	if err != nil {
		t.Fatal(err)
	}
	refused := []string{
		"cd src && python3 - <<'E'\nf='app.py'\ns=open(f).read()\nopen(f,'w').write(s.replace('2','3'))\nE",
		"python3 - <<'E'\nimport pathlib\np=pathlib.Path('src')/'app.py'\np.write_text('X = 3')\nE",
		`python3 -c "import sys; open(sys.argv[1],'w').write('x')" src/app.py`,
		`node -e "require('fs').writeFileSync(process.argv[1],'x')" src/app.py`,
		`ruby -e "File.write('app.rb','x')"`,
		`perl -e 'open(my $f, ">", "app.pl"); print $f 1'`,
		"f=src/app.py; perl -pi -e 's/2/3/' $f",
		"git apply fix.patch", "patch -p1 < fix.patch", "patch src/app.py fix.patch",
		"f=src/app.py; echo x > $f", `echo x > "$OUT"`, "cp /tmp/a.py $DEST", "echo x | tee $F",
	}
	for _, c := range refused {
		for _, pi := range []bool{false, true} {
			r := preToolCall(t, env, repo, "Bash", cmd(c), pi)
			// A pi child's safety-guard may refuse the unresolvable target first.
			if pi && r.Code == 2 {
				if strings.Contains(r.Stderr, "route-guard") {
					t.Errorf("pi child: %s: route-guard refused it: %q", c, r.Stderr)
				}
				continue
			}
			code(t, r, 0, c)
			if got := adviceOf(t, r.Stdout); got != setUpNote() {
				t.Errorf("%s (pi %v): note %q, want the set-up note", c, pi, got)
			}
		}
	}
	allowed := []string{
		`python3 -c "import sys; sys.stdout.write('hi')"`, `python3 -c "print(open('src/app.py').read())"`, `node -e "process.stdout.write('1')"`,
		"python3 - <<'E'\nopen('/tmp/jhon-t94-probe.txt','w').write('x')\nE",
		"git apply --check fix.patch", "git apply --stat fix.patch", "patch --dry-run -p1 < fix.patch",
		"echo x > $HOME/out.log", "d=$(mktemp -d) && echo x > $d/o.log && rm -r $d", "echo x > /dev/null 2>&1",
	}
	for _, c := range allowed {
		r := preToolCall(t, env, repo, "Bash", cmd(c), false)
		code(t, r, 0, c)
		if strings.Contains(r.Stderr+r.Stdout, "route-guard") {
			t.Errorf("%s: %q %q", c, r.Stdout, r.Stderr)
		}
	}
	after, _ := os.ReadFile(filepath.Join(repo, "src", "app.py"))
	if string(after) != string(before) {
		t.Errorf("src/app.py changed: %q", after)
	}
}

// Rule 6 for scope-guard: a bounded writer's write whose target the guard
// cannot resolve runs with a note that it cannot be held to the surfaces
// (T153, L957); one that resolves into them passes with none.
func TestScopeGuardUnresolvedWrites(t *testing.T) {
	t.Parallel()
	p := scopePrompt
	s := newScope(t, &p)
	for _, c := range []string{"echo x > $OUT", "git apply fix.patch", "patch -p1 < fix.patch", `node -e "require('fs').writeFileSync(process.argv[1],'x')" docs/x.md`} {
		r := s.guard(t, "Bash", map[string]any{"command": c})
		code(t, r, 0, c)
		matches(t, adviceOf(t, r.Stdout), unresolvedNote, c)
	}
	for _, c := range []string{"f=src/a.ts; echo x > $f", "git apply --check fix.patch", `python3 -c "open('src/a.ts','w').write('x')"`, "echo x > /dev/null"} {
		code(t, s.guard(t, "Bash", map[string]any{"command": c}), 0, c)
	}
	code(t, s.guard(t, "Bash", map[string]any{"command": "f=docs/x.md; echo x > $f"}), 2, "a variable naming a file out of scope")
}

// Rule 2 (L635): the owner's line passes in a sub-agent. A redirect the
// guard cannot resolve passes too: the sandbox bounds it (L1366).
func TestScratchDirFreshRedirectResolves(t *testing.T) {
	t.Parallel()
	s := scratchTree(t)
	for _, c := range []string{ownersSeedLine, "tmp=$(mktemp -d); git show HEAD:cli.py > $tmp/seed.py; rm -rf $tmp",
		`d=$(mktemp -d) && echo x > "$d/out.txt" && rm -r $d`} {
		r := s.bash(t, c, sgOpts{})
		code(t, r, 0, c)
		if r.Stdout != "" || r.Stderr != "" {
			t.Errorf("%s: %q %q", c, r.Stdout, r.Stderr)
		}
		s.code(t, c, top(), 0, "top level")
		if r := s.piCall(t, c, true); r.Code != 0 {
			t.Errorf("pi child: %s exits %d: %s", c, r.Code, r.Stderr)
		}
	}
	for _, c := range []string{
		"d=$(mktemp -d) && tar -C $d -xf x.tar && echo x > $d/a && rm -r $d", "d=$(mktemp -d) && echo x > $d/sub/a && rm -r $d",
		"d=$(mktemp -d) && echo x > $d/../a", "d=$(mktemp -d) && echo x > $d/* ", "d=$(mktemp -d) && echo x > $d/a && echo y > $d/a",
		"true || d=$(mktemp -d); echo x > $d/a", "d=$(mktemp -d) && bash -c 'echo x > $d/a'", "d=$(mktemp -d) && (echo x > $d/a)",
		"d=$(mktemp -d) && python3 mk.py $d && echo x > $d/a", "d=$(mktemp -d) || true; echo x > $d/a",
	} {
		s.code(t, c, sgOpts{}, 0, "an unresolved write")
	}
}

// Rule 3 (L638): an edit tool's write into a home credentials directory is
// refused under any name, with a scratch HOME.
func TestEditToolsInHomeCredentialDirsRefused(t *testing.T) {
	t.Parallel()
	s, home := homeSG(t)
	support.Write(t, home, map[string]string{".aws/config": "[x]\n", ".config/gcloud/configurations/config_default": "x\n"})
	for _, f := range []string{".ssh/config", ".ssh/authorized_keys", ".aws/config", ".config/gcloud/configurations/config_default", ".aws/new.txt"} {
		abs := filepath.Join(home, f)
		before, _ := os.ReadFile(abs)
		for _, c := range []struct {
			tool string
			in   map[string]any
		}{
			{"Write", map[string]any{"file_path": abs, "content": "x"}},
			{"Edit", map[string]any{"file_path": abs, "old_string": "x", "new_string": "y"}},
			{"MultiEdit", map[string]any{"file_path": abs, "edits": []any{map[string]any{"old_string": "x", "new_string": "y"}}}},
			{"NotebookEdit", map[string]any{"notebook_path": abs, "new_source": "x"}},
		} {
			r := hook(t, s.env, "secrets-guard", []string{"--agent", "*"}, map[string]any{"tool_name": c.tool, "tool_input": c.in, "cwd": s.p, "agent_id": "a1", "agent_type": "worker"})
			code(t, r, 2, c.tool+" "+f)
			if want := "secrets-guard: " + c.tool + " of " + abs + ", in a home credentials directory.\n"; r.Stderr != want {
				t.Errorf("%s %s: stderr %q, want %q", c.tool, f, r.Stderr, want)
			}
			tilde := with(c.in, "file_path", "~/"+f, "notebook_path", "~/"+f)
			code(t, hook(t, s.env, "secrets-guard", []string{"--agent", "*"}, map[string]any{"tool_name": c.tool, "tool_input": tilde, "cwd": s.p}), 2, c.tool+" ~/"+f)
		}
		after, _ := os.ReadFile(abs)
		if string(after) != string(before) {
			t.Errorf("%s changed", f)
		}
	}
	for _, f := range []string{filepath.Join(home, "notes.md"), filepath.Join(s.p, "src", "a.go")} {
		r := hook(t, s.env, "secrets-guard", []string{"--agent", "*"}, map[string]any{"tool_name": "Write", "tool_input": map[string]any{"file_path": f, "content": "x"}, "cwd": s.p})
		code(t, r, 0, "Write "+f)
	}
}

// Rule 5 (L647): MultiEdit is judged by every guard that judges Edit.
func TestMultiEditRoutedLikeEdit(t *testing.T) {
	t.Parallel()
	r := hook(t, nil, "pre-tool", []string{"--matchers"}, "")
	code(t, r, 0, "--matchers")
	for _, line := range strings.Split(strings.TrimSpace(r.Stdout), "\n") {
		g, tools, _ := strings.Cut(line, "\t")
		ts := strings.Split(tools, "|")
		if hasEdit, hasMulti := contains(ts, "Edit"), contains(ts, "MultiEdit"); hasEdit != hasMulti {
			t.Errorf("%s judges %s: Edit %v, MultiEdit %v", g, tools, hasEdit, hasMulti)
		}
	}
	repo, env := noLayoutRepo(t)
	in := map[string]any{"file_path": filepath.Join(repo, "src", "app.py"), "edits": []any{map[string]any{"old_string": "2", "new_string": "3"}}}
	m := preToolCall(t, env, repo, "MultiEdit", in, false)
	code(t, m, 0, "MultiEdit in a repository with no layout")
	if got := adviceOf(t, m.Stdout); got != setUpNote() {
		t.Errorf("MultiEdit: note %q, want the set-up note", got)
	}
	p := scopePrompt
	s := newScope(t, &p)
	w := s.guard(t, "MultiEdit", map[string]any{"file_path": filepath.Join(s.repo, "src/util/helpers.ts"), "edits": []any{}})
	code(t, w, 2, "MultiEdit out of scope")
}

// T94b (L667): common agent commands the T94 tightening refused stay
// allowed, through route-guard in a repository with no layout and through
// scope-guard; the verifier's gaps stay refused.
var t94bAllowed = []string{
	"out=$(mktemp); pytest > $out 2>&1; tail $out",
	`log=$(mktemp) && npm test > "$log" 2>&1`,
	`pytest > "${TMPDIR:-/tmp}/out.log"`,
	"d=$(mktemp -d); cp src/app.py $d/; rm -r $d", "d=$(mktemp -d); cp src/app.py $d/",
	`python3 -c "print(1 >> 2)"`,
	`node -e "console.log(8 >> 1)"`,
	`python3 -c "import csv,sys; csv.writer(sys.stdout).writerow([1])"`,
	`python3 -c "l=[1,2]; l.remove(1); print(l)"`,
	`python3 -c "import pandas as pd; print(pd.DataFrame({'a':[1]}).rename(columns={'a':'b'}))"`,
	"go test ./... > /dev/null 2>&1", "rg -n TODO src", "git log --oneline -5", "git diff HEAD~1", "cat src/app.py",
}

func TestT94bCommonCommandsAllowed(t *testing.T) {
	t.Parallel()
	repo, env := noLayoutRepo(t)
	for _, c := range t94bAllowed {
		r := routeCall(t, env, repo, "Bash", cmd(c))
		code(t, r, 0, "route-guard: "+c)
		if r.Stderr != "" {
			t.Errorf("%s: %q", c, r.Stderr)
		}
		// safety-guard refuses `rm -r $d` once cp names $d, as before T94 (scratchLinkers).
		if !strings.Contains(c, "rm -r") {
			r = preToolCall(t, env, repo, "Bash", cmd(c), false)
			code(t, r, 0, c)
			if strings.Contains(r.Stderr+r.Stdout, "refused") {
				t.Errorf("%s: %q %q", c, r.Stdout, r.Stderr)
			}
		}
	}
	p := scopePrompt
	s := newScope(t, &p)
	for _, c := range t94bAllowed {
		code(t, s.guard(t, "Bash", map[string]any{"command": c}), 0, "scope-guard: "+c)
	}
	// An in-scope argv target of inline code is in scope.
	c := `python3 -c "import sys; open(sys.argv[1],'w').write('x')" src/a.ts`
	code(t, s.guard(t, "Bash", map[string]any{"command": c}), 0, "scope-guard: "+c)
}

// The verifier's gaps are still read as code writes: route-guard's set-up
// note and scope-guard's unresolved-target note (each a refusal until T153,
// L957); a target it resolves out of scope stays refused.
func TestT94bGapsRefused(t *testing.T) {
	t.Parallel()
	repo, env := noLayoutRepo(t)
	refused := []string{
		`perl -e 'open(F,">src/app.py"); print F 1'`,
		`perl -e 'open(F, ">$ARGV[0]"); print F 1' src/app.py`,
		`perl -e 'my $p = shift; open my $fh, '"'"'>'"'"', $p; print $fh 1' src/app.py`,
		"git -C . apply fix.patch", "git -c core.x=y apply fix.patch",
		"python3 - <<'E'\nimport sys\np='src/'+'app.py'\nopen(p,'w').write('x')\nE",
		`python3 -c "import sys; print('x', file=open(sys.argv[1],'w'))" src/app.py`,
		// Second verify of T94b (L678): a variable open mode and os.open with write flags.
		`python3 -c "m='w'; open('src/app.py', m).write('x')"`,
		`python3 -c "import os; fd=os.open('src/app.py', os.O_WRONLY); os.write(fd,b'x')"`,
		`ruby -e "IO.write(ARGV[0], 'x')" src/app.py`,
		"d=$(mktemp -d) || true; echo x > $d/a; cp $d/a src/app.py",
	}
	for _, c := range refused {
		r := preToolCall(t, env, repo, "Bash", cmd(c), false)
		code(t, r, 0, c)
		if got := adviceOf(t, r.Stdout); got != setUpNote() {
			t.Errorf("%s: note %q, want the set-up note", c, got)
		}
	}
	p := scopePrompt
	s := newScope(t, &p)
	for _, c := range []string{`perl -e 'open(F, ">$ARGV[0]"); print F 1' docs/x.md`, "git -C . apply fix.patch",
		`python3 -c "import sys; open(sys.argv[1],'w').write('x')" docs/x.md`} {
		r := s.guard(t, "Bash", map[string]any{"command": c})
		code(t, r, 0, "scope-guard: "+c)
		matches(t, adviceOf(t, r.Stdout), unresolvedNote, "scope-guard: "+c)
	}
	c := `python3 -c "m='w'; open('docs/x.md', m).write('x')"`
	code(t, s.guard(t, "Bash", map[string]any{"command": c}), 2, "scope-guard: "+c)
}
