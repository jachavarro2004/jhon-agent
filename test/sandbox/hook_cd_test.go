// A `cd` inside a wrapped command carries to the next call (L899 c). Claude
// Code keeps its Bash working directory between calls by asking its own
// shell where it ended; the wrapped command runs in an inner shell, so the
// inner shell reports its last directory through a file the hook names, and
// the line the outer shell runs ends there, with the command's exit code.
package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// cwdFile is the file a wrapped line names for the inner shell's last
// directory: under /tmp, with a random name.
var cwdFile = regexp.MustCompile(`/tmp/jhon-cwd-[0-9a-f]{32}`)

// followed is the line the outer shell runs for a foreground Bash call:
// the wrap, whose inner shell writes its last directory to file as it
// exits and evaluates the command it received as its first argument; then
// the outer shell reads the file, removes it, moves there and gives the
// command's exit code.
func followed(jhon, proj, shell, file, command string) string {
	return jhon + " sandbox run --project '" + proj + "' --profile work -- " + shell +
		` -c 'trap "{ command pwd -P >| ` + file + `; } 2>/dev/null" EXIT; __jhon_sandbox_command=$1; set --; eval " $__jhon_sandbox_command"' ` + shell + " '" + strings.ReplaceAll(command, "'", `'\''`) + "'" +
		`; __jhon_sandbox_s=$?; __jhon_sandbox_cd() { unset __jhon_sandbox_s; local d=; { IFS= builtin read -r -d '' d < ` + file + `; } 2>/dev/null; command rm -f -- ` + file +
		` 2>/dev/null; d=${d%$'\n'}; case $d in /*) builtin cd -- "$d" 2>/dev/null ;; esac; unset -f __jhon_sandbox_cd; return "$1"; }; __jhon_sandbox_cd "$__jhon_sandbox_s"`
}

// expected is the line the hook hands back for tool's command: a foreground
// Bash call's is followed, with the file got names; a Monitor command's is
// the plain wrap.
func expected(t *testing.T, tool, got, proj, shell, command string) string {
	t.Helper()
	if tool == "Bash" {
		return followed(support.Bin(t), proj, shell, cwdFile.FindString(got), command)
	}
	return support.Bin(t) + " sandbox run --project '" + proj + "' --profile work -- " + shell + " -c '" + strings.ReplaceAll(command, "'", `'\''`) + "'"
}

// wantBash holds got, a foreground Bash call's wrapped line, to followed
// with the file it names.
func wantBash(t *testing.T, got, proj, shell, command string) {
	t.Helper()
	files := cwdFile.FindAllString(got, -1)
	if len(files) != 3 || files[0] != files[1] || files[1] != files[2] {
		t.Fatalf("the line names %q; want one file under /tmp three times:\n %s", files, got)
	}
	if want := followed(support.Bin(t), proj, shell, files[0], command); got != want {
		t.Errorf("the line is\n %s\nwant\n %s", got, want)
	}
}

// noGuard judges no Bash call: these tests are the wrap's.
var noGuard = []string{"vcs-guard", ".git"}

// line is the wrapped line of a foreground Bash call of command.
func line(t *testing.T, b box, env []string, command string) string {
	t.Helper()
	r, a := preTool(t, b, env, noGuard, "Bash", `{"command":`+quoted(t, command)+`}`, false)
	if r.Code != 0 || a.Out.Decision != "allow" {
		t.Fatalf("%q: exit %d, stderr %q, stdout %q", command, r.Code, r.Stderr, r.Stdout)
	}
	return a.command(t)
}

// outerShells are the shells Claude Code runs a Bash call's line in.
func outerShells(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, name := range []string{"bash", "zsh"} {
		if p, err := exec.LookPath(name); err == nil {
			out[name] = p
		}
	}
	if len(out) == 0 {
		t.Skip("skipped: neither bash nor zsh on PATH")
	}
	return out
}

// after runs wrapped in the outer shell in dir, as Claude Code does, then
// asks that shell what it sees: the line's exit code, where it is, and
// whether the line left a name behind. The answer follows a marker.
func after(t *testing.T, outer, wrapped, dir string, env []string) (r support.Result, code, pwd, left string) {
	t.Helper()
	const mark = "--jhon-test-after--"
	script := wrapped + `; __test_rc=$?; printf '%s%s\037%s\037%s' '` + mark + `' "$__test_rc" "$PWD" "${__jhon_sandbox_s-}${__jhon_sandbox_command-}$(typeset -f __jhon_sandbox_cd)"`
	r = support.Run(t, support.Cmd{Args: []string{outer, "-c", script}, Dir: dir, Env: env})
	out, tail, ok := strings.Cut(r.Stdout, mark)
	parts := strings.Split(tail, "\x1f")
	if !ok || len(parts) != 3 {
		t.Fatalf("the outer shell printed %q, stderr %q", r.Stdout, r.Stderr)
	}
	r.Stdout = out
	return r, parts[0], parts[1], parts[2]
}

