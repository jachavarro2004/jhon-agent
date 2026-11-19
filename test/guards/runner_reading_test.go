// The guard reads what a runner runs (L486): each misread is read right, and
// each input the guard cannot see is unreadable, asked as such.

package guards

import (
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// runnerActs are the owner-only acts and a deny-level one, each refused
// wherever it is read.
var runnerActs = []string{"jhon ldd off", "jhon mode yolo", "jhon grant push", "git push " + "--force"}

var (
	modes   = []string{"normal", "yolo", "super-yolo"}
	callers = []string{"top-default", "top-bypass", "subagent"}
)

// partA are the forms that run act, each read as running it (L486, A1-A8).
func partA(act string) []string {
	ifs := strings.ReplaceAll(act, " ", "${IFS}")
	return []string{
		// A4: parallel runs {} through a shell.
		"parallel {} ::: a1 '" + act + "'",
		"parallel sh -c {} ::: '" + act + "'",
		"parallel 'sh -c {}' ::: '" + act + "'",
		// A5: xargs -0 and -d.
		`printf '%s\0' '` + act + `' | xargs -0 sh -c`,
		`printf '%s\0' '` + act + `' | xargs --null sh -c`,
		`printf '%s;' '` + act + `' | xargs -d ';' sh -c`,
		// A6: $IFS is a word break.
		"ash -c '" + ifs + "'",
		"sh -c '" + ifs + "'",
		"sh -c '" + strings.ReplaceAll(act, " ", "$IFS ") + "'",
		"echo '" + ifs + "' | at now",
		// L491: parallel's stdin, xargs clusters.
		"echo '" + act + "' | parallel --pipe sh",
		"echo '" + act + "' | parallel sh",
		"echo '" + act + "' | parallel -j1 --pipe bash",
		`printf '%s\0' '` + act + `' | xargs -r0 sh -c`,
		`printf '%s\0' '` + act + `' | xargs -0r sh -c`,
		// herdr 0.9.5 (L491): what it runs or types into a pane.
		"herdr pane run p1 " + act,
		"herdr --session s pane run p1 '" + act + "'",
		"herdr --session=s pane run p1 -- " + act,
		"herdr pane send-text p1 '" + act + `\n'`,
		"herdr --remote=host pane send-text p1 $'" + act + `\r'`,
		"herdr pane send-keys p1 '" + act + "' enter",
		"herdr --machine m pane send-keys p1 " + act + " Enter",
		"herdr agent send-keys a1 '" + act + "' enter",
		"herdr agent prompt a1 '!" + act + "'",
		"herdr agent prompt --wait a1 '!" + act + "' --until idle",
		"herdr agent prompt a1 --timeout=5 $'hi\\n!" + act + "'",
		"herdr agent start x --kind " + strings.Replace(act, " ", " --pane p1 -- ", 1),
		"herdr agent start x --pane=p1 --kind=" + strings.Replace(act, " ", " -- ", 1),
		"herdr agent start x --timeout 5 --pane p1 --kind " + strings.Replace(act, " ", " -- ", 1),
	}
}

// partB are the forms whose commands come from input the guard cannot see:
// judged by their visible words, which harm nothing (T152, L1311). f exists
// and the line does not write it.
var partB = []string{
	"parallel :::: f",
	"parallel -a f",
	"parallel --arg-file f",
	"cat f | parallel",
	"cat f | parallel sh -c",
	"xargs -a f sh -c",
	"at -f f now",
	"at now < f",
	"batch -f f",
	// A9: at reads a file the line writes before it runs.
	"echo x > g; at -f g now",
	"echo x > g; at now < g",
}

func TestRunnersReadRight(t *testing.T) {
	t.Parallel()
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			repo, _, env := ownerFixture(t, mode)
			for _, act := range runnerActs {
				for _, c := range partA(act) {
					for _, caller := range callers {
						code(t, ownerCall(t, env, repo, caller, c), 2, mode+"/"+caller+": "+c)
					}
				}
			}
		})
	}
}

func TestRunnersUnseenInputPasses(t *testing.T) {
	t.Parallel()
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			repo, _, env := ownerFixture(t, mode)
			support.Write(t, repo, map[string]string{"f": "jhon ldd off\n"})
			for _, c := range partB {
				for _, caller := range callers {
					r := ownerCall(t, env, repo, caller, c)
					label := mode + "/" + caller + ": " + c
					code(t, r, 0, label)
					noDecision(t, r.Stdout, label)
				}
			}
		})
	}
}

// What these runners run harmlessly keeps its decision (L486).
func TestRunnersBenignKeepTheirDecision(t *testing.T) {
	t.Parallel()
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			repo, _, env := ownerFixture(t, mode)
			support.Write(t, repo, map[string]string{"f": "a\nb\n"})
			for _, c := range []string{"parallel gzip ::: a b",
				`printf 'a\0b' | xargs -0 echo`, "at now <<< 'make'", "parallel echo :::: f",
				"seq 3 | parallel echo", `printf 'a\0' | xargs -r0 echo`,
				"herdr pane run p1 'make test'", "herdr --session s pane run p1 ls", "herdr pane send-text p1 'ls\n'", "herdr pane send-keys p1 ls enter",
				"herdr agent prompt a1 'Fix the failing test in src/a.ts and run it' --wait",
				"herdr agent start worker --kind pi --pane p1", "herdr agent start w --kind=claude --pane=p1 -- --model sonnet"} {
				for _, caller := range callers {
					r := ownerCall(t, env, repo, caller, c)
					code(t, r, 0, mode+"/"+caller+": "+c)
					noDecision(t, r.Stdout, mode+"/"+caller+": "+c)
				}
			}
		})
	}
}
