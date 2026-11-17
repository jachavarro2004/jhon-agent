package sh

import (
	"reflect"
	"testing"
)

func words(t *testing.T, src string) [][]string {
	t.Helper()
	cmds, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	var out [][]string
	for _, c := range cmds {
		out = append(out, c.Words)
	}
	return out
}

func TestParseWords(t *testing.T) {
	for src, want := range map[string][][]string{
		`git status`:                     {{"git", "status"}},
		`a && b; c | d`:                  {{"a"}, {"b"}, {"c"}, {"d"}},
		`echo "x\y\$z" 'a b' a\ b`:       {{"echo", `x\y$z`, "a b", "a b"}},
		`$'\x67it' push`:                 {{"git", "push"}},
		`echo $(rm -rf /) done`:          {{"rm", "-rf", "/"}, {"echo", "$(…)", "done"}},
		"echo `id`":                      {{"id"}, {"echo", "`…`"}},
		`if true; then rm -rf x; fi`:     {{"true"}, {"rm", "-rf", "x"}},
		`f() { git reset --hard; }`:      {{"git", "reset", "--hard"}},
		`X=1 Y=$HOME/a cmd`:              {{"X=1", "Y=$HOME/a", "cmd"}},
		`export A=1`:                     {{"export", "A=1"}},
		`time git push`:                  {{"time", "git", "push"}},
		`echo $(( $(rm x) + 1 ))`:        {{"rm", "x"}, {"echo", "$((…))"}},
		`cat <(git log) > >(tee out)`:    {{"git", "log"}, {"tee", "out"}, {"cat", "<(…)"}},
		`while read l; do rm "$l"; done`: {{"read", "l"}, {"rm", "$l"}},
	} {
		if got := words(t, src); !reflect.DeepEqual(got, want) {
			t.Errorf("Parse(%q) words = %q, want %q", src, got, want)
		}
	}
}

func TestParseRedirectsAndPipes(t *testing.T) {
	cmds, err := Parse("cat <<'EOF' | sh\nrm -rf /\nEOF\necho hi 2>&1 >> log")
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 3 {
		t.Fatalf("got %d commands", len(cmds))
	}
	if r := cmds[0].Redirects[0]; r.Op != "<<" || r.Target != "EOF" || !r.Quoted || r.Body == nil || *r.Body != "rm -rf /\n" {
		t.Errorf("heredoc = %+v", r)
	}
	if cmds[1].PipeFrom != cmds[0] {
		t.Error("sh does not read cat's output")
	}
	if f := StdinOf(cmds[1]); f == nil || f.Text == nil || *f.Text != "rm -rf /\n" {
		t.Errorf("StdinOf(sh) = %+v", f)
	}
	rs := cmds[2].Redirects
	if len(rs) != 2 || rs[0].Op != ">&" || rs[0].Target != "1" || rs[0].N != "2" || rs[1].Op != ">>" || rs[1].Target != "log" || rs[1].N != "" {
		t.Errorf("redirects = %+v %+v", rs[0], rs[1])
	}
	if g, err := Parse("{ a; } | sh"); err != nil || !g[1].PipeOpaque {
		t.Errorf("a group's output is opaque: %+v %v", g, err)
	}
}

func TestParseUnreadable(t *testing.T) {
	for _, src := range []string{`echo 'open`, "cat <<EOF\n$(rm x)\nEOF", "cat <<EOF\nno end", `echo )`} {
		if _, err := Parse(src); err == nil {
			t.Errorf("Parse(%q) read an unreadable command", src)
		}
	}
}

func TestProgramsUnwrap(t *testing.T) {
	cmds, _ := Parse(`sudo env -C /x FOO=1 bash -c 'git push --force' && echo a b | xargs rm`)
	var got [][]string
	for _, c := range cmds {
		ps, err := CommandPrograms(c, nil, nil, Opts{})
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range ps {
			got = append(got, append(append([]string{}, p.Via...), p.Argv...))
		}
	}
	want := [][]string{{"sudo", "env", "bash -c", "git", "push", "--force"}, {"echo", "a", "b"}, {"xargs", "rm"}, {"xargs", "rm", "a", "b"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("programs = %q, want %q", got, want)
	}
}

func TestShellWritesSkipsDescriptors(t *testing.T) {
	any := func(string) bool { return true }
	for _, cmd := range []string{"ls 2>&1", "echo x >&2", "ls 2>&-", "ls 2>&1 | tail"} {
		if ws := ShellWrites(cmd, any); len(ws) != 0 {
			t.Errorf("ShellWrites(%q) = %+v", cmd, ws)
		}
	}
	for _, cmd := range []string{"echo x 2>f", "echo x &>f", "echo x > f"} {
		if ws := ShellWrites(cmd, any); len(ws) != 1 {
			t.Errorf("ShellWrites(%q) read no write", cmd)
		}
	}
}

func TestParseLiteralLoops(t *testing.T) {
	loops := func(src string) [][]Loop {
		cmds, err := Parse(src)
		if err != nil {
			t.Fatalf("Parse(%q): %v", src, err)
		}
		var out [][]Loop
		for _, c := range cmds {
			out = append(out, c.Loops)
		}
		return out
	}
	ab := []Loop{{Var: "f", Values: []string{"a", "b"}}}
	for src, want := range map[string][][]Loop{
		`for f in a b; do sed x > t_$f.py; done`:           {ab},
		`for f in a b; do for g in c; do echo; done; done`: {{{Var: "f", Values: []string{"a", "b"}}, {Var: "g", Values: []string{"c"}}}},
		`for f in a $X; do echo > $f; done`:                {nil},
		`for f in a b; do cd $f; echo > x; done`:           {nil, nil},
		`echo > x`:                                         {nil},
	} {
		if got := loops(src); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: loops %v, want %v", src, got, want)
		}
	}
}

// A bash 5.3 funsub (`${ …; }`, `${| …; }`) runs in the current shell: its
// commands share the outer scope. `$(…)`, `<(…)` and `( … )` are subshells.
func TestParseFunsubIsNoSubshell(t *testing.T) {
	scopes := func(src string) []int {
		t.Helper()
		cmds, err := Parse(src)
		if err != nil {
			t.Fatalf("Parse(%q): %v", src, err)
		}
		var out []int
		for _, c := range cmds {
			out = append(out, len(c.Scope))
		}
		return out
	}
	for src, want := range map[string][]int{
		`x=${ cd d; } && pwd`:      {0, 0, 0},
		`x=${| cd d; } && pwd`:     {0, 0, 0},
		`echo "${ cd d; }" && pwd`: {0, 0, 0},
		`x=$(cd d) && pwd`:         {1, 0, 0},
		`(x=${ cd d; }) && pwd`:    {1, 1, 0},
		`x=$(y=${ cd d; }) && pwd`: {1, 1, 0, 0},
		`cat <(cd d) && pwd`:       {1, 0, 0},
	} {
		if got := scopes(src); !reflect.DeepEqual(got, want) {
			t.Errorf("Parse(%q) scope depths = %v, want %v", src, got, want)
		}
	}
}