func TestACdInsideTheSandboxCarriesToTheNextCall(t *testing.T) {
	bash := realShell(t, "bash")
	b, env := wired(t, bash)
	sub := filepath.Join(b.proj, "sub dir", "it's")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, outer := range outerShells(t) {
		t.Run(name, func(t *testing.T) {
			first := line(t, b, env, `cd "sub dir/it's" && echo there`)
			wantBash(t, first, b.proj, bash, `cd "sub dir/it's" && echo there`)
			r, code, pwd, left := after(t, outer, first, b.proj, env)
			if r.Stdout != "there\n" || r.Stderr != "" || code != "0" || pwd != sub || left != "" {
				t.Fatalf("the first call: stdout %q, stderr %q, exit %s, the outer shell in %q with %q left; want there, 0 and %q", r.Stdout, r.Stderr, code, pwd, left, sub)
			}
			if file := cwdFile.FindString(first); exists(file) {
				t.Errorf("%s was not removed", file)
			}
			// Claude Code starts the next call where the first line ended.
			second := line(t, b, env, "pwd -P")
			r, code, pwd, _ = after(t, outer, second, pwd, env)
			if r.Stdout != sub+"\n" || r.Stderr != "" || code != "0" || pwd != sub {
				t.Errorf("the second call: stdout %q, stderr %q, exit %s, the outer shell in %q; want %q", r.Stdout, r.Stderr, code, pwd, sub)
			}
		})
	}
}

// The line's exit code, stdout and stderr are the command's, however the
// command ends, and the outer shell moves only where the inner shell could
// report a directory the outer one can enter.
func TestTheLineKeepsTheCommandsExitCodeHoweverItEnds(t *testing.T) {
	bash := realShell(t, "bash")
	b, env := wired(t, bash)
	run := filepath.Join(b.base, "run")
	sub := filepath.Join(b.proj, "sub")
	for _, d := range []string{run, sub} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// The runtime dir is a private folder inside the sandbox: what a command
	// makes there is not on the disk outside.
	env = append(env, "XDG_RUNTIME_DIR="+run)
	for _, c := range []struct {
		name, command, stdout, stderr, code, dir string
	}{
		{"exit N", "cd sub; exit 7", "", "", "7", sub},
		{"a failing last command", "cd sub; false", "", "", "1", sub},
		{"a signal bash traps on", "cd sub; kill -TERM $$", "", "", "143", sub},
		{"a kill", "cd sub; kill -KILL $$", "", "", "137", b.proj},
		{"exec", "cd sub; exec echo replaced", "replaced\n", "", "0", b.proj},
		{"its own EXIT trap", "cd sub; trap 'echo own' EXIT", "own\n", "", "0", b.proj},
		{"an open here-document", "cd sub\ncat <<EOF\nhello", "hello\n", bash + ": line 3: warning: here-document at line 2 delimited by end-of-file (wanted `EOF')\n", "0", sub},
		{"its last directory deleted", "mkdir gone && cd gone && rmdir ../gone", "", "", "0", b.proj},
		{"a directory only the sandbox has", `mkdir "$XDG_RUNTIME_DIR/inside" && cd "$XDG_RUNTIME_DIR/inside" && pwd`, run + "/inside\n", "", "0", b.proj},
		{"no positional parameter of its own", `echo "$#|$0|$1|$LINENO"; cd sub`, "0|" + bash + "||1\n", "", "0", sub},
		{"no cd at all", "echo here", "here\n", "", "0", b.proj},
	} {
		for name, outer := range outerShells(t) {
			wrapped := line(t, b, env, c.command)
			r, code, pwd, left := after(t, outer, wrapped, b.proj, env)
			if r.Stdout != c.stdout || r.Stderr != c.stderr || code != c.code || pwd != c.dir || left != "" {
				t.Errorf("%s through %s: stdout %q, stderr %q, exit %s, the outer shell in %q with %q left\n want %q, %q, %s, %q", c.name, name, r.Stdout, r.Stderr, code, pwd, left, c.stdout, c.stderr, c.code, c.dir)
			}
			if file := cwdFile.FindString(wrapped); exists(file) {
				t.Errorf("%s through %s: %s was not removed", c.name, name, file)
			}
		}
	}
}

// What the file holds is only ever the argument of `cd --`: a value that is
// missing, empty, relative, no directory or made of shell text moves nothing
// and runs nothing.
func TestTheOuterShellMovesOnlyToADirectoryTheFileNames(t *testing.T) {
	bash := realShell(t, "bash")
	b, env := wired(t, bash)
	sub := filepath.Join(b.proj, "sub")
	odd := filepath.Join(b.proj, "odd\nname\n")
	for _, d := range []string{sub, odd} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	marker := filepath.Join(b.proj, "ran")
	wrapped := line(t, b, env, "true")
	file := cwdFile.FindString(wrapped)
	_, tail, ok := strings.Cut(wrapped, "; __jhon_sandbox_s=$?; ")
	if !ok || file == "" {
		t.Fatalf("the line has no tail: %s", wrapped)
	}
	missing := "\x00missing"
	for name, c := range map[string]struct{ holds, dir string }{
		"missing":             {missing, b.proj},
		"empty":               {"", b.proj},
		"a newline":           {"\n", b.proj},
		"relative":            {"sub\n", b.proj},
		"a file":              {filepath.Join(b.proj, ".git", "HEAD") + "\n", b.proj},
		"not there":           {filepath.Join(b.proj, "gone") + "\n", b.proj},
		"shell text":          {"$(touch " + marker + ")`touch " + marker + "`; touch " + marker + "\n", b.proj},
		"an option":           {"-P\n", b.proj},
		"a directory":         {sub + "\n", sub},
		"a name with newline": {odd + "\n", odd},
	} {
		for shell, outer := range outerShells(t) {
			os.Remove(file)
			if c.holds != missing {
				if err := os.WriteFile(file, []byte(c.holds), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			// The tail as the line runs it, after a command that gave 3.
			r, code, pwd, left := after(t, outer, "(exit 3); __jhon_sandbox_s=$?; "+tail, b.proj, env)
			if r.Stdout != "" || r.Stderr != "" || code != "3" || pwd != c.dir || left != "" || exists(file) || exists(marker) {
				t.Errorf("%s through %s: stdout %q, stderr %q, exit %s, the outer shell in %q with %q left, the file there %v, shell text run %v; want 3 and %q", name, shell, r.Stdout, r.Stderr, code, pwd, left, exists(file), exists(marker), c.dir)
			}
		}
	}
}

// A background Bash call and a Monitor command outlive the line: nothing
// follows them, and their form is the plain wrap.
func TestACommandThatOutlivesItsLineIsNotFollowed(t *testing.T) {
	bash := realShell(t, "bash")
	b, env := wired(t, bash)
	plain := support.Bin(t) + " sandbox run --project '" + b.proj + "' --profile work -- " + bash + " -c 'cd sub && ls'"
	for name, c := range map[string]struct{ tool, input string }{
		"a background Bash call":       {"Bash", `{"command":"cd sub && ls","run_in_background":true}`},
		"a background mark not a bool": {"Bash", `{"command":"cd sub && ls","run_in_background":"yes"}`},
		"a Monitor command":            {"Monitor", `{"command":"cd sub && ls","timeout_ms":1000}`},
	} {
		if _, a := preTool(t, b, env, noGuard, c.tool, c.input, false); a.command(t) != plain {
			t.Errorf("%s: the command is\n %s\nwant\n %s", name, a.command(t), plain)
		}
	}
	for name, input := range map[string]string{
		"a foreground Bash call":       `{"command":"cd sub && ls"}`,
		"a Bash call marked not so":    `{"command":"cd sub && ls","run_in_background":false}`,
		"a Bash call with a null mark": `{"command":"cd sub && ls","run_in_background":null}`,
	} {
		_, a := preTool(t, b, env, noGuard, "Bash", input, false)
		t.Run(name, func(t *testing.T) { wantBash(t, a.command(t), b.proj, bash, "cd sub && ls") })
	}
}

// modeCall is the hook's answer for a top-level Monitor command, whose
// wrapped line has no random part, in a permission mode of Claude Code.
func modeCall(t *testing.T, b box, env []string, mode, command string) support.Result {
	t.Helper()
	payload := `{"hook_event_name":"PreToolUse","session_id":"s","cwd":` + quoted(t, b.proj) + `,"tool_name":"Monitor","tool_input":{"command":` + quoted(t, command) + `,"timeout_ms":1000},"permission_mode":` + quoted(t, mode) + `}`
	return support.Run(t, support.Cmd{Args: append([]string{support.Bin(t), "hook", "pre-tool", "--agent", "worker"}, bashGuards...), Env: env, Stdin: payload})
}

// In Claude Code's plan mode the hook sends the wrapped command and no
// decision, so plan mode's own rules decide (L899 a); in every other mode a
// command the guards allow says allow, as it did.
func TestPlanModeGetsTheWrappedCommandAndNoDecision(t *testing.T) {
	bash := realShell(t, "bash")
	b, env := wired(t, bash)
	updated := `"updatedInput":{"command":"` + support.Bin(t) + ` sandbox run --project '` + b.proj + `' --profile work -- ` + bash + ` -c 'ls src'","timeout_ms":1000}}}`
	r := modeCall(t, b, env, "plan", "ls src")
	if want := `{"hookSpecificOutput":{"hookEventName":"PreToolUse",` + updated; r.Code != 0 || r.Stderr != "" || r.Stdout != want {
		t.Errorf("plan mode: exit %d, stderr %q, stdout\n %s\nwant\n %s", r.Code, r.Stderr, r.Stdout, want)
	}
	for _, mode := range []string{"default", "acceptEdits", "auto", "dontAsk", "bypassPermissions"} {
		r := modeCall(t, b, env, mode, "ls src")
		want := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow","permissionDecisionReason":"Jhon's guards allow the command; it runs in Jhon's sandbox",` + updated
		if r.Code != 0 || r.Stderr != "" || r.Stdout != want {
			t.Errorf("%s mode: exit %d, stderr %q, stdout\n %s\nwant\n %s", mode, r.Code, r.Stderr, r.Stdout, want)
		}
	}
}
